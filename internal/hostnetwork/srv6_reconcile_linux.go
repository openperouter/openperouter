// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

var isolationReconcileMu sync.Mutex

// ReconcileSRv6Isolation configures namespace-scoped SRv6 underlay exceptions.
// Empty state only cleans up owned rules; it needs neither SRv6 nor l3mdev.
func ReconcileSRv6Isolation(targetNS string, vpns []L3VPNParams, tenantVRFs ...string) error {
	isolationReconcileMu.Lock()
	defer isolationReconcileMu.Unlock()
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("open SRv6 isolation namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("close SRv6 namespace", "err", err)
		}
	}()
	return netnamespace.In(ns, func() error {
		return reconcileSRv6Isolation(vpns, tenantVRFs)
	})
}

func reconcileSRv6Isolation(vpns []L3VPNParams, tenantVRFs []string) error {
	if err := validateIsolationVPNModes(vpns); err != nil {
		return err
	}
	domains := map[string]SRv6Isolation{}
	for _, vpn := range vpns {
		tenantVRFs = append(tenantVRFs, vpn.VRF)
		if vpn.RoutingIsolation != nil {
			domains[vpn.VRF] = *vpn.RoutingIsolation
		}
	}
	slices.Sort(tenantVRFs)
	tenantVRFs = slices.Compact(tenantVRFs)
	source, err := prepareIsolationVRFs(domains, tenantVRFs)
	if err != nil {
		return err
	}
	current, err := listIsolationPolicyRules()
	if len(domains) == 0 && errors.Is(err, unix.EAFNOSUPPORT) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range current {
		if len(domains) > 0 && entry.rule.Protocol == uint8(vrfGuardProtocol) && !ownedIsolationPolicyRule(entry) {
			return fmt.Errorf("conflicting policy rule uses reserved OpenPERouter protocol %d", vrfGuardProtocol)
		}
	}

	desired, err := planIsolationPriorities(desiredIsolationRules(domains), current)
	if err != nil {
		return err
	}
	if err := applyIsolationRules(current, desired, source, isolationRuleOperations{
		add: netlink.RuleAdd, remove: netlink.RuleDel,
		getSource: getSRv6TunnelSource, setSource: setSRv6TunnelSource,
	}); err != nil {
		return err
	}
	actual, err := listIsolationPolicyRules()
	if err != nil {
		return err
	}
	if !isolationPoliciesEqual(desired, actual) {
		return fmt.Errorf("SRv6 policy conflict after netlink update")
	}
	// Existing legacy VPNs stay unchanged until planning and all exceptions succeed.
	snapshot := &vrfGuardSnapshot{}
	for name := range domains {
		if err := setupVRFWithSnapshot(name, snapshot, srv6VRF, guardedSRv6VRF); err != nil {
			return err
		}
	}
	return nil
}

func validateIsolationVPNModes(vpns []L3VPNParams) error {
	legacy := false
	dedicated := false
	for _, vpn := range vpns {
		if vpn.RoutingIsolation == nil {
			legacy = true
			continue
		}
		if vpn.RoutingIsolation.UnderlayTable != unix.RT_TABLE_MAIN {
			dedicated = true
		}
	}
	if legacy && dedicated {
		return fmt.Errorf("legacy and dedicated-table SRv6 VPNs cannot share a namespace: local-output lookup cannot distinguish VPNs")
	}
	return nil
}

func prepareIsolationVRFs(domains map[string]SRv6Isolation, tenants []string) (net.IP, error) {
	var source net.IP
	var table uint32
	for name, config := range domains {
		if err := config.Validate(); err != nil {
			return nil, fmt.Errorf("VRF %s: %w", name, err)
		}
		next := net.ParseIP(config.Source)
		if source != nil && !source.Equal(next) {
			return nil, fmt.Errorf("SRv6 exceptions in one namespace must share source and underlay table")
		}
		if source != nil && table != config.UnderlayTable {
			return nil, fmt.Errorf("SRv6 exceptions in one namespace must share source and underlay table")
		}
		source, table = next, config.UnderlayTable
	}
	if source == nil {
		return nil, nil
	}
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("inspect underlay table ownership: %w", err)
	}
	for _, link := range links {
		if vrf, ok := link.(*netlink.Vrf); ok && vrf.Table == table {
			return nil, &VRFTableOwnershipError{Table: table, VRF: vrf.Name}
		}
	}
	// Exclude the explicit transport table from allocation of every pending tenant.
	for _, name := range tenants {
		vrf, err := createVRF(name, table)
		if err != nil {
			return nil, err
		}
		if vrf.Table == table {
			return nil, fmt.Errorf("underlayTable %d belongs to tenant %s", table, name)
		}
		config, ok := domains[name]
		if !ok {
			continue
		}
		if err := validateSRv6Decapsulation(vrf, config); err != nil {
			return nil, err
		}
	}
	return source, nil
}

