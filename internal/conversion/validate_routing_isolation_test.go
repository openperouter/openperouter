// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"reflect"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

func TestValidateRoutingIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, source     string
		pool             []string
		table            int64
		tenantCIDR, want string
		features         []v1alpha1.L3VPNFeature
	}{
		{name: "explicit isolation", source: "2001:db8::1", pool: []string{"fc00:20::/48"}, table: 701},
		{name: "no source fallback", pool: []string{"fc00:20::/48"}, table: 254, want: "encapsulationSource"},
		{name: "infrastructure overlaps tenant", source: "2001:db8::1", pool: []string{"fc00:20::/48"}, table: 254, tenantCIDR: "2001:db8::/64", want: ""},
		{name: "negative table", source: "2001:db8::1", table: -1, pool: []string{"fc00:20::/48"}, want: "underlayTable"},
		{name: "table overflow", source: "2001:db8::1", table: 4294967296, pool: []string{"fc00:20::/48"}, want: "underlayTable"},
		{name: "per-family main", source: "2001:db8::1", table: 254, pool: []string{"fc00:20::/48"}, features: []v1alpha1.L3VPNFeature{v1alpha1.UDT4UDT6}},
		{name: "per-family dedicated table", source: "2001:db8::1", table: 701, pool: []string{"fc00:20::/48"}, features: []v1alpha1.L3VPNFeature{v1alpha1.UDT4UDT6}, want: "uDT4uDT6"},
		{name: "no pools", source: "2001:db8::1", table: 254, want: "remoteLocatorPrefixes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := APIConfigData{
				Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{SRV6: &v1alpha1.SRV6Config{EncapsulationSource: new(tc.source)}}}},
				L3VPNs:    []v1alpha1.L3VPN{{Spec: v1alpha1.L3VPNSpec{VRF: "red", Features: tc.features, RoutingIsolation: &v1alpha1.SRV6RoutingIsolation{UnderlayTable: tc.table, RemoteLocatorPrefixes: tc.pool}}}},
			}
			if tc.tenantCIDR != "" {
				config.L3VPNs[0].Spec.HostSession = &v1alpha1.HostSession{LocalCIDRs: []string{tc.tenantCIDR}}
			}
			err := ValidateRoutingIsolation(config)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}

func TestRoutingIsolationNamespaceModes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		table     int64
		legacy    bool
		wantError bool
	}{
		{name: "legacy only", legacy: true},
		{name: "main and legacy", table: 254, legacy: true},
		{name: "dedicated and legacy", table: 701, legacy: true, wantError: true},
		{name: "fully isolated", table: 701},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := APIConfigData{Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{
				SRV6: &v1alpha1.SRV6Config{EncapsulationSource: new("2001:db8::1")},
			}}}}
			if tc.table != 0 {
				config.L3VPNs = append(config.L3VPNs, v1alpha1.L3VPN{Spec: v1alpha1.L3VPNSpec{
					VRF: "red", RoutingIsolation: &v1alpha1.SRV6RoutingIsolation{
						UnderlayTable: tc.table, RemoteLocatorPrefixes: []string{"fc00:20::/48"}},
				}})
			}
			if tc.legacy {
				config.L3VPNs = append(config.L3VPNs, v1alpha1.L3VPN{Spec: v1alpha1.L3VPNSpec{VRF: "blue"}})
			}
			err := ValidateRoutingIsolation(config)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "cannot share a namespace") {
					t.Fatalf("expected namespace-mode error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			usage := ValidateEncapsulationSourceUsage(config)
			if tc.table == 0 && usage == nil {
				t.Fatal("unused source was accepted")
			}
			if tc.table != 0 && usage != nil {
				t.Fatal(usage)
			}
		})
	}
}

func TestAPItoHostConfigWithoutUnderlayIgnoresStaleIsolation(t *testing.T) {
	config := APIConfigData{L3VPNs: []v1alpha1.L3VPN{{Spec: v1alpha1.L3VPNSpec{
		VRF: "red", RoutingIsolation: &v1alpha1.SRV6RoutingIsolation{UnderlayTable: -1},
	}}}}
	actual, err := APItoHostConfig(0, "/unused", config)
	if err != nil {
		t.Fatal(err)
	}
	want := HostConfigData{L3VNIs: []hostnetwork.L3VNIParams{}, L2VNIs: []hostnetwork.L2VNIParams{}}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("got %#v; want %#v", actual, want)
	}
}
