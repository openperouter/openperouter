// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	managedVRFAlias = "openperouter:tenant-vrf"
	srv6VRF         = "srv6VRF"
	evpnVRF         = "evpnVRF"
	guardedSRv6VRF  = "guardedSRv6VRF"
)

var allocateVRFTable = findFreeRoutingTableID

// lookupVRF finds an existing VRF by name and returns an error if it
// does not exist or is not a VRF.
func lookupVRF(name string) (*netlink.Vrf, error) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("could not find vrf %s: %w", name, err)
	}
	vrf, ok := link.(*netlink.Vrf)
	if !ok {
		return nil, fmt.Errorf("link %s exists but is not a VRF", name)
	}
	return vrf, nil
}

// setupVRF creates a new VRF and sets it up.
func setupVRF(name string, opts ...string) error {
	return setupVRFWithSnapshot(name, nil, opts...)
}

func setupVRFWithSnapshot(name string, snapshot *vrfGuardSnapshot, opts ...string) error {
	vrfGuardMu.Lock()
	defer vrfGuardMu.Unlock()
	if slices.Contains(opts, srv6VRF) && slices.Contains(opts, evpnVRF) && !slices.Contains(opts, guardedSRv6VRF) {
		return fmt.Errorf("VRF %s cannot use both EVPN isolation and SRv6", name)
	}
	vrf, err := createVRF(name)
	if err != nil {
		return err
	}

	if slices.Contains(opts, srv6VRF) {
		if err := setupSRv6VRF(vrf, slices.Contains(opts, guardedSRv6VRF)); err != nil {
			return err
		}
	}
	if slices.Contains(opts, evpnVRF) || slices.Contains(opts, guardedSRv6VRF) {
		if err := ensureVRFDefaultGuardsWithSnapshot(vrf, snapshot); err != nil {
			return &VRFGuardError{VRF: name, Err: err}
		}
	}

	err = linkSetUp(vrf)
	if err != nil {
		return fmt.Errorf("could not set link up for VRF %s: %v", name, err)
	}

	// A desired VRF successfully provisioned by this controller is managed even
	// when it was created before aliases were introduced. Never replace another
	// owner's explicit alias.
	if vrf.Alias == "" {
		if err := netlink.LinkSetAlias(vrf, managedVRFAlias); err != nil {
			return &VRFGuardError{VRF: name, Err: fmt.Errorf("mark configured VRF %s: %w", name, err)}
		}
	}
	return nil
}

func createVRF(name string, excludedTables ...uint32) (*netlink.Vrf, error) {
	link, err := netlink.LinkByName(name)
	var missing netlink.LinkNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, fmt.Errorf("could not get link by name %s: %w", name, err)
	}
	if vrf, ok := link.(*netlink.Vrf); ok {
		return vrf, nil
	}
	tableID, err := allocateVRFTable(excludedTables...)
	if err != nil {
		return nil, err
	}
	if link != nil {
		if err := netlink.LinkDel(link); err != nil {
			return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
		}
	}
	return addVRF(name, tableID)
}

func addVRF(name string, tableID uint32) (*netlink.Vrf, error) {
	toCreate := &netlink.Vrf{
		LinkAttrs: netlink.LinkAttrs{Name: name},
		Table:     tableID,
	}
	if err := netlink.LinkAdd(toCreate); err != nil {
		return nil, fmt.Errorf("could not add VRF %s: %v", name, err)
	}
	if err := netlink.LinkSetAlias(toCreate, managedVRFAlias); err != nil {
		return nil, fmt.Errorf("mark managed VRF %s: %w", name, err)
	}
	return toCreate, nil
}

func findFreeRoutingTableID(excludedTables ...uint32) (uint32, error) {
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
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		if err := collectRoutingTableIDs(family, takenTables); err != nil {
			return 0, err
		}
	}

	for res := uint32(1); res < math.MaxUint32; res++ {
		if reservedVRFTable(res) || slices.Contains(excludedTables, res) {
			continue
		}
		if _, ok := takenTables[res]; !ok {
			return res, nil
		}
	}
	return 0, fmt.Errorf("findFreeRoutingTableID: Failed to find an available routing id")
}

func collectRoutingTableIDs(family int, taken map[uint32]struct{}) error {
	rules, err := listRoutingTableRules(family, netlink.RuleList)
	if family == netlink.FAMILY_V6 && errors.Is(err, unix.EAFNOSUPPORT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list policy rules for routing table allocation: %w", err)
	}
	for _, rule := range rules {
		if rule.Table > 0 {
			taken[uint32(rule.Table)] = struct{}{}
		}
	}
	const dumpAttempts = 3
	var routes []netlink.Route
	for range dumpAttempts {
		routes, err = netlink.RouteListFiltered(family,
			&netlink.Route{Table: unix.RT_TABLE_UNSPEC}, netlink.RT_FILTER_TABLE)
		if !errors.Is(err, netlink.ErrDumpInterrupted) {
			break
		}
	}
	if family == netlink.FAMILY_V6 && errors.Is(err, unix.EAFNOSUPPORT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list routes for routing table allocation: %w", err)
	}
	for _, route := range routes {
		if route.Table > 0 {
			taken[uint32(route.Table)] = struct{}{}
		}
	}
	return nil
}

func setupSRv6VRF(vrf *netlink.Vrf, guarded bool) error {
	// VRF strict mode is needed when using SRV6 and we need to do this as close
	// to VRF creation as possible to minimize the risk of rejected "B>r"
	// routes in BGP. Likewise, we must disable the RPFilter for SRv6 setups.
	if err := sysctl.Ensure(sysctl.EnableVRFStrictMode()); err != nil {
		return fmt.Errorf("failed to ensure VRF strict mode after adding VRF %s: %w", vrf.Name, err)
	}
	if err := sysctl.Ensure(sysctl.DisableRPFilter(vrf.Name)); err != nil {
		return fmt.Errorf("failed to disable rp_filter after adding VRF %s: %w", vrf.Name, err)
	}
	if guarded {
		return nil
	}
	if err := removeVRFDefaultGuards(vrf); err != nil {
		return &VRFGuardError{VRF: vrf.Name, Err: err}
	}
	return nil
}

func listRoutingTableRules(family int, list func(int) ([]netlink.Rule, error)) ([]netlink.Rule, error) {
	const attempts = 3
	var rules []netlink.Rule
	var err error
	for range attempts {
		rules, err = list(family)
		if !errors.Is(err, netlink.ErrDumpInterrupted) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return rules, nil
}
