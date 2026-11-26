// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestIsolationReadbackRejectsForeignRetainedPriority(t *testing.T) {
	config := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	current := isolationFailureState(t, map[string]SRv6Isolation{"red": config})
	var desired []netlink.Rule
	for _, entry := range current {
		if ownedIsolationPolicyRule(entry) {
			desired = append(desired, entry.rule)
		}
	}
	foreign := *netlink.NewRule()
	foreign.Family, foreign.Priority, foreign.Table = netlink.FAMILY_V6, desired[0].Priority, 702
	current = append(current, isolationPolicyRule{rule: foreign})
	if isolationPoliciesEqual(desired, current) {
		t.Fatal("accepted foreign lookup at retained priority")
	}
}

func TestIsolationReadbackL3MDevBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		boundary int
		empty    bool
		want     bool
	}{
		{name: "before l3mdev", boundary: 1000, want: true},
		{name: "moved l3mdev", boundary: 5},
		{name: "missing l3mdev"},
		{name: "empty without l3mdev", empty: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var desired []netlink.Rule
			var current []isolationPolicyRule
			if !tc.empty {
				desired = []netlink.Rule{
					expectedIsolationPriorityRule(21, "red", 701, unix.FR_ACT_TO_TBL),
					expectedIsolationPriorityRule(22, "red", 0, unix.FR_ACT_UNREACHABLE),
				}
				for _, rule := range desired {
					current = append(current, isolationPolicyRule{rule: rule})
				}
			}
			if tc.boundary != 0 {
				current = append(current, isolationPolicyRule{rule: netlink.Rule{Priority: tc.boundary}, l3mdev: true})
			}
			if got := isolationPoliciesEqual(desired, current); got != tc.want {
				t.Fatalf("readback = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsolationPriorities(t *testing.T) {
	config := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	desired := desiredIsolationRules(map[string]SRv6Isolation{"red": config})
	shared := desiredIsolationRules(map[string]SRv6Isolation{"red": config, "blue": config})
	kernel := isolationPolicyRule{rule: netlink.Rule{Priority: 1000}, l3mdev: true}
	for _, tc := range []struct {
		name      string
		desired   []netlink.Rule
		current   []isolationPolicyRule
		wantRules []netlink.Rule
		wantErr   string
	}{
		{name: "empty namespace", wantRules: nil},
		{
			name: "cleanup without l3mdev",
			current: []isolationPolicyRule{
				{rule: expectedIsolationPriorityRule(1, "lo", 701, unix.FR_ACT_TO_TBL)},
			},
			wantRules: nil,
		},
		{name: "missing l3mdev", desired: desired, wantErr: "l3mdev"},
		{
			name: "normal", desired: desired, current: []isolationPolicyRule{kernel},
			wantRules: []netlink.Rule{
				expectedIsolationPriorityRule(1, "lo", 701, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(2, "lo", 0, unix.FR_ACT_UNREACHABLE),
				expectedIsolationPriorityRule(3, "red", 701, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(4, "red", 0, unix.FR_ACT_UNREACHABLE),
			},
		},
		{
			name: "shared loopback pair", desired: shared, current: []isolationPolicyRule{kernel},
			wantRules: []netlink.Rule{
				expectedIsolationPriorityRule(1, "blue", 701, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(2, "blue", 0, unix.FR_ACT_UNREACHABLE),
				expectedIsolationPriorityRule(3, "lo", 701, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(4, "lo", 0, unix.FR_ACT_UNREACHABLE),
				expectedIsolationPriorityRule(5, "red", 701, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(6, "red", 0, unix.FR_ACT_UNREACHABLE),
			},
		},
		{
			name: "retain existing priorities", desired: desired,
			current: []isolationPolicyRule{
				kernel,
				{rule: expectedIsolationPriorityRule(21, "lo", 701, unix.FR_ACT_TO_TBL)},
				{rule: expectedIsolationPriorityRule(22, "lo", 0, unix.FR_ACT_UNREACHABLE)},
				{rule: expectedIsolationPriorityRule(23, "red", 701, unix.FR_ACT_TO_TBL)},
				{rule: expectedIsolationPriorityRule(24, "red", 0, unix.FR_ACT_UNREACHABLE)},
			},
			wantRules: []netlink.Rule{
				expectedIsolationPriorityRule(21, "lo", 701, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(22, "lo", 0, unix.FR_ACT_UNREACHABLE),
				expectedIsolationPriorityRule(23, "red", 701, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(24, "red", 0, unix.FR_ACT_UNREACHABLE),
			},
		},
		{
			name: "stage table replacement before the existing terminal",
			desired: desiredIsolationRules(map[string]SRv6Isolation{"red": {
				Source: config.Source, UnderlayTable: 702, RemoteLocatorPrefixes: config.RemoteLocatorPrefixes,
			}}),
			current: []isolationPolicyRule{
				kernel,
				{rule: expectedIsolationPriorityRule(1, "lo", 701, unix.FR_ACT_TO_TBL)},
				{rule: expectedIsolationPriorityRule(2, "lo", 0, unix.FR_ACT_UNREACHABLE)},
				{rule: expectedIsolationPriorityRule(3, "red", 701, unix.FR_ACT_TO_TBL)},
				{rule: expectedIsolationPriorityRule(4, "red", 0, unix.FR_ACT_UNREACHABLE)},
			},
			wantRules: []netlink.Rule{
				expectedIsolationPriorityRule(1, "lo", 702, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(2, "lo", 0, unix.FR_ACT_UNREACHABLE),
				expectedIsolationPriorityRule(3, "red", 702, unix.FR_ACT_TO_TBL),
				expectedIsolationPriorityRule(4, "red", 0, unix.FR_ACT_UNREACHABLE),
			},
		},
		{
			name: "priority conflict", desired: desired,
			current: []isolationPolicyRule{kernel, {rule: netlink.Rule{Priority: 1}}},
			wantErr: "conflicts",
		},
		{
			name: "priority space", desired: desired,
			current: []isolationPolicyRule{{rule: netlink.Rule{Priority: 4}, l3mdev: true}},
			wantErr: "not enough",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := planIsolationPriorities(tc.desired, tc.current)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got %v, want %s", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, tc.wantRules) {
				t.Fatalf("rules = %#v; want %#v", actual, tc.wantRules)
			}
		})
	}
}

func TestSharedIsolationPairSurvivesDomainRemoval(t *testing.T) {
	config := SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	kernel := isolationPolicyRule{rule: netlink.Rule{Priority: 1000}, l3mdev: true}
	initial, err := planIsolationPriorities(
		desiredIsolationRules(map[string]SRv6Isolation{"red": config, "blue": config}),
		[]isolationPolicyRule{kernel})
	if err != nil {
		t.Fatal(err)
	}
	current := make([]isolationPolicyRule, 0, len(initial)+1)
	current = append(current, kernel)
	for _, rule := range initial {
		current = append(current, isolationPolicyRule{rule: rule})
	}
	remaining, err := planIsolationPriorities(desiredIsolationRules(map[string]SRv6Isolation{"red": config}), current)
	if err != nil {
		t.Fatal(err)
	}
	want := []netlink.Rule{
		expectedIsolationPriorityRule(3, "lo", 701, unix.FR_ACT_TO_TBL),
		expectedIsolationPriorityRule(4, "lo", 0, unix.FR_ACT_UNREACHABLE),
		expectedIsolationPriorityRule(5, "red", 701, unix.FR_ACT_TO_TBL),
		expectedIsolationPriorityRule(6, "red", 0, unix.FR_ACT_UNREACHABLE),
	}
	if !reflect.DeepEqual(remaining, want) {
		t.Fatalf("remaining rules = %#v; want %#v", remaining, want)
	}
}

func expectedIsolationPriorityRule(priority int, iface string, table int, action uint8) netlink.Rule {
	return netlink.Rule{
		Priority: priority, Family: netlink.FAMILY_V6, Table: table,
		Src:     &net.IPNet{IP: net.ParseIP("2001:db8::1").To16(), Mask: net.CIDRMask(128, 128)},
		Dst:     &net.IPNet{IP: net.ParseIP("fc00:20::").To16(), Mask: net.CIDRMask(48, 128)},
		IifName: iface, Type: action, Protocol: 242,
		SuppressIfgroup: -1, SuppressPrefixlen: -1, Goto: -1, Flow: -1,
	}
}
