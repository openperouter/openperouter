// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestIsolationRotationCapacity(t *testing.T) {
	for _, tc := range []struct {
		name         string
		blueLocators []string
		wantError    bool
	}{
		{name: "shared locators", blueLocators: rotationLocators(32)},
		{name: "distinct locators", blueLocators: rotationLocators(256), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			red := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: rotationLocators(32)}
			blue := red
			blue.RemoteLocatorPrefixes = tc.blueLocators
			current := []isolationPolicyRule{{rule: netlink.Rule{Priority: 1000}, l3mdev: true}}
			desired := desiredIsolationRules(map[string]SRv6Isolation{"red": red, "blue": blue})
			err := validateIsolationRotationCapacity(desired, current)
			if tc.wantError {
				want := "SRv6 isolation needs 512 priority pairs for source rotation; only 499 fit before IPv6 l3mdev rule 1000"
				if err == nil || err.Error() != want {
					t.Fatalf("got %v, want %s", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLegacyMainLookupConflict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		locator string
		table   uint32
		want    bool
	}{
		{name: "same selector different table", locator: "fc00:20::/48", table: 701, want: true},
		{name: "already main", locator: "fc00:20::/48", table: 254},
		{name: "different selector", locator: "fc00:30::/48", table: 701},
		{name: "more specific selector", locator: "fc00:20:0:1::/64", table: 701},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
			desired := desiredIsolationRules(map[string]SRv6Isolation{"blue": config})
			config.UnderlayTable, config.RemoteLocatorPrefixes = tc.table, []string{tc.locator}
			legacy := desiredIsolationRules(map[string]SRv6Isolation{"red": config})
			if got := conflictingLegacyIsolationLookup(desired, legacy); got != tc.want {
				t.Fatalf("conflict=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestRetireSupersededIsolationRulesRetries(t *testing.T) {
	config := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	initial := isolationFailureState(t, map[string]SRv6Isolation{"red": config})
	for _, address := range []string{"2001:db8::2", "2001:db8::3"} {
		config.Source = address
		planned, err := planIsolationPriorities(desiredIsolationRules(map[string]SRv6Isolation{"red": config}), initial)
		if err != nil {
			t.Fatal(err)
		}
		for _, rule := range planned {
			initial = append(initial, isolationPolicyRule{rule: rule})
		}
	}
	for _, active := range []string{"2001:db8::1", "2001:db8::2"} {
		for _, next := range []string{"2001:db8::3", active, "2001:db8::4"} {
			for failAt := 1; failAt <= 4; failAt++ {
				t.Run(fmt.Sprintf("active=%s/next=%s/failure=%d", active, next, failAt), func(t *testing.T) {
					checkSupersededRuleRetry(t, initial, active, next, failAt)
				})
			}
		}
	}
}

func checkSupersededRuleRetry(t *testing.T, initial []isolationPolicyRule, active, next string, failAt int) {
	t.Helper()
	actual := slices.Clone(initial)
	keep := currentIsolationSources(actual, net.ParseIP(active), net.ParseIP("2001:db8::3"))
	calls := 0
	failure := errors.New("rule delete failed")
	remove := func(rule *netlink.Rule) error {
		calls++
		if calls == failAt {
			return failure
		}
		if !ownedIsolationPolicyRule(isolationPolicyRule{rule: *rule}) || rule.Src.IP.Equal(net.ParseIP(active)) {
			t.Fatalf("removing active or foreign rule: %v", rule)
		}
		if rule.Type == unix.FR_ACT_UNREACHABLE {
			for _, entry := range actual {
				lookup := entry.rule
				if lookup.Type == unix.FR_ACT_TO_TBL && lookup.IifName == rule.IifName && lookup.Src.String() == rule.Src.String() {
					t.Fatalf("terminal removed before lookup: %v", rule)
				}
			}
		}
		actual = slices.DeleteFunc(actual, func(entry isolationPolicyRule) bool { return reflect.DeepEqual(entry.rule, *rule) })
		return nil
	}
	err := retireSupersededIsolationRules(actual, keep, remove)
	if !errors.Is(err, failure) || calls != failAt {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	// Retry can use the same desired source, roll back, or supersede it again.
	keep = currentIsolationSources(actual, net.ParseIP(active), net.ParseIP(next))
	if err := retireSupersededIsolationRules(actual, keep, remove); err != nil {
		t.Fatal(err)
	}
	want := currentIsolationSources(initial, net.ParseIP(active), net.ParseIP(next))
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("retry rules = %v, want %v", actual, want)
	}
}

func rotationLocators(base int) []string {
	locators := make([]string, 0, 64)
	for i := range 64 {
		locators = append(locators, fmt.Sprintf("fc00:%x::/48", base+i))
	}
	return locators
}
