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

// StageSRv6Isolation prepares both sources' exception pairs before FRR can
// change the namespace source. It does not install guards or retire old rules.
func StageSRv6Isolation(targetNS string, vpns []L3VPNParams, source net.IP, tenantVRFs ...string) error {
	if err := CheckSRv6IsolationPlan(targetNS, vpns, source, tenantVRFs...); err != nil {
		return err
	}
	domains := map[string]SRv6Isolation{}
	for _, vpn := range vpns {
		if vpn.RoutingIsolation != nil {
			domains[vpn.VRF] = *vpn.RoutingIsolation
		}
	}
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
		if err := cleanupOrphanVRFDefaultGuards(); err != nil {
			return err
		}
		isolatedSource, table, err := isolationDomainSourceAndTable(domains)
		if err != nil {
			return err
		}
		if isolatedSource != nil {
			if source != nil && !source.Equal(isolatedSource) {
				return fmt.Errorf("SRv6 isolation source differs from FRR encapsulation source")
			}
			source = isolatedSource
			if err := requireIsolationTableUnowned(table); err != nil {
				return err
			}
		}
		current, err := listIsolationPolicyRules()
		if err != nil {
			return err
		}
		desired, current, err := planStagedIsolationRules(vpns, domains, source, current)
		if err != nil {
			return err
		}
		if len(desired) == 0 {
			return nil
		}
		return stageIsolationRules(current, desired, netlink.RuleAdd)
	})
}

// CheckSRv6IsolationPlan detects policy conflicts before FRR changes the
// namespace's encapsulation source. It does not create VRFs or mutate rules.
func CheckSRv6IsolationPlan(targetNS string, vpns []L3VPNParams, source net.IP, tenantVRFs ...string) error {
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
		isolatedSource, table, err := isolationDomainSourceAndTable(domains)
		if err != nil {
			return err
		}
		if isolatedSource != nil {
			if source != nil && !source.Equal(isolatedSource) {
				return fmt.Errorf("SRv6 isolation source differs from FRR encapsulation source")
			}
			source = isolatedSource
			if err := checkIsolationTableOwners(domains, tenantVRFs, table); err != nil {
				return err
			}
		}
		current, err := listIsolationPolicyRules()
		if err != nil {
			return err
		}
		_, _, err = planStagedIsolationRules(vpns, domains, source, current)
		if err != nil {
			return err
		}
		return nil
	})
}

func planStagedIsolationRules(vpns []L3VPNParams, domains map[string]SRv6Isolation, source net.IP,
	current []isolationPolicyRule,
) ([]netlink.Rule, []isolationPolicyRule, error) {
	guarded, err := guardedLegacyVPNs(vpns, current)
	if err != nil {
		return nil, nil, err
	}
	if len(domains) == 0 && len(guarded) == 0 {
		return nil, current, nil
	}
	if err := checkIsolationRuleOwners(current); err != nil {
		return nil, nil, err
	}
	if isolationL3MDevPriority(current) == 0 {
		current = append(current, isolationPolicyRule{rule: netlink.Rule{Priority: 1000}, l3mdev: true})
	}
	legacy, err := legacyIsolationRulesForSource(guarded, current, source)
	if err != nil {
		return nil, nil, err
	}
	desired, err := planIsolationPriorities(uniqueIsolationPairs(desiredIsolationRules(domains), legacy), current)
	return desired, current, err
}

// ReconcileSRv6Isolation configures namespace-scoped SRv6 underlay exceptions.
// Empty state only cleans up owned rules; it needs neither SRv6 nor l3mdev.
func ReconcileSRv6Isolation(targetNS string, vpns []L3VPNParams, tenantVRFs ...string) error {
	return reconcileSRv6IsolationInNamespace(targetNS, vpns, false, true, nil, tenantVRFs)
}

