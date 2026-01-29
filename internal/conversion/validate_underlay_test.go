// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateUnderlay(t *testing.T) {
	tests := []struct {
		name     string
		underlay v1alpha1.Underlay
		wantErr  bool
	}{
		{
			name: "valid underlay",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{"eth0"},
					ASN:  65001,
				},
			},
			wantErr: false,
		},
		{
			name: "missing EVPN configuration",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					Nics: []string{"eth0"},
					ASN:  65001,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid VTEP CIDR",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "invalidCIDR",
					},
					Nics: []string{"eth0", "eth1"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "empty VTEP CIDR",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "",
					},
					Nics: []string{"eth0", "eth1"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid NIC name",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{"eth0", "1$^&invalid"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "zero nics",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{},
					ASN:  65001,
				},
			},
			wantErr: false,
		},
		{
			name: "valid underlay with no nics",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: nil,
					ASN:  65001,
				},
			},
			wantErr: false,
		},
		{
			name: "more than one nic",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{"eth0", "eth1"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "same local and remote ASN",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					ASN: 65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN: 65001,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "underlay NIC is a vlan sub-interface",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{"eno2.161"},
					ASN:  65001,
				},
			},
		},
		{
			name: "underlay NIC starts with dot",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{".eth0"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "a vlan sub interface whose name is too long",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{"verylongname.123"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "underlay NIC with invalid characters after dot",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{"eth0.100!"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "both vtepcidr and vtepInterface specified",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR:      "192.168.1.0/24",
						VTEPInterface: "eth0",
					},
					Nics: []string{"eth0"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "neither vtepcidr nor vtepInterface specified",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{},
					Nics: []string{"eth0"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
		{
			name: "only vtepInterface specified",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPInterface: "eth0",
					},
					Nics: []string{"eth1"},
					ASN:  65001,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid vtepInterface name",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPInterface: "1invalid",
					},
					Nics: []string{"eth0"},
					ASN:  65001,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUnderlays([]v1alpha1.Underlay{tt.underlay})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUnderlay() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	// Additional test: more than one underlay should error
	t.Run("multiple underlays", func(t *testing.T) {
		underlays := []v1alpha1.Underlay{
			{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Nics: []string{"eth0"},
					ASN:  65001,
				},
			},
			{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.2.0/24",
					},
					Nics: []string{"eth1"},
					ASN:  65002,
				},
			},
		}
		err := ValidateUnderlays(underlays)
		if err == nil {
			t.Errorf("expected error for multiple underlays, got nil")
		}
	})
}

func TestValidateUnderlaysForNodes(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []corev1.Node
		underlays []v1alpha1.Underlay
		wantErr   bool
		errMsg    string
	}{
		{
			name: "single node with matching underlay",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						ASN:  65001,
						Nics: []string{"eth0"},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "single node with non-matching underlay - no error because no underlay matches",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-2"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-2"},
						},
						ASN:  65001,
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "single node matching multiple underlays - should error",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1", "zone": "us-east-1a"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						ASN:  65001,
						Nics: []string{"eth0"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-zone"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"zone": "us-east-1a"},
						},
						ASN:  65002,
						Nics: []string{"eth1"},
					},
				},
			},
			wantErr: true,
			errMsg:  "can't have more than one underlay per node",
		},
		{
			name: "multiple nodes each with one matching underlay",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-2",
						Labels: map[string]string{"rack": "rack-2"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						ASN:  65001,
						Nics: []string{"eth0"},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-2"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-2"},
						},
						ASN:  65002,
						Nics: []string{"eth1"},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.2.0/24",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "node matching underlay with nil selector",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-all"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: nil,
						ASN:          65001,
						Nics:         []string{"eth0"},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "node matching underlay with empty selector",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-all"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{},
						ASN:          65001,
						Nics:         []string{"eth0"},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "node matching invalid underlay - should error on validation",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-invalid"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						ASN:  0, // Invalid ASN
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: true,
			errMsg:  "must have a valid ASN",
		},
		{
			name: "multiple nodes, one matching invalid underlay",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-2",
						Labels: map[string]string{"rack": "rack-2"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						ASN: 65001,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "invalid-cidr", // Invalid VTEP CIDR
						},
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: true,
			errMsg:  "failed to validate underlays for node",
		},
		{
			name: "node with both nil selector and specific selector underlays - should error",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-all"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: nil,
						ASN:          65001,
						Nics:         []string{"eth0"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						ASN:  65002,
						Nics: []string{"eth1"},
					},
				},
			},
			wantErr: true,
			errMsg:  "can't have more than one underlay per node",
		},
		{
			name:  "no nodes",
			nodes: []corev1.Node{},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-1"},
					Spec: v1alpha1.UnderlaySpec{
						ASN:  65001,
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "nodes with no matching underlays",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{},
			wantErr:   false,
		},
		{
			name: "complex multi-label selector matching",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1", "zone": "us-east-1a", "type": "compute"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-specific"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"rack": "rack-1",
								"zone": "us-east-1a",
							},
						},
						ASN:  65001,
						Nics: []string{"eth0"},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "node with underlay having same local and remote ASN - should error",
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node-1",
						Labels: map[string]string{"rack": "rack-1"},
					},
				},
			},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						ASN: 65001,
						Neighbors: []v1alpha1.Neighbor{
							{ASN: 65001},
						},
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: true,
			errMsg:  "local ASN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUnderlaysForNodes(tt.nodes, tt.underlays)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUnderlaysForNodes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			}
		})
	}
}