type isolationRuleOperations struct {
	add       func(*netlink.Rule) error
	remove    func(*netlink.Rule) error
	getSource func() (net.IP, error)
	setSource func(net.IP) error
}

func applyIsolationRules(current []isolationPolicyRule, desired []netlink.Rule, source net.IP,
	ops isolationRuleOperations,
) error {
	missing := missingIsolationRules(current, desired)
	obsolete := obsoleteIsolationRules(current, desired)
	// Install terminal actions before lookups, retaining old terminal actions
	// until their lookups are gone. Failed updates do not create a fallback gap.
	if err := addIsolationRules(missing, unix.FR_ACT_UNREACHABLE, ops.add); err != nil {
		return err
	}
	if err := deleteIsolationRules(obsolete, unix.FR_ACT_TO_TBL, ops.remove); err != nil {
		return err
	}
	if source == nil {
		if err := resetRetiredSRv6Source(current, ops); err != nil {
			return err
		}
	} else if err := ensureSRv6TunnelSource(source, ops); err != nil {
		return err
	}
	if err := addIsolationRules(missing, unix.FR_ACT_TO_TBL, ops.add); err != nil {
		return err
	}
	return deleteIsolationRules(obsolete, unix.FR_ACT_UNREACHABLE, ops.remove)
}

func addIsolationRules(rules []netlink.Rule, action uint8, add func(*netlink.Rule) error) error {
	for _, r := range rules {
		if r.Type != action {
			continue
		}
		if err := add(&r); err != nil {
			return fmt.Errorf("add SRv6 policy rule: %w", err)
		}
	}
	return nil
}

func deleteIsolationRules(rules []netlink.Rule, action uint8, remove func(*netlink.Rule) error) error {
	for _, r := range rules {
		if r.Type != action {
			continue
		}
		if err := remove(&r); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("delete SRv6 policy rule: %w", err)
		}
	}
	return nil
}

func ensureSRv6TunnelSource(source net.IP, ops isolationRuleOperations) error {
	actual, err := ops.getSource()
	if err != nil {
		return err
	}
	if actual.Equal(source) {
		return nil
	}
	if err := ops.setSource(source); err != nil {
		return err
	}
	actual, err = ops.getSource()
	if err != nil {
		return err
	}
	if !source.Equal(actual) {
		return fmt.Errorf("SRv6 source readback mismatch: %s", actual)
	}
	return nil
}

func verifySRv6Isolation(name string, config SRv6Isolation) error {
	vrf, err := lookupVRF(name)
	if err != nil {
		return err
	}
	if err := validateSRv6Decapsulation(vrf, config); err != nil {
		return err
	}
	current, err := listIsolationPolicyRules()
	if err != nil {
		return err
	}
	before := isolationL3MDevPriority(current)
	desired := desiredIsolationRules(map[string]SRv6Isolation{name: config})
	for i := 0; i < len(desired); i += 2 {
		priority := existingIsolationPair(desired[i], desired[i+1], current)
		if priority <= 0 || priority+1 >= before {
			return fmt.Errorf("SRv6 exceptions for VRF %s must be reconciled before attaching ports", name)
		}
	}

	source, err := getSRv6TunnelSource()
	if err != nil {
		return err
	}
	if !source.Equal(net.ParseIP(config.Source)) {
		return fmt.Errorf("SRv6 source changed for VRF %s", name)
	}
	return nil
}

func resetRetiredSRv6Source(previous []isolationPolicyRule, ops isolationRuleOperations) error {
	owned := obsoleteIsolationRules(previous, nil)
	if len(owned) == 0 {
		return nil
	}
	actual, err := ops.getSource()
	if err != nil {
		return err
	}
	for _, rule := range owned {
		if actual.Equal(rule.Src.IP) {
			return ops.setSource(net.IPv6zero)
		}
	}
	return nil
}

func validateSRv6Decapsulation(vrf *netlink.Vrf, config SRv6Isolation) error {
	if config.UnderlayTable == unix.RT_TABLE_MAIN {
		return nil
	}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6,
		&netlink.Route{Table: unix.RT_TABLE_UNSPEC}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return fmt.Errorf("inspect SRv6 decapsulation routes: %w", err)
	}
	for _, route := range routes {
		local, ok := route.Encap.(*netlink.SEG6LocalEncap)
		if !ok {
			continue
		}
		if local.Action != nl.SEG6_LOCAL_ACTION_END_DT6 {
			continue
		}
		if !local.Flags[nl.SEG6_LOCAL_TABLE] {
			continue
		}
		if local.Table != int(vrf.Table) {
			continue
		}
		return fmt.Errorf("VRF %s uses legacy End.DT6 table lookup; legacy End.DT6 preserves underlay iif; "+
			"dedicated underlay lookup requires per-VRF SID", vrf.Name)
	}
	return nil
}