// PrepareSRv6Isolation keeps exceptions for retained VPNs that are changing
// to legacy mode until their guards have been removed by attachment setup.
// remainingSource is the source still configured by FRR after isolation retires.
func PrepareSRv6Isolation(targetNS string, vpns []L3VPNParams, remainingSource net.IP, tenantVRFs ...string) error {
	return reconcileSRv6IsolationInNamespace(targetNS, vpns, true, true, remainingSource, tenantVRFs)
}

// FinishSRv6Isolation retires exceptions for legacy VRFs whose guards are gone,
// retaining exceptions for transitions that still need their guards.
func FinishSRv6Isolation(targetNS string, vpns []L3VPNParams, remainingSource net.IP, tenantVRFs ...string) error {
	return reconcileSRv6IsolationInNamespace(targetNS, vpns, true, false, remainingSource, tenantVRFs)
}

func reconcileSRv6IsolationInNamespace(targetNS string, vpns []L3VPNParams, retainLegacy, installGuards bool,
	remainingSource net.IP, tenantVRFs []string,
) error {
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
		return reconcileSRv6Isolation(vpns, tenantVRFs, retainLegacy, installGuards, remainingSource)
	})
}

func reconcileSRv6Isolation(vpns []L3VPNParams, tenantVRFs []string, retainLegacy, installGuards bool,
	remainingSource net.IP,
) error {
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
	if retainLegacy {
		desired, source, err = retainLegacyRules(vpns, current, desired, source)
		if err != nil {
			return err
		}
	}
	if err := applyIsolationRules(current, desired, source, isolationRuleOperations{
		add: netlink.RuleAdd, remove: netlink.RuleDel,
		getSource: getSRv6TunnelSource, setSource: setSRv6TunnelSource,
		remainingSource: remainingSource,
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
	if !installGuards {
		return nil
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

func retainLegacyRules(vpns []L3VPNParams, current []isolationPolicyRule, desired []netlink.Rule,
	source net.IP,
) ([]netlink.Rule, net.IP, error) {
	guarded, err := guardedLegacyVPNs(vpns, current)
	if err != nil {
		return nil, nil, err
	}
	if len(guarded) == 0 {
		return desired, source, nil
	}
	if source == nil {
		source, err = getSRv6TunnelSource()
		if err != nil {
			return nil, nil, err
		}
	}
	needed, err := legacyIsolationRulesForSource(guarded, current, source)
	if err != nil {
		return nil, nil, err
	}
	retained := make([]netlink.Rule, 0, len(needed))
	for i := 0; i < len(needed); i += 2 {
		priority := existingIsolationPair(needed[i], needed[i+1], current)
		if priority == 0 {
			return nil, nil, fmt.Errorf("guarded legacy VPN lacks a complete SRv6 exception for source %s", source)
		}
		needed[i].Priority, needed[i+1].Priority = priority, priority+1
		retained = append(retained, needed[i], needed[i+1])
	}
	if err := checkRetainedIsolationPriorities(desired, retained); err != nil {
		return nil, nil, err
	}
	for _, rule := range retained {
		if !containsIsolationRule(desired, rule) {
			desired = append(desired, rule)
		}
	}
	return desired, source, nil
}

func guardedLegacyVPNs(vpns []L3VPNParams, current []isolationPolicyRule) ([]L3VPNParams, error) {
	var guarded []L3VPNParams
	for _, vpn := range vpns {
		if vpn.RoutingIsolation != nil {
			continue
		}
		if !slices.ContainsFunc(current, func(entry isolationPolicyRule) bool {
			return ownedIsolationPolicyRule(entry) && entry.rule.IifName == vpn.VRF
		}) {
			continue
		}
		vrf, err := lookupVRF(vpn.VRF)
		var missing netlink.LinkNotFoundError
		if errors.As(err, &missing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
			routes, err := listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))
			if family == netlink.FAMILY_V6 && errors.Is(err, unix.EAFNOSUPPORT) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if len(routes) > 0 {
				guarded = append(guarded, vpn)
				break
			}
		}
	}
	return guarded, nil
}

func checkRetainedIsolationPriorities(planned, retained []netlink.Rule) error {
	for _, rule := range retained {
		for _, next := range planned {
			if next.Priority == rule.Priority && isolationRuleKey(next) != isolationRuleKey(rule) {
				return fmt.Errorf("retained SRv6 rule conflicts with new priority %d", rule.Priority)
			}
		}
	}
	return nil
}

// Use a complete existing pair as the selector template for each guarded VRF.
// A pair for the requested source takes precedence over an older staged source.
func legacyIsolationRulesForSource(vpns []L3VPNParams, current []isolationPolicyRule,
	source net.IP,
) ([]netlink.Rule, error) {
	if len(vpns) == 0 {
		return nil, nil
	}
	if source == nil || source.Equal(net.IPv6zero) {
		return nil, fmt.Errorf("guarded legacy VPN needs an SRv6 encapsulation source")
	}
	var result []netlink.Rule
	for _, vpn := range vpns {
		selected := map[string]netlink.Rule{}
		var selectors []string
		for _, entry := range current {
			lookup := entry.rule
			if !ownedIsolationPolicyRule(entry) || lookup.IifName != vpn.VRF {
				continue
			}
			selectors = append(selectors, lookup.Dst.String())
			if !completeLegacyIsolationPair(lookup, current) {
				continue
			}
			key := lookup.Dst.String()
			previous, exists := selected[key]
			if !exists || lookup.Src.IP.Equal(source) && !previous.Src.IP.Equal(source) ||
				lookup.Src.IP.Equal(previous.Src.IP) && lookup.Priority < previous.Priority {
				selected[key] = lookup
			}
		}
		slices.Sort(selectors)
		for _, selector := range slices.Compact(selectors) {
			if _, ok := selected[selector]; !ok {
				return nil, fmt.Errorf("guarded legacy VPN %s lacks a complete SRv6 exception pair for locator %s", vpn.VRF, selector)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("guarded legacy VPN %s has no complete SRv6 exception pair", vpn.VRF)
		}
		for _, lookup := range selected {
			result = append(result, desiredIsolationRules(map[string]SRv6Isolation{
				vpn.VRF: {Source: source.String(), UnderlayTable: uint32(lookup.Table),
					RemoteLocatorPrefixes: []string{lookup.Dst.String()}},
			})...)
		}
	}
	return uniqueIsolationPairs(result, nil), nil
}

func completeLegacyIsolationPair(lookup netlink.Rule, current []isolationPolicyRule) bool {
	if lookup.Type != unix.FR_ACT_TO_TBL {
		return false
	}
	terminal := lookup
	terminal.Table, terminal.Type = 0, unix.FR_ACT_UNREACHABLE
	if existingIsolationPair(lookup, terminal, current) == 0 {
		return false
	}
	lookup.IifName, terminal.IifName = "lo", "lo"
	return existingIsolationPair(lookup, terminal, current) != 0
}

func uniqueIsolationPairs(first, second []netlink.Rule) []netlink.Rule {
	pairs := map[string][2]netlink.Rule{}
	for _, rules := range [][]netlink.Rule{first, second} {
		for i := 0; i < len(rules); i += 2 {
			pairs[isolationRuleKey(rules[i])] = [2]netlink.Rule{rules[i], rules[i+1]}
		}
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]netlink.Rule, 0, 2*len(keys))
	for _, key := range keys {
		result = append(result, pairs[key][0], pairs[key][1])
	}
	return result
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
	if err := requireIsolationTableUnowned(table); err != nil {
		return nil, err
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

func requireIsolationTableUnowned(table uint32) error {
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("inspect underlay table ownership: %w", err)
	}
	for _, link := range links {
		if vrf, ok := link.(*netlink.Vrf); ok && vrf.Table == table {
			return &VRFTableOwnershipError{Table: table, VRF: vrf.Name}
		}
	}
	return nil
}

type isolationRuleOperations struct {
	remainingSource net.IP
	add             func(*netlink.Rule) error
	remove          func(*netlink.Rule) error
	getSource       func() (net.IP, error)
	setSource       func(net.IP) error
}

func applyIsolationRules(current []isolationPolicyRule, desired []netlink.Rule, source net.IP,
	ops isolationRuleOperations,
) error {
	obsolete := obsoleteIsolationRules(current, desired)
	// Stage each new lookup behind its terminal before switching the source.
	// Keep old pairs until the replacement can handle encapsulated traffic.
	if err := stageIsolationRules(current, desired, ops.add); err != nil {
		return err
	}
	if source != nil {
		if err := ensureSRv6TunnelSource(source, ops); err != nil {
			return err
		}
	}
	if err := deleteIsolationRules(obsolete, unix.FR_ACT_TO_TBL, ops.remove); err != nil {
		return err
	}
	if source == nil {
		if err := resetRetiredSRv6Source(current, ops); err != nil {
			return err
		}
	}
	return deleteIsolationRules(obsolete, unix.FR_ACT_UNREACHABLE, ops.remove)
}

func stageIsolationRules(current []isolationPolicyRule, desired []netlink.Rule, add func(*netlink.Rule) error) error {
	missing := missingIsolationRules(current, desired)
	if err := addIsolationRules(missing, unix.FR_ACT_UNREACHABLE, add); err != nil {
		return err
	}
	return addIsolationRules(missing, unix.FR_ACT_TO_TBL, add)
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
			source := ops.remainingSource
			if source == nil {
				source = net.IPv6zero
			}
			return ensureSRv6TunnelSource(source, ops)
		}
	}
	return nil
}

func validateSRv6Decapsulation(vrf *netlink.Vrf, config SRv6Isolation) error {
	if config.UnderlayTable == unix.RT_TABLE_MAIN {
		return nil
	}
	routes, err := listSRv6DecapsulationRoutes(func() ([]netlink.Route, error) {
		return netlink.RouteListFiltered(netlink.FAMILY_V6,
			&netlink.Route{Table: unix.RT_TABLE_UNSPEC}, netlink.RT_FILTER_TABLE)
	})
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

func listSRv6DecapsulationRoutes(list func() ([]netlink.Route, error)) ([]netlink.Route, error) {
	const dumpAttempts = 3
	var routes []netlink.Route
	var err error
	for range dumpAttempts {
		routes, err = list()
		if !errors.Is(err, netlink.ErrDumpInterrupted) {
			return routes, err
		}
	}
	return nil, err
}

func checkIsolationRuleOwners(current []isolationPolicyRule) error {
	for _, entry := range current {
		if entry.rule.Protocol == uint8(vrfGuardProtocol) && !ownedIsolationPolicyRule(entry) {
			return fmt.Errorf("conflicting policy rule uses reserved OpenPERouter protocol %d", vrfGuardProtocol)
		}
	}
	return nil
}

func isolationDomainSourceAndTable(domains map[string]SRv6Isolation) (net.IP, uint32, error) {
	var source net.IP
	var table uint32
	for _, config := range domains {
		if err := config.Validate(); err != nil {
			return nil, 0, err
		}
		if source != nil && (!source.Equal(net.ParseIP(config.Source)) || table != config.UnderlayTable) {
			return nil, 0, fmt.Errorf("SRv6 exceptions in one namespace must share source and underlay table")
		}
		source, table = net.ParseIP(config.Source), config.UnderlayTable
	}
	return source, table, nil
}

func checkIsolationTableOwners(domains map[string]SRv6Isolation, tenants []string, table uint32) error {
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("inspect underlay table ownership: %w", err)
	}
	for _, link := range links {
		vrf, ok := link.(*netlink.Vrf)
		if !ok {
			continue
		}
		if config, isolated := domains[vrf.Name]; isolated {
			if err := validateSRv6Decapsulation(vrf, config); err != nil {
				return err
			}
		}
		if vrf.Table != table {
			continue
		}
		if slices.Contains(tenants, vrf.Name) || vrf.Alias != managedVRFAlias {
			return &VRFTableOwnershipError{Table: table, VRF: vrf.Name}
		}
		// Configure can remove a confirmed obsolete managed owner.
	}
	return nil
}
