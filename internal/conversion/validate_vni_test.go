// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"
	"strings"
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
						VNI:         ptr.To(int64(1001)),
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1002)),
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: ptr.To(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
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
						VNI:         ptr.To(int64(1001)),
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: ptr.To("2001:db8::/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1002)),
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: ptr.To(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: ptr.To("2001:db9::/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
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
						VNI:         ptr.To(int64(1001)),
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.1.0/24"), IPv6: ptr.To("2001:db8::/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1002)),
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: ptr.To(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.2.0/24"), IPv6: ptr.To("2001:db9::/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate VNI",
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1001)),
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1001)),
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: ptr.To(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.2.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
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
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1002)),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate VRF name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
						VRF: ptr.To("vrf1"),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1002)),
						VRF: ptr.To("vrf1"),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate VNI",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid VRF name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
						VRF: ptr.To("invalid-vrf-name-with-dashes"),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid hostmaster name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
						HostMaster: &v1alpha1.HostMaster{
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								Name: ptr.To("invalid-hostmaster-name-with-dashes"),
							},
						},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "valid hostmaster name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
						HostMaster: &v1alpha1.HostMaster{
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								Name: ptr.To("validhostmaster"),
							},
						},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "nil hostmaster name with autocreate true",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI: ptr.To(int64(1001)),
						HostMaster: &v1alpha1.HostMaster{
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								AutoCreate: ptr.To(true),
							},
						},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIPs IPv4 CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test-vrf"),
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIPs IPv6 CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test-vrf"),
						L2GatewayIPs: []string{"2001:db8::/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "valid L2GatewayIPs dual-stack",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test-vrf"),
						L2GatewayIPs: []string{"192.168.1.0/24", "2001:db8::/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "l2gatewayips without VRF",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "ivalid L2GatewayIPs dual-stack both ipv4",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test-vrf"),
						L2GatewayIPs: []string{"192.168.1.0/24", "192.168.2.0/24"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "ivalid L2GatewayIPs dual-stack both ipv6",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test-vrf"),
						L2GatewayIPs: []string{"2002:db8::/64", "2001:db8::/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid L2GatewayIPs CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test-vrf"),
						L2GatewayIPs: []string{"invalid-cidr-format"},
					},
					Status: &v1alpha1.L2VNIStatus{},
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

func TestValidateVRFs(t *testing.T) {
	tests := []struct {
		name       string
		l2vnis     []v1alpha1.L2VNI
		l3vnis     []v1alpha1.L3VNI
		wantErrStr string
	}{
		{
			name: "overlapping L2GatewayIPs CIDR in different VRFs",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("test2"),
						L2GatewayIPs: []string{"192.168.1.128/25"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
		},
		{
			name: "overlapping L2GatewayIPs CIDR V6 in different VRFs",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"2000::/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("test2"),
						L2GatewayIPs: []string{"2000::/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
		},
		{
			name: "overlapping L2GatewayIPs CIDR in same VRF",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"192.168.1.128/25"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErrStr: "subnet overlap in VRF \"test\": IPNet 192.168.1.128/25 (L2VNI test/vni2) overlaps with IPNet 192.168.1.0/24 (L2VNI test/vni1)",
		},
		{
			name: "overlapping L2GatewayIPs CIDR V6 in same VRF",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1001)),
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"2000::1/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"2000:0:0:0:1::1/80"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErrStr: "subnet overlap in VRF \"test\": IPNet 2000::1:0:0:0/80 (L2VNI test/vni2) overlaps with IPNet 2000::/64 (L2VNI test/vni1)",
		},
		{
			name: "l3VNI and L2GatewayIPs CIDR overlap in same VRF",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("vni1"),
						L2GatewayIPs: []string{"192.168.1.128/25"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1001)),
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantErrStr: "subnet overlap in VRF \"vni1\": IPNet 192.168.1.128/25 (L2VNI test/vni2) overlaps with IPNet 192.168.1.0/24 (L3VNI test/vni1)",
		},
		{
			name: "l3VNI and L2GatewayIPs CIDR V6 overlap in same VRF",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("vni1"),
						L2GatewayIPs: []string{"2000:0:0:0:1::1/80"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1001)),
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: ptr.To("2000::1/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantErrStr: "subnet overlap in VRF \"vni1\": IPNet 2000::1:0:0:0/80 (L2VNI test/vni2) overlaps with IPNet 2000::/64 (L3VNI test/vni1)",
		},
		{
			name: "l3VNI and L2GatewayIPs CIDR overlap in different VRFs",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"192.168.1.128/25"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1001)),
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
		},
		{
			name: "l3VNI and L2GatewayIPs CIDR V6 overlap in different VRFs",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(1002)),
						VRF:          ptr.To("vni1"),
						L2GatewayIPs: []string{"2000:0:0:0:1::1/80"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1001)),
						VRF:         "vni2",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: ptr.To("2000::1/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
		},
		{
			name: "more than one L3VNI in the same VRF",
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1001)),
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: ptr.To(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         ptr.To(int64(1002)),
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: ptr.To(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: ptr.To("192.168.2.0/24")}},
						VRF:         "vni1",
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantErrStr: "more than one L3VNI detected in VRF \"vni1\": test/vni1 - test/vni2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVRFs(tt.l2vnis, tt.l3vnis)
			if tt.wantErrStr == "" && err != nil {
				t.Errorf("ValidateVRFs() no error expected but got error = %q", err)
			}
			if tt.wantErrStr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrStr)) {
				t.Errorf("ValidateVRFs() error expected but got error = %q, wantErr %q", err, tt.wantErrStr)
			}
		})
	}
}

