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
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing tunnel endpoint configuration",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid VTEP CIDR",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"invalidCIDR"},
					},
					Nics: []string{"eth0", "eth1"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty VTEP CIDR",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{""},
					},
					Nics: []string{"eth0", "eth1"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid NIC name",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0", "1$^&invalid"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "more than one nic",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0", "eth1"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "same local and remote ASN (iBGP)",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					ASN: 65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN: new(int64(65001)),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "underlay NIC is a vlan sub-interface",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eno2.161"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "underlay NIC starts with dot",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{".eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "a vlan sub interface whose name is too long",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"verylongname.123"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "underlay NIC with invalid characters after dot",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0.100!"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty vtepCIDR specified",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{},
					Nics:           []string{"eth0"},
					ASN:            65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "multiple listen range neighbors with explicit neighbors",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.0/24"),
						},
						{
							Type:        new("internal"),
							ListenRange: new("192.168.11.0/24"),
						},
						{
							Type:        new("internal"),
							ListenRange: new("fd00:10::/64"),
						},
						{
							Type:    new("internal"),
							Address: new("192.168.10.3"),
						},
						{
							Type:    new("internal"),
							Address: new("192.168.10.4"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "multiple neighbors without address do not collide",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:       new(int64(65002)),
							Interface: new("eth0"),
						},
						{
							ASN:       new(int64(65002)),
							Interface: new("eth1"),
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate listen range",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.0/24"),
						},
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.0/24"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "overlapping listen ranges",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.0/24"),
						},
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.128/25"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "listen ranges equal after masking host bits",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.1/24"),
						},
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.0/24"),
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid listen range",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							Type:        new("internal"),
							ListenRange: new("192.168.10.5"),
						},
					},
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
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.1.0/24"},
					},
					Nics: []string{"eth0"},
					ASN:  65001,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65002)),
							Address: new("192.168.1.1"),
						},
					},
				},
			},
			{
				Spec: v1alpha1.UnderlaySpec{
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"192.168.2.0/24"},
					},
					Nics: []string{"eth1"},
					ASN:  65002,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(65003)),
							Address: new("192.168.2.1"),
						},
					},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65003)),
								Address: new("192.168.1.1"),
							},
						},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65003)),
								Address: new("192.168.1.2"),
							},
						},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65003)),
								Address: new("192.168.2.1"),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.2.0/24"},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"invalid-cidr"}, // Invalid VTEP CIDR
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65003)),
								Address: new("192.168.1.1"),
							},
						},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65003)),
								Address: new("192.168.1.2"),
							},
						},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
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
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(65002)),
								Address: new("192.168.1.1"),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "node with underlay having same local and remote ASN (iBGP)",
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
							{ASN: new(int64(65001))},
						},
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "neighbor having ASN 0 with external",
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
							{ASN: new(int64(0)), Type: new("external")},
						},
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "neighbor having ASN 0 with internal",
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
							{ASN: new(int64(0)), Type: new("internal")},
						},
						Nics: []string{"eth0"},
					},
				},
			},
			wantErr: false,
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
