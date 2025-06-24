// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestValidateVNIs(t *testing.T) {
	tests := []struct {
		name    string
		vnis    []v1alpha1.L3VNI
		wantErr bool
	}{
		{
			name: "valid VNIs",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.1.0/24",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1002,
						LocalCIDR: "192.168.2.0/24",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate VRF name",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.1.0/24",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1002,
						LocalCIDR: "192.168.2.0/24",
						VRF:       ptr.To("vni1"),
					},
					Status: v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "overlapping CIDRs",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.1.0/24",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1002,
						LocalCIDR: "192.168.1.128/25",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate VNI",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.1.0/24",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.2.0/24",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid localcidr",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:       100,
						LocalCIDR: "not-a-cidr",
					},
					Status: v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateL3VNIs(tt.vnis)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateL3VNIs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateL2VNIs(t *testing.T) {
	tests := []struct {
		name    string
		vnis    []v1alpha1.L2VNI
		wantErr bool
	}{
		{
			name: "valid L2VNIs",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
					},
					Status: v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1002,
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate VRF name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						VRF: ptr.To("vrf1"),
					},
					Status: v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1002,
						VRF: ptr.To("vrf1"),
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate VNI",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
					},
					Status: v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid VRF name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						VRF: ptr.To("invalid-vrf-name-with-dashes"),
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid hostmaster name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						HostMaster: &v1alpha1.HostMaster{
							Name: "invalid-hostmaster-name-with-dashes",
						},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "valid hostmaster name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						HostMaster: &v1alpha1.HostMaster{
							Name: "validhostmaster",
						},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "nil hostmaster name with autocreate true",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						HostMaster: &v1alpha1.HostMaster{
							AutoCreate: true,
						},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIP IPv4 CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:         1001,
						L2GatewayIP: "192.168.1.0/24",
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIP IPv6 CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:         1001,
						L2GatewayIP: "2001:db8::/64",
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid L2GatewayIP CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:         1001,
						L2GatewayIP: "invalid-cidr-format",
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateL2VNIs(tt.vnis)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateL2VNIs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
