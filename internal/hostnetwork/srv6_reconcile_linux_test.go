// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestApplyIsolationRulesPartialFailures(t *testing.T) {
	original := SRv6Isolation{
		Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"},
	}
	changed := SRv6Isolation{
		Source: "2001:db8::2", UnderlayTable: 702,
		RemoteLocatorPrefixes: []string{"fc00:20::/48", "fc00:30::/48"},
	}
	for _, tc := range []struct {
		name            string
		initial         map[string]SRv6Isolation
		source          net.IP
		remainingSource net.IP
		desired         map[string]SRv6Isolation
	}{
		{name: "first installation", source: net.IPv6zero, desired: map[string]SRv6Isolation{"red": original}},
		{
			name:    "change source table and locators",
			initial: map[string]SRv6Isolation{"red": original, "blue": original},
			desired: map[string]SRv6Isolation{"red": changed, "blue": changed},
		},
		{
			name:    "change table retaining source and terminals",
			initial: map[string]SRv6Isolation{"red": original},
			desired: map[string]SRv6Isolation{"red": {
				Source: original.Source, UnderlayTable: 702, RemoteLocatorPrefixes: original.RemoteLocatorPrefixes,
			}},
		},
		{
			name:    "remove one domain retaining shared rules",
			initial: map[string]SRv6Isolation{"red": original, "blue": original},
			desired: map[string]SRv6Isolation{"red": original},
		},
		{name: "remove last configuration", initial: map[string]SRv6Isolation{"red": original}},
		{name: "retire isolation restoring FRR source", initial: map[string]SRv6Isolation{"red": original},
			remainingSource: net.ParseIP("2001:db8::99")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source
			if source == nil {
				source = net.ParseIP(original.Source)
			}
			checkIsolationPartialFailures(t, tc.initial, tc.desired, source, tc.remainingSource)
		})
	}
}

func TestLegacyIsolationRequiresEveryLocatorPair(t *testing.T) {
	const nextSource = "2001:db8::2"
	config := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254,
		RemoteLocatorPrefixes: []string{"fc00:20::/48", "fc00:30::/48"}}
	for _, tc := range []struct {
		name        string
		iface       string
		action      uint8
		otherSource bool
		wantError   bool
	}{
		{name: "complete pairs"},
		{name: "missing tenant terminal", iface: "red", action: unix.FR_ACT_UNREACHABLE, wantError: true},
		{name: "missing tenant lookup", iface: "red", action: unix.FR_ACT_TO_TBL, wantError: true},
		{name: "missing shared terminal", iface: "lo", action: unix.FR_ACT_UNREACHABLE, wantError: true},
		{name: "missing shared lookup", iface: "lo", action: unix.FR_ACT_TO_TBL, wantError: true},
		{name: "complete alternative source", iface: "red", action: unix.FR_ACT_UNREACHABLE, otherSource: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := isolationFailureState(t, map[string]SRv6Isolation{"red": config})
			current = slices.DeleteFunc(current, func(entry isolationPolicyRule) bool {
				return ownedIsolationPolicyRule(entry) && entry.rule.IifName == tc.iface && entry.rule.Type == tc.action &&
					entry.rule.Dst.String() == "fc00:30::/48"
			})
			next := config
			next.Source = nextSource
			if tc.otherSource {
				additional := isolationFailureState(t, map[string]SRv6Isolation{"red": next})
				for _, entry := range additional {
					if ownedIsolationPolicyRule(entry) {
						entry.rule.Priority += 20
						current = append(current, entry)
					}
				}
			}
			got, err := legacyIsolationRulesForSource([]L3VPNParams{{VRF: "red"}}, current, net.ParseIP(next.Source))
			if tc.wantError {
				want := "guarded legacy VPN red lacks a complete SRv6 exception pair for locator fc00:30::/48"
				if err == nil || err.Error() != want || got != nil {
					t.Fatalf("got rules=%v, err=%v; want no rules and %q", got, err, want)
				}
				return
			}
			want := desiredIsolationRules(map[string]SRv6Isolation{"red": next})
			if err != nil || !reflect.DeepEqual(want, got) {
				t.Fatalf("got rules=%v, err=%v; want complete rules=%v", got, err, want)
			}
		})
	}
}

