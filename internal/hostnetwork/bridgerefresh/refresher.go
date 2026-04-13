// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	// DefaultRefreshPeriod is how often to check and refresh neighbor entries.
	DefaultRefreshPeriod = 60 * time.Second
)

// StartOptions configures optional parameters for BridgeRefresher.
type StartOptions struct {
	RefreshPeriod time.Duration // Override DefaultRefreshPeriod (for testing)
}

// BridgeRefresher manages neighbor refresh for an L2VNI bridge.
// It reacts to netlink neighbor notifications and also periodically
// sends ICMP pings to STALE neighbors to prevent EVPN Type-2 routes
// from being withdrawn and to force STALE neighbors to FAILED state
// in case the entry is really STALE.
type BridgeRefresher struct {
	bridgeName    string // e.g., "br-pe-110"
	namespace     string // Path to network namespace
	refreshPeriod time.Duration
	vni           int
	bridgeIndex   int // cached link index for filtering netlink events

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new BridgeRefresher for an L2VNI.
// Call Start to begin the refresh loop.
func New(params hostnetwork.L2VNIParams, opts StartOptions) (*BridgeRefresher, error) {
	refreshPeriod := DefaultRefreshPeriod
	if opts.RefreshPeriod > 0 {
		refreshPeriod = opts.RefreshPeriod
	}

	refresher := &BridgeRefresher{
		bridgeName:    hostnetwork.BridgeName(params.VNI),
		namespace:     params.TargetNS,
		refreshPeriod: refreshPeriod,
		vni:           params.VNI,
	}
	return refresher, nil
}

// Start begins the refresh loop.
// The refresher runs in the background and periodically sends ICMP pings
// to STALE neighbors to prevent route withdrawal.
func (r *BridgeRefresher) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	r.wg.Go(func() {
		r.run(ctx)
	})

	slog.Info("started bridge refresher", "bridge", r.bridgeName, "vni", r.vni)
}

// Stop gracefully stops the refresher and waits for it to finish.
func (r *BridgeRefresher) Stop() {
	r.cancel()
	r.wg.Wait()
	slog.Info("stopped bridge refresher", "bridge", r.bridgeName)
}

// run is the main refresh loop. It subscribes to netlink neighbor
// notifications for immediate reaction to STALE transitions, with
// periodic polling as a fallback.
func (r *BridgeRefresher) run(ctx context.Context) {
	ticker := time.NewTicker(r.refreshPeriod)
	defer ticker.Stop()

	neighCh, stopNetlinkListener := r.subscribeNeighborUpdates()

	for {
		select {
		case <-ctx.Done():
			stopNetlinkListener()
			return
		case <-ticker.C:
			r.refresh()
		case update, ok := <-neighCh:
			if !ok {
				slog.Error("neighbor subscription closed unexpectedly", "bridge", r.bridgeName)
				neighCh = nil
				continue
			}
			if r.isRelevantNeighUpdate(update) {
				r.refreshNeighbor(update.Neigh)
			}
		}
	}
}

// isRelevantNeighUpdate returns true if the update is a STALE transition
// on this refresher's bridge that warrants an immediate refresh.
func (r *BridgeRefresher) isRelevantNeighUpdate(update netlink.NeighUpdate) bool {
	if update.LinkIndex != r.bridgeIndex {
		return false
	}
	if update.State&netlink.NUD_STALE == 0 {
		return false
	}
	if update.IP == nil || update.IP.IsLinkLocalUnicast() {
		return false
	}
	return true
}

// refreshNeighbor pings a single neighbor inside the bridge namespace.
func (r *BridgeRefresher) refreshNeighbor(neigh netlink.Neigh) {
	ns, err := netns.GetFromPath(r.namespace)
	if err != nil {
		slog.Debug("failed to get namespace for neighbor refresh", "namespace", r.namespace, "error", err)
		return
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Debug("failed to close namespace", "namespace", r.namespace, "error", err)
		}
	}()

	if err := netnamespace.In(ns, func() error {
		slog.Debug("pinging stale neighbor", "ip", neigh.IP, "bridge", r.bridgeName)
		return r.sendPing(neigh.IP)
	}); err != nil {
		slog.Debug("failed to ping neighbor", "ip", neigh.IP, "bridge", r.bridgeName, "error", err)
	}
}

// refresh performs a single refresh cycle.
func (r *BridgeRefresher) refresh() {
	ns, err := netns.GetFromPath(r.namespace)
	if err != nil {
		slog.Debug("failed to get namespace for refresh", "namespace", r.namespace, "error", err)
		return
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Debug("failed to close namespace", "namespace", r.namespace, "error", err)
		}
	}()

	if err := netnamespace.In(ns, func() error {
		r.refreshStaleNeighbors()
		return nil
	}); err != nil {
		slog.Debug("failed to execute refresh in namespace", "namespace", r.namespace, "error", err)
	}
}

// refreshStaleNeighbors sends ICMP pings to all STALE neighbors on the bridge.
func (r *BridgeRefresher) refreshStaleNeighbors() {
	slog.Debug("refreshing stale neighbors", "bridge", r.bridgeName)

	neighbors, err := r.listStaleNeighbors()
	if err != nil {
		slog.Debug("failed to list stale neighbors", "bridge", r.bridgeName, "error", err)
		return
	}

	for _, neigh := range neighbors {
		slog.Debug("pinging stale neighbor", "ip", neigh.IP, "bridge", r.bridgeName)
		if err := r.sendPing(neigh.IP); err != nil {
			slog.Debug("failed to ping neighbor", "ip", neigh.IP, "bridge", r.bridgeName, "error", err)
		}
	}
}