func TestHasSubnetOverlap(t *testing.T) {
	tests := []struct {
		name            string
		ipNets          []string
		wantErrContains string
	}{
		{
			name:   "no overlap - different subnets",
			ipNets: []string{"192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"},
		},
		{
			name:   "no overlap - IPv6 different subnets",
			ipNets: []string{"2001:db8::/64", "2001:db9::/64"},
		},
		{
			name:            "overlap - one subnet contains another",
			ipNets:          []string{"192.168.0.0/16", "192.168.1.0/24"},
			wantErrContains: "IPNet 192.168.1.0/24 (element 1) overlaps with IPNet 192.168.0.0/16 (element 0)",
		},
		{
			name:            "overlap - one subnet contains another - ordered the other way",
			ipNets:          []string{"192.168.1.0/24", "192.168.0.0/16"},
			wantErrContains: "IPNet 192.168.1.0/24 (element 0) overlaps with IPNet 192.168.0.0/16 (element 1)",
		},
		{
			name:            "overlap - larger subnet contains smaller",
			ipNets:          []string{"192.168.0.0/24", "192.168.2.0/16"},
			wantErrContains: "IPNet 192.168.0.0/16 (element 1) overlaps with IPNet 192.168.0.0/24 (element 0)",
		},
		{
			name:            "overlap - IPv6 one subnet contains another",
			ipNets:          []string{"2001:db8::/32", "2001:db8:1::/64"},
			wantErrContains: "IPNet 2001:db8:1::/64 (element 1) overlaps with IPNet 2001:db8::/32 (element 0)",
		},
		{
			name:            "overlap - identical subnets",
			ipNets:          []string{"192.168.1.0/24", "192.168.1.0/24"},
			wantErrContains: "IPNet 192.168.1.0/24 (element 1) overlaps with IPNet 192.168.1.0/24 (element 0)",
		},
		{
			name:            "overlap - multiple subnets with both overlapping",
			ipNets:          []string{"192.168.1.0/24", "192.168.2.0/24", "192.168.0.0/16"},
			wantErrContains: "IPNet 192.168.1.0/24 (element 0) overlaps with IPNet 192.168.0.0/16 (element 2)",
		},
		{
			name:            "overlap - multiple subnets inside a larger subnet",
			ipNets:          []string{"192.168.0.0/16", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"},
			wantErrContains: "IPNet 192.168.1.0/24 (element 1) overlaps with IPNet 192.168.0.0/16 (element 0)",
		},
		{
			name:   "single subnet - no overlap",
			ipNets: []string{"192.168.1.0/24"},
		},
		{
			name:   "empty slice - no overlap",
			ipNets: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vniSubnets := make(subnets, len(tt.ipNets))
			for i, cidr := range tt.ipNets {
				_, ipnet, err := net.ParseCIDR(cidr)
				if err != nil {
					t.Fatalf("failed to parse CIDR %s: %v", cidr, err)
				}
				vniSubnets[i] = subnetWithSource{
					source: fmt.Sprintf("element %d", i),
					subnet: ipnet,
				}
			}

			vniSubnets.sort()
			err := hasSubnetOverlap(vniSubnets)
			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("hasIPOverlap() error = nil, want error containing %q", tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("hasIPOverlap() error = %v, want error containing %q", err, tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Errorf("hasIPOverlap() error = %v, want nil", err)
			}
		})
	}
}
