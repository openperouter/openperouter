// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// listStaleNeighbors returns all STALE neighbors (IPv4 and IPv6) on the bridge.
// These neighbors are about to be garbage collected and need pings
// to refresh them and prevent EVPN Type-2 route withdrawal.
func (r *BridgeRefresher) listStaleNeighbors() ([]netlink.Neigh, error) {
	bridge, err := netlink.LinkByName(r.bridgeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge %s: %w", r.bridgeName, err)
	}

	neighbors, err := netlink.NeighList(bridge.Attrs().Index, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list neighbors: %w", err)
	}

	staleNeighbors := make([]netlink.Neigh, 0, len(neighbors))
	for _, neigh := range neighbors {
		// Only include STALE neighbors that need refreshing
		if neigh.State&netlink.NUD_STALE == 0 {
			continue
		}

		// Skip entries without MAC address as they won't generate type 2 routes
		if len(neigh.HardwareAddr) == 0 {
			continue
		}

		// Skip entries without IP address
		if neigh.IP == nil {
			continue
		}

		// Skip link-local addresses — they are not advertised as EVPN
		// Type-2 routes, and the bridge typically has no IPv6 address
		// to send ICMPv6 pings from.
		if neigh.IP.IsLinkLocalUnicast() {
			continue
		}

		staleNeighbors = append(staleNeighbors, neigh)
	}

	return staleNeighbors, nil
}
