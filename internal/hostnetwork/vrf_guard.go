// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"net"

	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	// Linux VRF's recommended metric is interpreted by FRR as [255/8192].
	vrfGuardPriority = 4278198272
	// A private protocol, distinct from zebra/BGP/staticd. FRR treats it as a kernel route.
	vrfGuardProtocol netlink.RouteProtocol = 242
)

var (
	guardRouteList = netlink.RouteListFiltered
	guardRouteAdd  = netlink.RouteAdd
	guardRouteDel  = netlink.RouteDel
	guardLinkList  = netlink.LinkList
	guardIPv6Read  = func(device string) (string, error) {
		return sysctl.Read(sysctl.Sysctl{Path: "net/ipv6/conf/" + device + "/disable_ipv6"})
	}
)

// VRFGuardError identifies protection failures whose existing attachments must
// survive cleanup. Setup stops before it can attach any new tenant ports.
type VRFGuardError struct {
	VRF string
	Err error
}

func (e *VRFGuardError) Error() string { return e.Err.Error() }
func (e *VRFGuardError) Unwrap() error { return e.Err }

type vrfGuardSnapshot struct {
	routes map[int][]netlink.Route
}

func ensureVRFDefaultGuards(vrf *netlink.Vrf) error {
	return ensureVRFDefaultGuardsWithSnapshot(vrf, nil)
}

func ensureVRFDefaultGuardsWithSnapshot(vrf *netlink.Vrf, snapshot *vrfGuardSnapshot) error {
	if reservedVRFTable(vrf.Table) {
		return fmt.Errorf("refusing default guards in reserved routing table %d for VRF %s", vrf.Table, vrf.Name)
	}
	if err := ensureVRFDefaultGuard(vrf, netlink.FAMILY_V4, snapshot); err != nil {
		return err
	}
	enabled, err := namespaceIPv6Enabled()
	if err != nil {
		return fmt.Errorf("read IPv6 availability for VRF %s: %w", vrf.Name, err)
	}
	if !enabled {
		return nil
	}
	return ensureVRFDefaultGuard(vrf, netlink.FAMILY_V6, snapshot)
}

func removeVRFDefaultGuards(vrf *netlink.Vrf) error {
	stale, err := vrfDefaultGuardsToRemove(vrf)
	if err != nil {
		return err
	}
	return deleteVRFDefaultGuards(stale)
}

func vrfDefaultGuardsToRemove(vrf *netlink.Vrf) ([]netlink.Route, error) {
	var stale []netlink.Route
	// Obtain complete snapshots for both families before deleting any guard.
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))
		if family == netlink.FAMILY_V6 && errors.Is(err, unix.EAFNOSUPPORT) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("list stale default guards for VRF %s: %w", vrf.Name, err)
		}
		stale = append(stale, routes...)
	}
	return stale, nil
}

func deleteVRFDefaultGuards(routes []netlink.Route) error {
	for _, route := range routes {
		if err := guardRouteDel(&route); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("remove stale default guard for table %d: %w", route.Table, err)
		}
	}
	return nil
}

