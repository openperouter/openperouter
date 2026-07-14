// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openperouter/openperouter/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterValidL3VPNs(t *testing.T) {
	tcs := []struct {
		name      string
		l3vpns    []v1alpha1.L3VPN
		wantValid []v1alpha1.L3VPN
		wantErr   string
	}{
		{
			name: "all valid",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
						ExportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
						ExportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
		},
		{
			name: "invalid VRF name",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantErr: "L3VPN/vpn2: invalid vrf name for vpn \"vpn2\", vrf \"\": interface name cannot be empty",
		},
		{
			name: "invalid route target",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002, VRF: "vrf2",
						ImportRTs: []v1alpha1.RouteTarget{"65000:100"},
						ExportRTs: []v1alpha1.RouteTarget{"invalid"},
					},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantErr: "3VPN/vpn2: invalid route targets for vpn \"vpn2\": RT \"invalid\" must have one of the " +
				"following formats: 'ASN:MN' or 'IPv4Address:MN'",
		},
		{
			name: "empty importRTs are invalid but empty exportRTs are valid",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
					},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantErr: "L3VPN/vpn2: invalid import route targets for vpn \"vpn2\": import route targets cannot be empty",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			valid, err := FilterValidL3VPNs(tc.l3vpns)
			if diff := cmp.Diff(tc.wantValid, valid); diff != "" {
				t.Fatalf("valid items mismatch (-want +got):\n%s", diff)
			}

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q but got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("got error = %q, but want error %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error but got %q", err)
			}
		})
	}
}

func TestFilterUniqueL3VPNs(t *testing.T) {
	tcs := []struct {
		name      string
		l3vpns    []v1alpha1.L3VPN
		wantValid []v1alpha1.L3VPN
		wantMap   map[int32]string
		wantErr   string
	}{
		{
			name: "all unique RD assigned numbers",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1001, VRF: "vrf1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1002, VRF: "vrf2"},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1001, VRF: "vrf1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1002, VRF: "vrf2"},
				},
			},
			wantMap: map[int32]string{1001: "L3VPN/vpn1", 1002: "L3VPN/vpn2"},
		},
		{
			name: "duplicate RD assigned number",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1001, VRF: "vrf1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1002, VRF: "vrf2"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn3"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1001, VRF: "vrf3"},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1001, VRF: "vrf1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn2"},
					Spec:       v1alpha1.L3VPNSpec{RDAssignedNumber: 1002, VRF: "vrf2"},
				},
			},
			wantMap: map[int32]string{1001: "L3VPN/vpn1", 1002: "L3VPN/vpn2"},
			wantErr: "L3VPN/vpn3: duplicate rdAssignedNumber 1001:L3VPN/vpn1",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			valid, gotMap, err := FilterUniqueL3VPNs(tc.l3vpns)
			if diff := cmp.Diff(tc.wantValid, valid); diff != "" {
				t.Fatalf("valid items mismatch (-want +got):\n%s", diff)
			}
			if !cmp.Equal(gotMap, tc.wantMap) {
				t.Fatalf("expected map %v; but got %v", tc.wantMap, gotMap)
			}

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q but got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("got error = %q, but want error %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error but got %q", err)
			}
		})
	}
}

func TestFilterUniqueVRFsForL3VPNs(t *testing.T) {
	tcs := []struct {
		name      string
		l3vpns    []v1alpha1.L3VPN
		wantValid []v1alpha1.L3VPN

		wantErr string
	}{
		{
			name: "all resources in different VRFs",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},
		},
		{
			name: "filters duplicate VRFs",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni3", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1003,
						HostSession:      &v1alpha1.HostSession{ASN: 65005, HostASN: new(int64(65006)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.3.0/24")}},
						VRF:              "vrf1",
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},
			wantValid: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1001,
						VRF:              "vrf1",
						HostSession:      &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 1002,
						VRF:              "vrf2",
						HostSession:      &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VPNStatus{},
				},
			},

			wantErr: "L3VPN/vni3: more than one L3VPN detected in VRF \"vrf1\": \"test/vni1\" already exists",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			valid, err := FilterUniqueVRFsForL3VPNs(tc.l3vpns)
			if diff := cmp.Diff(tc.wantValid, valid); diff != "" {
				t.Fatalf("valid items mismatch (-want +got):\n%s", diff)
			}

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q but got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("got error = %q, but want error %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error but got error %q", err)
			}
		})
	}
}

