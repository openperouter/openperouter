// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestValidateL3VPNsForNodes tests the per-node filtering logic only.
// The underlying validation rules are already covered by TestValidateL3VPNs.
func TestValidateL3VPNsForNodes(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []corev1.Node
		l3vpns    []v1alpha1.L3VPN
		l2vnis    []v1alpha1.L2VNI
		underlays []v1alpha1.Underlay
		errorStr  string
	}{
		{
			name:      "no nodes",
			nodes:     nil,
			l3vpns:    nil,
			underlays: nil,
			errorStr:  "",
		},
		{
			name: "L3VPN and underlay with nil nodeSelectors match all nodes",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "pe"}}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec:       v1alpha1.L3VPNSpec{VRF: "vrf1"},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec:       v1alpha1.UnderlaySpec{SRV6: &v1alpha1.SRV6Config{}},
				},
			},
			errorStr: "",
		},
		{
			name: "L3VPN nodeSelector matches node",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "pe"}}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:          "vrf1",
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec:       v1alpha1.UnderlaySpec{SRV6: &v1alpha1.SRV6Config{}},
				},
			},
			errorStr: "",
		},
		{
			name: "L3VPN nodeSelector does not match node, skips validation",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "compute"}}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:          "vrf1",
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			underlays: nil,
			errorStr:  "",
		},
		{
			name: "underlay nodeSelector does not match node, no underlay for node",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "pe"}}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec:       v1alpha1.L3VPNSpec{VRF: "vrf1"},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "other"}},
						SRV6:         &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: "failed to validate underlays for node \"node1\": cannot create L3VPNs without a valid SRV6 configuration (have no underlays)",
		},
		{
			name: "multiple nodes, all valid",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "pe"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"role": "pe"}}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:          "vrf1",
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
						SRV6:         &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: "",
		},
		{
			name: "multiple L3VPNs with SRV6 underlay and L2VNI",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "pe"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"role": "pe"}}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf1",
						RDAssignedNumber: 100,
						NodeSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf2",
						RDAssignedNumber: 101,
						NodeSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6:         &v1alpha1.SRV6Config{},
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("vrf1"),
						VNI:          100,
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "other"}},
					},
				},
			},
			errorStr: "",
		},
		{
			name: "multiple L3VPNs with SRV6 underlay and L2VNI with overlapping VNI",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "pe"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"role": "pe"}}},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf1",
						RDAssignedNumber: 100,
						NodeSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf2",
						RDAssignedNumber: 101,
						NodeSelector:     &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6:         &v1alpha1.SRV6Config{},
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("vrf1"),
						VNI:          100,
						NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "pe"}},
					},
				},
			},
			errorStr: "failed to validate underlays for node \"node1\": duplicate vni 100:l3vpn1 - l2vni",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateL3VPNsForNodes(tt.nodes, tt.l3vpns, tt.underlays, tt.l2vnis)
			if tt.errorStr == "" {
				if err != nil {
					t.Fatalf("ValidateL3VPNsForNodes() unexpected error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateL3VPNsForNodes() expected error %q, got nil", tt.errorStr)
			}
			if err.Error() != tt.errorStr {
				t.Fatalf("ValidateL3VPNsForNodes() error = %q, want %q", err.Error(), tt.errorStr)
			}
		})
	}
}

func TestValidateL3VPNs(t *testing.T) {
	tests := []struct {
		name      string
		l3vpns    []v1alpha1.L3VPN
		underlays []v1alpha1.Underlay
		l2vnis    []v1alpha1.L2VNI
		errorStr  string
	}{
		{
			name:      "no L3VPNs, no underlays",
			l3vpns:    nil,
			underlays: nil,
			errorStr:  "",
		},
		{
			name: "valid L3VPN with SRV6 underlay",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "vrf1",
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: "",
		},
		{
			name: "multiple L3VPNs with SRV6 underlay",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf1",
						RDAssignedNumber: 100,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf2",
						RDAssignedNumber: 101,
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: "",
		},
		{
			name: "multiple L3VPNs with SRV6 underlay and L2VNIs",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf1",
						RDAssignedNumber: 100,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf2",
						RDAssignedNumber: 101,
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF: new("vrf1"),
						VNI: 102,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF: new("vrf3"),
						VNI: 103,
					},
				},
			},
			errorStr: "",
		},
		{
			name: "multiple L3VPNs with SRV6 underlay and L2VNIs with overlapping VNI",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf1",
						RDAssignedNumber: 100,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf2",
						RDAssignedNumber: 101,
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF: new("vrf1"),
						VNI: 101,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF: new("vrf3"),
						VNI: 102,
					},
				},
			},
			errorStr: "duplicate vni 101:l3vpn2 - l2vni1",
		},
		{
			name: "multiple L3VPNs with SRV6 underlay with same RDAssignedNumber should fail",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf1",
						RDAssignedNumber: 100,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn2"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf2",
						RDAssignedNumber: 100,
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: "duplicate vni 100:l3vpn1 - l3vpn2",
		},
		{
			name: "invalid VRF name should fail",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf123456789abcdefg",
						RDAssignedNumber: 100,
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: "invalid vrf name for vni l3vpn1: vrf123456789abcdefg - interface name vrf123456789abcdefg " +
				"can't be longer than 15 characters",
		},
		{
			name: "invalid route target should fail",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "vrf1",
						RDAssignedNumber: 100,
						ImportRTs:        []v1alpha1.RouteTarget{"abc"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: `invalid route targets for vni l3vpn1: RT "abc" must have one of the following formats: ` +
				`'ASN:MN' or 'IPv4Address:MN'`,
		},
		{
			name: "L3VPN without any underlays",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "vrf1",
					},
				},
			},
			underlays: nil,
			errorStr:  "cannot create L3VPNs without a valid SRV6 configuration (have no underlays)",
		},
		{
			name: "L3VPN with empty underlays slice",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "vrf1",
					},
				},
			},
			underlays: []v1alpha1.Underlay{},
			errorStr:  "cannot create L3VPNs without a valid SRV6 configuration (have no underlays)",
		},
		{
			name: "L3VPN with more than one underlay",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "vrf1",
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay2"},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
			errorStr: "cannot have more than one underlay per node",
		},
		{
			name: "L3VPN with underlay missing SRV6 config",
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l3vpn1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "vrf1",
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
					Spec:       v1alpha1.UnderlaySpec{},
				},
			},
			errorStr: "cannot create L3VPNs without a valid underlay.Spec.SRV6 definition",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateL3VPNs(tt.l3vpns, tt.underlays, tt.l2vnis)
			if tt.errorStr == "" {
				if err != nil {
					t.Fatalf("ValidateL3VPNs() unexpected error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateL3VPNs() expected error %q, got nil", tt.errorStr)
			}
			if err.Error() != tt.errorStr {
				t.Fatalf("ValidateL3VPNs() error = %q, want %q", err.Error(), tt.errorStr)
			}
		})
	}
}
