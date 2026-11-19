// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"strings"
	"testing"
)

func TestSRv6IsolationValidation(t *testing.T) {
	tests := []struct {
		name, source string
		table        uint32
		prefixes     []string
		want         string
	}{
		{name: "valid main", source: "2001:db8::1", table: 254, prefixes: []string{"fc00:20::/48"}},
		{name: "explicit custom table", source: "2001:db8::1", table: 701, prefixes: []string{"fc00:20::/48"}},
		{name: "missing source", table: 254, prefixes: []string{"fc00:20::/48"}, want: "encapsulationSource"},
		{name: "IPv4 source", source: "192.0.2.1", table: 254, prefixes: []string{"fc00:20::/48"}, want: "encapsulationSource"},
		{name: "link-local source", source: "fe80::1", table: 254, prefixes: []string{"fc00:20::/48"}, want: "encapsulationSource"},
		{name: "reserved table", source: "2001:db8::1", table: 255, prefixes: []string{"fc00:20::/48"}, want: "underlayTable"},
		{name: "missing pools", source: "2001:db8::1", table: 254, want: "remoteLocatorPrefixes"},
		{name: "broad default", source: "2001:db8::1", table: 254, prefixes: []string{"::/0"}, want: "remote locator"},
		{name: "IPv4 pool", source: "2001:db8::1", table: 254, prefixes: []string{"192.0.2.0/24"}, want: "remote locator"},
		{name: "host bits", source: "2001:db8::1", table: 254, prefixes: []string{"fc00:20::1/48"}, want: "canonical"},
		{name: "source in remote pool", source: "2001:db8::1", table: 254, prefixes: []string{"2001:db8::/64"}},
		{name: "overlapping link-local", source: "2001:db8::1", table: 254, prefixes: []string{"fe00::/8"}, want: "remote locator"},
		{name: "overlapping multicast", source: "2001:db8::1", table: 254, prefixes: []string{"fc00::/6"}, want: "remote locator"},
		{name: "malformed locator", source: "2001:db8::1", table: 254, prefixes: []string{"invalid"}, want: "remote locator"},
		{name: "duplicate pool", source: "2001:db8::1", table: 254, prefixes: []string{"fc00:20::/48", "fc00:20::/48"}, want: "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (SRv6Isolation{Source: tt.source, UnderlayTable: tt.table, RemoteLocatorPrefixes: tt.prefixes}).Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestL3VPNRejectsInvalidIsolationBeforeNamespaceAccess(t *testing.T) {
	configurator := &VNIConfigurator{}
	err := configurator.SetupL3VPN(context.Background(), L3VPNParams{VRF: "red", RoutingIsolation: &SRv6Isolation{}})
	if err == nil || err.Error() != "encapsulationSource must be a unicast IPv6 address" {
		t.Fatalf("invalid isolation reached kernel verification: %v", err)
	}
}