func TestDetectMutuallyExclusiveOverlays(t *testing.T) {
	tcs := []struct {
		name     string
		l3vnis   []v1alpha1.L3VNI
		l3vpns   []v1alpha1.L3VPN
		wantErrs []string
	}{
		{
			name: "both empty",
		},
		{
			name:   "only L3VNIs",
			l3vnis: []v1alpha1.L3VNI{{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}}},
		},
		{
			name:   "only L3VPNs",
			l3vpns: []v1alpha1.L3VPN{{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}}},
		},
		{
			name:   "both present",
			l3vnis: []v1alpha1.L3VNI{{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}}},
			l3vpns: []v1alpha1.L3VPN{{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}}},
			wantErrs: []string{
				"L3VNI/vni1: cannot specify L3VNI resources and L3VPN resources at the same time",
				"L3VPN/vpn1: cannot specify L3VPN resources and L3VNI resources at the same time",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := DetectMutuallyExclusiveOverlays(tc.l3vnis, tc.l3vpns)
			if len(tc.wantErrs) == 0 {
				if err != nil {
					t.Fatalf("expected no error but got %q", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error but got nil")
			}
			for _, wantErr := range tc.wantErrs {
				if !strings.Contains(err.Error(), wantErr) {
					t.Errorf("got error = %q, want it to contain %q", err, wantErr)
				}
			}
		})
	}
}

func TestValidateSRv6ForNodes(t *testing.T) {
	tests := []struct {
		name       string
		nodes      []corev1.Node
		underlays  []v1alpha1.Underlay
		l3vpns     []v1alpha1.L3VPN
		wantErrStr string
	}{
		{
			name:  "no L3VPNs and no underlays",
			nodes: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
		},
		{
			name:  "no nodes",
			nodes: []corev1.Node{},
		},
		{
			name:  "L3VPN with SRv6-enabled underlay",
			nodes: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fc00:0:1::/48",
								Format:     "usid-f3216",
							},
						},
					},
				},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 100,
						VRF:              "red",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
		},
		{
			name:  "L3VPN without any underlay",
			nodes: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 100,
						VRF:              "red",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantErrStr: "L3VPN/vpn1: cannot specify L3VPN configuration without an underlay with SRV6 " +
				"configuration on node \"node1\"",
		},
		{
			name:  "L3VPN with underlay missing SRv6 config",
			nodes: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec:       v1alpha1.UnderlaySpec{},
				},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 100,
						VRF:              "red",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantErrStr: "L3VPN/vpn1: cannot specify L3VPN configuration without an underlay with SRV6 " +
				"configuration on node \"node1\"",
		},
		{
			name:  "underlay with SRv6 but no L3VPNs",
			nodes: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fc00:0:1::/48",
								Format:     "usid-f3216",
							},
						},
					},
				},
			},
		},
		{
			name: "L3VPN on node without matching underlay due to node selector",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{
					Name:   "node1",
					Labels: map[string]string{"rack": "a"},
				}},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "b"},
						},
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fc00:0:1::/48",
								Format:     "usid-f3216",
							},
						},
					},
				},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 100,
						VRF:              "red",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantErrStr: "L3VPN/vpn1: cannot specify L3VPN configuration without an underlay with SRV6 " +
				"configuration on node \"node1\"",
		},
		{
			name: "three nodes - one valid two invalid",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{
					Name:   "node1",
					Labels: map[string]string{"rack": "a"},
				}},
				{ObjectMeta: metav1.ObjectMeta{
					Name:   "node2",
					Labels: map[string]string{"rack": "b"},
				}},
				{ObjectMeta: metav1.ObjectMeta{
					Name:   "node3",
					Labels: map[string]string{"rack": "c"},
				}},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "a"},
						},
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fc00:0:1::/48",
								Format:     "usid-f3216",
							},
						},
					},
				},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 100,
						VRF:              "red",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					},
				},
			},
			wantErrStr: "L3VPN/vpn1: cannot specify L3VPN configuration without an underlay with SRV6 configuration on node \"node2\"",
		},
		{
			name: "L3VPN not matching node is not validated",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{
					Name:   "node1",
					Labels: map[string]string{"rack": "a"},
				}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						RDAssignedNumber: 100,
						VRF:              "red",
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "b"},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSRv6ForNodes(tt.nodes, tt.underlays, tt.l3vpns)
			if tt.wantErrStr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrStr)
				}
				if !strings.Contains(err.Error(), tt.wantErrStr) {
					t.Errorf("error = %q, want substring %q", err, tt.wantErrStr)
				}
				return
			}
			if err != nil {
				t.Errorf("expected no error, but got: %v", err)
			}
		})
	}
}
