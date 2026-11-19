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
		name    string
		initial map[string]SRv6Isolation
		source  net.IP
		desired map[string]SRv6Isolation
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source
			if source == nil {
				source = net.ParseIP(original.Source)
			}
			checkIsolationPartialFailures(t, tc.initial, tc.desired, source)
		})
	}
}

func TestApplyIsolationRulesSourceReadbackMismatch(t *testing.T) {
	config := SRv6Isolation{Source: "2001:db8::2", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	domains := map[string]SRv6Isolation{"red": config}
	kernel := &isolationFailureKernel{
		rules: isolationFailureState(t, nil), source: net.ParseIP("2001:db8::1"), ignoreSourceSet: true,
	}
	if err := kernel.apply(domains); err == nil || !strings.Contains(err.Error(), "readback mismatch") {
		t.Fatal("source readback mismatch must stop lookup installation")
	}
	for _, entry := range kernel.rules {
		if ownedIsolationPolicyRule(entry) && entry.rule.Type == unix.FR_ACT_TO_TBL {
			t.Fatal("lookup installed before successful source readback")
		}
	}
	kernel.ignoreSourceSet = false
	kernel.reconcile(t, domains)
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

func checkIsolationPartialFailures(t *testing.T, initialDomains, desiredDomains map[string]SRv6Isolation, source net.IP) {
	t.Helper()
	initial := isolationFailureState(t, initialDomains)
	baseline := &isolationFailureKernel{rules: slices.Clone(initial), source: source}
	baseline.reconcile(t, desiredDomains)
	if len(desiredDomains) == 0 && !baseline.source.Equal(net.IPv6zero) {
		t.Fatalf("last configuration removal did not reset source: %s", baseline.source)
	}
	wantTrace := slices.Clone(baseline.trace)
	for failAt, operation := range wantTrace {
		t.Run(fmt.Sprintf("%02d_%s", failAt+1, operation), func(t *testing.T) {
			injected := errors.New("injected netlink failure")
			kernel := &isolationFailureKernel{
				rules: slices.Clone(initial), source: source,
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
	})
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
		required := !ownedIsolationPolicyRule(entry)
		for _, rule := range desired {
			required = required || isolationRuleKey(entry.rule) == isolationRuleKey(rule)
		}
		if !required {
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
