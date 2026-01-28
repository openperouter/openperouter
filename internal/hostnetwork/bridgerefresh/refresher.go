// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"github.com/openperouter/openperouter/internal/netnamespace"
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
// It periodically sends ARP probes to STALE neighbors to prevent
// EVPN Type-2 routes from being withdrawn. The ARP replies also
// refresh FDB entries as a side effect.
type BridgeRefresher struct {
	bridgeName    string   // e.g., "br-pe-110"
	namespace     string   // Path to network namespace
	gatewayIPs    []net.IP // L2 gateway IPs (source for ARP probes)
	refreshPeriod time.Duration
	vni           int

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a new BridgeRefresher for an L2VNI.
// Call Start to begin the refresh loop.
func New(params hostnetwork.L2VNIParams, opts StartOptions) (*BridgeRefresher, error) {
	gatewayIPs, err := parseGatewayIPs(params.L2GatewayIPs)
	if err != nil {
		return nil, err
	}
	if len(gatewayIPs) == 0 {
		slog.Debug("no gateway IPs configured, bridge refresher will skip ARP probes",
			"vni", params.VNI)
	}

	refreshPeriod := DefaultRefreshPeriod
	if opts.RefreshPeriod > 0 {
		refreshPeriod = opts.RefreshPeriod
	}

	refresher := &BridgeRefresher{
		bridgeName:    hostnetwork.BridgeName(params.VNI),
		namespace:     params.TargetNS,
		gatewayIPs:    gatewayIPs,
		refreshPeriod: refreshPeriod,
	}
	return refresher, nil
}

// Start begins the refresh loop.
// The refresher runs in the background and periodically sends ARP probes
// to STALE neighbors to prevent route withdrawal.
func (r *BridgeRefresher) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.run(ctx)
	}()

	slog.Info("started bridge refresher", "bridge", r.bridgeName, "vni", r.vni)
}

// Stop gracefully stops the refresher and waits for it to finish.
func (r *BridgeRefresher) Stop() {
	r.cancel()
	r.wg.Wait()
	slog.Info("stopped bridge refresher", "bridge", r.bridgeName)
}

// run is the main refresh loop.
func (r *BridgeRefresher) run(ctx context.Context) {
	ticker := time.NewTicker(r.refreshPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refresh()
		}
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

// refreshStaleNeighbors sends ARP probes to STALE neighbors.
func (r *BridgeRefresher) refreshStaleNeighbors() {
	if len(r.gatewayIPs) == 0 {
		return // No gateway IPs, can't send ARP probes
	}

	neighbors, err := r.listStaleNeighbors()
	if err != nil {
		slog.Debug("failed to list stale neighbors", "bridge", r.bridgeName, "error", err)
		return
	}

	for _, neigh := range neighbors {
		if err := r.sendARPProbe(neigh.IP, neigh.HardwareAddr); err != nil {
			slog.Debug("failed to send ARP probe", "ip", neigh.IP, "mac", neigh.HardwareAddr, "error", err)
		}
	}

	if len(neighbors) > 0 {
		slog.Debug("sent ARP probes to stale neighbors", "bridge", r.bridgeName, "count", len(neighbors))
	}
}

// parseGatewayIPs parses L2 gateway IP strings (in CIDR format) into net.IP addresses.
// It filters for IPv4 addresses only, as ARP is IPv4-specific.
func parseGatewayIPs(ipStrs []string) ([]net.IP, error) {
	var ips []net.IP
	for _, ipStr := range ipStrs {
		ip, _, err := net.ParseCIDR(ipStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse gateway IP %q: %w", ipStr, err)
		}
		// Only include IPv4 addresses for ARP
		if ipfamily.ForAddress(ip) == ipfamily.IPv4 {
			ips = append(ips, ip.To4())
		}
	}
	return ips, nil
}
