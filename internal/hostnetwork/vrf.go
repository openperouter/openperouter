// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"math"
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Route priority (aka metric) value that must be set on default unreachable routes.
// This value is the one recommended by the Linux kernel documentation, must be higher than any other route priority used in the system, to ensure that it is only used when no other route matches.
// See https://docs.kernel.org/networking/vrf.html for more details.
const UNREACHABLE_ROUTE_PRIORITY = 4278198272

var defaultUnreachableRoutes = []struct {
	family int
	dst    *net.IPNet
}{
	{family: netlink.FAMILY_V4, dst: &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}},
	{family: netlink.FAMILY_V6, dst: &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}},
}

// setupVRF creates a new VRF and sets it up.
func setupVRF(name string) (*netlink.Vrf, error) {
	vrf, err := createVRF(name)
	if err != nil {
		return nil, err
	}

	err = linkSetUp(vrf)
	if err != nil {
		return nil, fmt.Errorf("could not set link up for VRF %s: %v", name, err)
	}

	if err := ensureUnreachableDefaultRoutes(vrf); err != nil {
		return nil, err
	}

	return vrf, nil
}

func hasUnreachableDefaultRoute(vrfTable int, family int) (bool, error) {
	// Note: When listing routes, netlink represents the default route with Dst == nil.
	// So we need to filter with Dst: nil to match the default route, and it makes the function agnostic to the IP version.
	routeFilter := &netlink.Route{
		Dst:      nil,
		Table:    vrfTable,
		Type:     unix.RTN_UNREACHABLE,
		Priority: UNREACHABLE_ROUTE_PRIORITY,
	}
	filterMask := netlink.RT_FILTER_DST | netlink.RT_FILTER_TABLE | netlink.RT_FILTER_TYPE | netlink.RT_FILTER_PRIORITY
	routes, err := netlink.RouteListFiltered(family, routeFilter, filterMask)
	if err != nil {
		return false, fmt.Errorf("failed to get routes: %w", err)
	}
	return len(routes) > 0, nil
}

// ensureUnreachableDefaultRoutes adds default unreachable routes for both IPv4 and IPv6 to the given VRF, if they do not already exist.
// This is needed to ensure that packets that do not match any entry in the VRF table are dropped, instead of being routed to the next table referenced in `ip rule`.
func ensureUnreachableDefaultRoutes(vrf *netlink.Vrf) error {
	for _, r := range defaultUnreachableRoutes {
		found, err := hasUnreachableDefaultRoute(int(vrf.Table), r.family)
		if err != nil {
			return fmt.Errorf("failed to get unreachable default route from VRF %s (table %d): %w", vrf.Name, vrf.Table, err)
		}
		if found {
			continue
		}

		route := &netlink.Route{
			Dst:      r.dst,
			Family:   r.family,
			Table:    int(vrf.Table),
			Type:     unix.RTN_UNREACHABLE,
			Priority: UNREACHABLE_ROUTE_PRIORITY,
		}
		if err := netlink.RouteReplace(route); err != nil {
			return fmt.Errorf("failed to add unreachable default route to VRF %s (table %d): %w", vrf.Name, vrf.Table, err)
		}
	}

	return nil
}

func createVRF(name string) (*netlink.Vrf, error) {
	tableID, err := findFreeRoutingTableID()
	if err != nil {
		return nil, err
	}

	toCreate := &netlink.Vrf{
		LinkAttrs: netlink.LinkAttrs{Name: name},
		Table:     tableID,
	}

	link, err := netlink.LinkByName(name)

	// does not exist. Let's create.
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		if err := netlink.LinkAdd(toCreate); err != nil {
			return nil, fmt.Errorf("could not add VRF %s: %v", name, err)
		}
		return toCreate, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not get link by name %s: %v", name, err)
	}

	// exists
	vrf, ok := link.(*netlink.Vrf)
	if ok {
		return vrf, nil
	}

	// exists but not of the right type, let's remove and recreate.
	err = netlink.LinkDel(link)
	if err != nil {
		return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
	}
	if err := netlink.LinkAdd(toCreate); err != nil {
		return nil, fmt.Errorf("could not add VRF %s: %v", name, err)
	}
	return toCreate, nil
}

func findFreeRoutingTableID() (uint32, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return 0, fmt.Errorf("findFreeRoutingTableID: Failed to find links %v", err)
	}

	takenTables := make(map[uint32]struct{}, len(links))
	for _, l := range links {
		if vrf, ok := l.(*netlink.Vrf); ok {
			takenTables[vrf.Table] = struct{}{}
		}
	}

	for res := uint32(1); res < math.MaxUint32; res++ {
		if _, ok := takenTables[res]; !ok {
			return res, nil
		}
	}
	return 0, fmt.Errorf("findFreeRoutingTableID: Failed to find an available routing id")
}
