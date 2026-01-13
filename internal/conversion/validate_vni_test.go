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
			name: "valid VNIs IPv4 only",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.2.0/24"}},
					},
					Status: v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid VNIs IPv6 only",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "2001:db8::/64"}},
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "2001:db9::/64"}},
					},
					Status: v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid VNIs dual stack",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24", IPv6: "2001:db8::/64"}},
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.2.0/24", IPv6: "2001:db9::/64"}},
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
						VNI:         1001,
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.2.0/24"}},
						VRF:         "vni1",
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
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: 65002, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.1.0/24"}},
					},
					Status: v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: 65004, LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "192.168.2.0/24"}},
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
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								Name: "invalid-hostmaster-name-with-dashes",
							},
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
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								Name: "validhostmaster",
							},
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
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								AutoCreate: true,
							},
						},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIPs IPv4 CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIPs IPv6 CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						L2GatewayIPs: []string{"2001:db8::/64"},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIPs dual-stack",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						L2GatewayIPs: []string{"192.168.1.0/24", "2001:db8::/64"},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "ivalid L2GatewayIPs dual-stack both ipv4",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						L2GatewayIPs: []string{"192.168.1.0/24", "192.168.2.0/24"},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "ivalid L2GatewayIPs dual-stack both ipv6",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						L2GatewayIPs: []string{"2002:db8::/64", "2001:db8::/64"},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},

		{
			name: "invalid L2GatewayIPs CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						L2GatewayIPs: []string{"invalid-cidr-format"},
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