func ensureVRFDefaultGuard(vrf *netlink.Vrf, family int, snapshot *vrfGuardSnapshot) error {
	route := vrfDefaultGuard(vrf.Table, family)
	routes, err := snapshot.list(route)
	if err != nil {
		return fmt.Errorf("list default guard for VRF %s family %d: %w", vrf.Name, family, err)
	}
	if len(routes) > 0 {
		return nil
	}
	// EXCL is essential: REPLACE can overwrite somebody else's default with the same
	// priority. Even EEXIST is only success after verifying our exact ownership tuple.
	err = guardRouteAdd(&route)
	if errors.Is(err, unix.EEXIST) {
		routes, listErr := listVRFDefaultGuards(route)
		if listErr != nil {
			return fmt.Errorf("verify concurrent default guard for VRF %s: %w", vrf.Name, listErr)
		}
		if len(routes) > 0 {
			snapshot.record(family, routes...)
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("add default guard for VRF %s table %d family %d: %w", vrf.Name, vrf.Table, family, err)
	}
	snapshot.record(family, route)
	return nil
}

func namespaceIPv6Enabled() (bool, error) {
	disabled, err := guardIPv6Read("all")
	if err != nil {
		return false, err
	}
	// Missing sysctl means the kernel has no IPv6 support.
	if disabled == "" {
		return false, nil
	}
	if disabled != "0" && disabled != "1" {
		return false, fmt.Errorf("unexpected IPv6 disable_ipv6 value %q on all", disabled)
	}
	// Writing all.disable_ipv6 updates existing interfaces, but later interfaces
	// inherit default.disable_ipv6, and individual devices can be re-enabled.
	enabled, err := ipv6EnabledOnDevice("default")
	if err != nil || enabled {
		return enabled, err
	}
	links, err := guardLinkList()
	if err != nil {
		return false, fmt.Errorf("list interfaces for IPv6 availability: %w", err)
	}
	for _, link := range links {
		enabled, err := ipv6EnabledOnDevice(link.Attrs().Name)
		if err != nil || enabled {
			return enabled, err
		}
	}
	return false, nil
}

func ipv6EnabledOnDevice(device string) (bool, error) {
	disabled, err := guardIPv6Read(device)
	if err != nil {
		return false, fmt.Errorf("read IPv6 setting on %s: %w", device, err)
	}
	switch disabled {
	case "0":
		return true, nil
	case "1", "":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected IPv6 disable_ipv6 value %q on %s", disabled, device)
	}
}

func vrfDefaultGuard(table uint32, family int) netlink.Route {
	dst := &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	if family == netlink.FAMILY_V6 {
		dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	}
	return netlink.Route{
		Family: family, Dst: dst, Table: int(table),
		Type: unix.RTN_UNREACHABLE, Scope: netlink.SCOPE_UNIVERSE,
		Priority: vrfGuardPriority, Protocol: vrfGuardProtocol,
	}
}

func listVRFDefaultGuards(route netlink.Route) ([]netlink.Route, error) {
	// RouteListFiltered uses nil Dst to match the family's default prefix.
	filter := route
	filter.Dst = nil
	// netlink omits non-main tables unless RT_FILTER_TABLE is set, even when
	// RT_TABLE_UNSPEC requests an all-table snapshot.
	mask := netlink.RT_FILTER_TABLE | netlink.RT_FILTER_DST | netlink.RT_FILTER_TYPE |
		netlink.RT_FILTER_PRIORITY | netlink.RT_FILTER_PROTOCOL
	const dumpAttempts = 3
	var routes []netlink.Route
	var err error
	for range dumpAttempts {
		routes, err = guardRouteList(route.Family, &filter, mask)
		if !errors.Is(err, netlink.ErrDumpInterrupted) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	// netlink v1.3.1 ignores RT_FILTER_PRIORITY when filtering route dumps.
	var owned []netlink.Route
	for _, candidate := range routes {
		if candidate.Priority == route.Priority && candidate.Tos == 0 {
			owned = append(owned, candidate)
		}
	}
	return owned, nil
}

func reservedVRFTable(table uint32) bool {
	return table == unix.RT_TABLE_UNSPEC || table >= unix.RT_TABLE_DEFAULT && table <= unix.RT_TABLE_LOCAL
}

func (snapshot *vrfGuardSnapshot) list(route netlink.Route) ([]netlink.Route, error) {
	if snapshot == nil {
		return listVRFDefaultGuards(route)
	}
	if snapshot.routes == nil {
		snapshot.routes = map[int][]netlink.Route{}
	}
	routes, ok := snapshot.routes[route.Family]
	if !ok {
		filter := route
		filter.Table = unix.RT_TABLE_UNSPEC
		var err error
		routes, err = listVRFDefaultGuards(filter)
		if err != nil {
			return nil, err
		}
		snapshot.routes[route.Family] = routes
	}
	var found []netlink.Route
	for _, candidate := range routes {
		if candidate.Table == route.Table {
			found = append(found, candidate)
		}
	}
	return found, nil
}

func (snapshot *vrfGuardSnapshot) record(family int, routes ...netlink.Route) {
	if snapshot == nil {
		return
	}
	snapshot.routes[family] = append(snapshot.routes[family], routes...)
}
