// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"fmt"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// listStaleNeighbors returns all STALE IPv4 neighbors on the bridge.
// These neighbors are about to be garbage collected and need ARP probes
// to refresh them and prevent EVPN Type-2 route withdrawal.
func (r *BridgeRefresher) listStaleNeighbors() ([]netlink.Neigh, error) {
	bridge, err := netlink.LinkByName(r.bridgeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge %s: %w", r.bridgeName, err)
	}

	// List all IPv4 neighbors on the bridge
	neighbors, err := netlink.NeighList(bridge.Attrs().Index, unix.AF_INET)
	if err != nil {
		return nil, fmt.Errorf("failed to list neighbors: %w", err)
	}

	staleNeighbors := make([]netlink.Neigh, 0, len(neighbors))
	for _, neigh := range neighbors {
		// Only include STALE neighbors that need refreshing
		if neigh.State&netlink.NUD_STALE == 0 {
			continue
		}

		// Skip entries without MAC address (can't send unicast ARP)
		if len(neigh.HardwareAddr) == 0 {
			continue
		}

		// Skip entries without IP address
		if neigh.IP == nil {
			continue
		}

		staleNeighbors = append(staleNeighbors, neigh)
	}

	return staleNeighbors, nil
}