func TestStageIsolationRulesPartialFailures(t *testing.T) {
	previous := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701,
		RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	next := SRv6Isolation{Source: "2001:db8::2", UnderlayTable: 702,
		RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	for _, initial := range []map[string]SRv6Isolation{nil, {"red": previous}} {
		for failAt := 1; failAt <= 4; failAt++ {
			t.Run(fmt.Sprintf("existing_%t_failure_%d", initial != nil, failAt), func(t *testing.T) {
				injected := errors.New("stage failed")
				before := isolationFailureState(t, initial)
				kernel := &isolationFailureKernel{rules: slices.Clone(before), source: net.ParseIP(previous.Source),
					failAt: failAt, failure: injected}
				domains := map[string]SRv6Isolation{"red": next}
				if err := kernel.stage(domains); !errors.Is(err, injected) || len(kernel.trace) != failAt {
					t.Fatalf("stage must stop at failure %d: trace=%v, err=%v", failAt, kernel.trace, err)
				}
				kernel.failAt = 0
				if err := kernel.stage(domains); err != nil {
					t.Fatal(err)
				}
				for _, entry := range before {
					if !slices.ContainsFunc(kernel.rules, func(actual isolationPolicyRule) bool { return reflect.DeepEqual(entry, actual) }) {
						t.Fatalf("stage changed existing rule: %+v", entry)
					}
				}
				if !kernel.source.Equal(net.ParseIP(previous.Source)) {
					t.Fatal("stage changed source before FRR")
				}
				kernel.trace = nil
				if err := kernel.stage(domains); err != nil || len(kernel.trace) != 0 {
					t.Fatalf("stage retry not idempotent: %v, %v", kernel.trace, err)
				}
				kernel.reconcile(t, map[string]SRv6Isolation{"red": next})
			})
		}
	}
}

func TestListSRv6DecapsulationRoutesRetriesInterruptedDump(t *testing.T) {
	partial := []netlink.Route{{Table: 701}}
	complete := []netlink.Route{{Table: 702}}
	reads := 0
	routes, err := listSRv6DecapsulationRoutes(func() ([]netlink.Route, error) {
		reads++
		if reads == 1 {
			return partial, netlink.ErrDumpInterrupted
		}
		return complete, nil
	})
	if err != nil || !reflect.DeepEqual(routes, complete) || reads != 2 {
		t.Fatalf("retry used an interrupted snapshot: routes=%v, reads=%d, err=%v", routes, reads, err)
	}
	reads = 0
	routes, err = listSRv6DecapsulationRoutes(func() ([]netlink.Route, error) {
		reads++
		return partial, netlink.ErrDumpInterrupted
	})
	if !errors.Is(err, netlink.ErrDumpInterrupted) || routes != nil || reads != 3 {
		t.Fatalf("persistent interrupted dump was accepted: routes=%v, reads=%d, err=%v", routes, reads, err)
	}
}

func TestApplyIsolationRulesSourceReadbackMismatch(t *testing.T) {
	previous := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701,
		RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	config := SRv6Isolation{Source: "2001:db8::2", UnderlayTable: 702,
		RemoteLocatorPrefixes: previous.RemoteLocatorPrefixes}
	domains := map[string]SRv6Isolation{"red": config}
	kernel := &isolationFailureKernel{
		rules:  isolationFailureState(t, map[string]SRv6Isolation{"red": previous}),
		source: net.ParseIP(previous.Source), ignoreSourceSet: true,
	}
	if err := kernel.apply(domains); err == nil || !strings.Contains(err.Error(), "readback mismatch") {
		t.Fatal("source readback mismatch must stop removal of old rules")
	}
	for _, expected := range []SRv6Isolation{previous, config} {
		lookup := desiredIsolationRules(map[string]SRv6Isolation{"red": expected})[2]
		if !slices.ContainsFunc(kernel.rules, func(entry isolationPolicyRule) bool {
			return ownedIsolationPolicyRule(entry) && entry.rule.Type == unix.FR_ACT_TO_TBL &&
				isolationRuleKey(entry.rule) == isolationRuleKey(lookup)
		}) {
			t.Fatalf("lookup for source %s was not retained during failed switch", expected.Source)
		}
	}
	kernel.ignoreSourceSet = false
	kernel.reconcile(t, domains)
}

func TestApplyIsolationRulesStagesReplacementBeforeSourceSwitch(t *testing.T) {
	previous := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701,
		RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	next := SRv6Isolation{Source: "2001:db8::2", UnderlayTable: 702,
		RemoteLocatorPrefixes: previous.RemoteLocatorPrefixes}
	kernel := &isolationFailureKernel{
		rules:  isolationFailureState(t, map[string]SRv6Isolation{"red": previous}),
		source: net.ParseIP(previous.Source),
	}
	kernel.reconcile(t, map[string]SRv6Isolation{"red": next})
	firstSourceSet, firstOldDelete := -1, -1
	newLookups := 0
	for i, operation := range kernel.trace {
		if strings.HasPrefix(operation, "set source ") && firstSourceSet < 0 {
			firstSourceSet = i
		}
		if strings.HasPrefix(operation, "delete ") && firstOldDelete < 0 {
			firstOldDelete = i
		}
		if strings.HasPrefix(operation, "add 1 ") {
			if firstSourceSet >= 0 {
				t.Fatalf("lookup installed after source switch: %v", kernel.trace)
			}
			newLookups++
		}
	}
	if newLookups != 2 || firstSourceSet < 0 || firstOldDelete <= firstSourceSet {
		t.Fatalf("replacement was not staged before retiring old rules: %v", kernel.trace)
	}
}

func TestApplyIsolationRulesPreservesForeignSource(t *testing.T) {
	config := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	foreign := net.ParseIP("2001:db8::99")
	kernel := &isolationFailureKernel{
		rules: isolationFailureState(t, map[string]SRv6Isolation{"red": config}), source: foreign,
	}
	kernel.reconcile(t, nil)
	if !kernel.source.Equal(foreign) {
		t.Fatal("cleanup reset a foreign encapsulation source")
	}
}

func checkIsolationPartialFailures(t *testing.T, initialDomains, desiredDomains map[string]SRv6Isolation,
	source, remainingSource net.IP,
) {
	t.Helper()
	initial := isolationFailureState(t, initialDomains)
	baseline := &isolationFailureKernel{rules: slices.Clone(initial), source: source, remainingSource: remainingSource}
	baseline.reconcile(t, desiredDomains)
	retiredSource := remainingSource
	if retiredSource == nil {
		retiredSource = net.IPv6zero
	}
	if len(desiredDomains) == 0 && !baseline.source.Equal(retiredSource) {
		t.Fatalf("last configuration removal did not reset source: %s", baseline.source)
	}
	wantTrace := slices.Clone(baseline.trace)
	for failAt, operation := range wantTrace {
		t.Run(fmt.Sprintf("%02d_%s", failAt+1, operation), func(t *testing.T) {
			injected := errors.New("injected netlink failure")
			kernel := &isolationFailureKernel{
				rules: slices.Clone(initial), source: source, remainingSource: remainingSource,
				failAt: failAt + 1, failure: injected,
			}
			if err := kernel.apply(desiredDomains); !errors.Is(err, injected) {
				t.Fatalf("want propagated failure, got %v", err)
			}
			if !reflect.DeepEqual(kernel.trace, wantTrace[:failAt+1]) {
				t.Fatalf("operations continued after failure: %v", kernel.trace)
			}
			kernel.assertPreserved(t, initial, desiredDomains)
			kernel.failAt = 0
			kernel.reconcile(t, desiredDomains)
			if !kernel.source.Equal(baseline.source) {
				t.Fatalf("retry source got %s, want %s", kernel.source, baseline.source)
			}
			kernel.assertPreserved(t, initial, desiredDomains)
			// A second retry must neither mutate rules nor set the source.
			kernel.trace = nil
			kernel.reconcile(t, desiredDomains)
			for _, op := range kernel.trace {
				if op != "get source" {
					t.Fatalf("non-idempotent retry: %v", kernel.trace)
				}
			}
		})
	}
}

// Only the four mutation/source calls are injected. Planning and ownership use
// production code, and each retry takes a fresh snapshot of the simulated kernel.
type isolationFailureKernel struct {
	rules           []isolationPolicyRule
	source          net.IP
	remainingSource net.IP
	trace           []string
	failAt          int
	failure         error
	ignoreSourceSet bool
}

func isolationFailureState(t *testing.T, domains map[string]SRv6Isolation) []isolationPolicyRule {
	t.Helper()
	want := desiredIsolationRules(domains)
	current := make([]isolationPolicyRule, 0, len(want)+2)
	current = append(current,
		isolationPolicyRule{rule: netlink.Rule{Priority: 1000}, l3mdev: true},
		isolationPolicyRule{rule: netlink.Rule{Priority: 900, Protocol: 99, Table: 800, Type: unix.FR_ACT_TO_TBL}},
	)
	desired, err := planIsolationPriorities(want, current)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range desired {
		current = append(current, isolationPolicyRule{rule: rule})
	}
	return current
}

func (k *isolationFailureKernel) apply(domains map[string]SRv6Isolation) error {
	current := slices.Clone(k.rules)
	desired, err := planIsolationPriorities(desiredIsolationRules(domains), current)
	if err != nil {
		return err
	}
	var source net.IP
	for _, config := range domains {
		source = net.ParseIP(config.Source)
	}
	return applyIsolationRules(current, desired, source, isolationRuleOperations{
		add: k.add, remove: k.remove, getSource: k.getSource, setSource: k.setSource,
		remainingSource: k.remainingSource,
	})
}

func (k *isolationFailureKernel) stage(domains map[string]SRv6Isolation) error {
	current := slices.Clone(k.rules)
	desired, err := planIsolationPriorities(desiredIsolationRules(domains), current)
	if err != nil {
		return err
	}
	return stageIsolationRules(current, desired, k.add)
}

func (k *isolationFailureKernel) reconcile(t *testing.T, domains map[string]SRv6Isolation) {
	t.Helper()
	if err := k.apply(domains); err != nil {
		t.Fatal(err)
	}
	want := desiredIsolationRules(domains)
	var actual []string
	for _, entry := range k.rules {
		if ownedIsolationPolicyRule(entry) {
			actual = append(actual, isolationRuleKey(entry.rule))
		}
	}
	keys := make([]string, 0, len(want))
	for _, rule := range want {
		keys = append(keys, isolationRuleKey(rule))
	}
	slices.Sort(actual)
	slices.Sort(keys)
	if !slices.Equal(actual, keys) {
		t.Fatalf("retry left duplicate, missing or obsolete owned rules:\ngot %v\nwant %v", actual, keys)
	}
	if len(domains) != 0 {
		for _, config := range domains {
			if !k.source.Equal(net.ParseIP(config.Source)) {
				t.Fatalf("source got %s, want %s", k.source, config.Source)
			}
		}
	}
}

func (k *isolationFailureKernel) assertPreserved(t *testing.T, initial []isolationPolicyRule,
	domains map[string]SRv6Isolation,
) {
	t.Helper()
	desired := desiredIsolationRules(domains)
	for _, entry := range initial {
		if !stillRequiredIsolationRule(entry, initial, desired) {
			continue
		}
		if !slices.ContainsFunc(k.rules, func(actual isolationPolicyRule) bool {
			return reflect.DeepEqual(actual, entry)
		}) {
			t.Fatalf("foreign or still-required shared rule changed: %+v", entry)
		}
		for _, operation := range []string{"add", "delete"} {
			if slices.Contains(k.trace, fmt.Sprintf("%s %d %s", operation, entry.rule.Type, isolationRuleKey(entry.rule))) {
				t.Fatalf("foreign or still-required shared rule touched: %+v", entry)
			}
		}
	}
}

func stillRequiredIsolationRule(entry isolationPolicyRule, initial []isolationPolicyRule, desired []netlink.Rule) bool {
	if !ownedIsolationPolicyRule(entry) {
		return true
	}
	if entry.rule.Type == unix.FR_ACT_TO_TBL {
		return slices.ContainsFunc(desired, func(rule netlink.Rule) bool {
			return isolationRuleKey(entry.rule) == isolationRuleKey(rule)
		})
	}
	for _, prior := range initial {
		if prior.rule.Type != unix.FR_ACT_TO_TBL || prior.rule.Priority+1 != entry.rule.Priority {
			continue
		}
		if slices.ContainsFunc(desired, func(rule netlink.Rule) bool {
			return rule.Type == unix.FR_ACT_TO_TBL && isolationRuleKey(prior.rule) == isolationRuleKey(rule)
		}) {
			return true
		}
	}
	return false
}

func (k *isolationFailureKernel) step(operation string) error {
	k.trace = append(k.trace, operation)
	if k.failAt == len(k.trace) {
		return k.failure
	}
	return nil
}

func (k *isolationFailureKernel) add(rule *netlink.Rule) error {
	if err := k.step(fmt.Sprintf("add %d %s", rule.Type, isolationRuleKey(*rule))); err != nil {
		return err
	}
	if rule.Type == unix.FR_ACT_TO_TBL {
		terminal := *rule
		terminal.Type, terminal.Table, terminal.Priority = unix.FR_ACT_UNREACHABLE, 0, rule.Priority+1
		if !slices.ContainsFunc(k.rules, func(entry isolationPolicyRule) bool {
			return containsIsolationRule([]netlink.Rule{terminal}, entry.rule)
		}) {
			return errors.New("lookup added without corresponding terminal")
		}
	}
	k.rules = append(k.rules, isolationPolicyRule{rule: *rule})
	return nil
}

func (k *isolationFailureKernel) remove(rule *netlink.Rule) error {
	if err := k.step(fmt.Sprintf("delete %d %s", rule.Type, isolationRuleKey(*rule))); err != nil {
		return err
	}
	if rule.Type == unix.FR_ACT_UNREACHABLE {
		for _, entry := range k.rules {
			lookup := entry.rule
			lookup.Type, lookup.Table, lookup.Priority = unix.FR_ACT_UNREACHABLE, 0, lookup.Priority+1
			if entry.rule.Type == unix.FR_ACT_TO_TBL && containsIsolationRule([]netlink.Rule{*rule}, lookup) {
				return errors.New("terminal removed before its lookup")
			}
		}
	}
	for i, entry := range k.rules {
		if containsIsolationRule([]netlink.Rule{*rule}, entry.rule) {
			k.rules = slices.Delete(k.rules, i, i+1)
			return nil
		}
	}
	return unix.ENOENT
}

func (k *isolationFailureKernel) getSource() (net.IP, error) {
	if err := k.step("get source"); err != nil {
		return nil, err
	}
	return k.source, nil
}

func (k *isolationFailureKernel) setSource(source net.IP) error {
	if err := k.step("set source " + source.String()); err != nil {
		return err
	}
	if !k.ignoreSourceSet {
		k.source = source
	}
	return nil
}
