// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"log/slog"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type teardownFn func()

// subscribeNeighborUpdates sets up a netlink subscription for neighbor
// state changes in the refresher's network namespace. Returns the update
// channel and a cleanup function. If subscription fails, returns a nil
// channel (the refresher falls back to periodic polling only) and a no-op
// cleanup function.
func (r *BridgeRefresher) subscribeNeighborUpdates() (<-chan netlink.NeighUpdate, teardownFn) {
	noop := func() {}

	ns, err := netns.GetFromPath(r.namespace)
	if err != nil {
		slog.Debug("failed to get namespace for neighbor subscription",
			"namespace", r.namespace, "error", err)
		return nil, noop
	}

	var bridgeIndex int
	err = netnamespace.In(ns, func() error {
		bridge, err := netlink.LinkByName(r.bridgeName)
		if err != nil {
			return err
		}
		bridgeIndex = bridge.Attrs().Index
		return nil
	})
	if err != nil {
		slog.Debug("failed to get bridge for neighbor subscription",
			"bridge", r.bridgeName, "error", err)
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", r.namespace, "error", err)
		}
		return nil, noop
	}

	r.bridgeIndex = bridgeIndex

	ch := make(chan netlink.NeighUpdate)
	done := make(chan struct{})

	err = netlink.NeighSubscribeWithOptions(ch, done, netlink.NeighSubscribeOptions{
		Namespace: &ns,
		ErrorCallback: func(err error) {
			slog.Debug("neighbor subscription error",
				"bridge", r.bridgeName, "error", err)
		},
	})
	if err != nil {
		slog.Debug("failed to subscribe to neighbor updates",
			"bridge", r.bridgeName, "error", err)
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", r.namespace, "error", err)
		}
		return nil, noop
	}

	slog.Info("subscribed to neighbor updates", "bridge", r.bridgeName, "vni", r.vni)

	return ch, func() {
		close(done)
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", r.namespace, "error", err)
		}
	}
}
