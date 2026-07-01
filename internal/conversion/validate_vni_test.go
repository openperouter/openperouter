// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterValidL3VNIs(t *testing.T) {
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
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
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
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: new("2001:db8::/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: new("2001:db9::/64")}},
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
						VNI:         1001,
						VRF:         "vrf1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24"), IPv6: new("2001:db8::/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vrf2",
						HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24"), IPv6: new("2001:db9::/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FilterValidL3VNIs(tt.vnis)
			if (err != nil) != tt.wantErr {
				t.Errorf("FilterValidL3VNIs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterValidL2VNIs(t *testing.T) {
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
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1002,
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
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						VRF: new("vrf1"),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1002,
						VRF: new("vrf1"),
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid VRF name",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						VRF: new("invalid-vrf-name-with-dashes"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						HostMaster: &v1alpha1.HostMaster{
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								Name: new("invalid-hostmaster-name-with-dashes"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						HostMaster: &v1alpha1.HostMaster{
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								Name: new("validhostmaster"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
						HostMaster: &v1alpha1.HostMaster{
							Type: "linux-bridge",
							LinuxBridge: &v1alpha1.LinuxBridgeConfig{
								AutoCreate: new(true),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test-vrf"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test-vrf"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test-vrf"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "disconnected L2VNI with long name passes validation",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "this-name-is-too-long-for-iface"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
					},
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
						VRF:          new("test-vrf"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test-vrf"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test-vrf"),
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
			_, err := FilterValidL2VNIs(tt.vnis)
			if (err != nil) != tt.wantErr {
				t.Errorf("FilterValidL2VNIs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterValidVRFSubnets(t *testing.T) {
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test"),
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("test2"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test"),
						L2GatewayIPs: []string{"2000::/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("test2"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test"),
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("test"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          new("test"),
						L2GatewayIPs: []string{"2000::1/64"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("test"),
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("vni1"),
						L2GatewayIPs: []string{"192.168.1.128/25"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("vni1"),
						L2GatewayIPs: []string{"2000:0:0:0:1::1/80"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: new("2000::1/64")}},
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("test"),
						L2GatewayIPs: []string{"192.168.1.128/25"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
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
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          new("vni1"),
						L2GatewayIPs: []string{"2000:0:0:0:1::1/80"},
					},
					Status: &v1alpha1.L2VNIStatus{},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1001,
						VRF:         "vni2",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: new("2000::1/64")}},
					},
					Status: &v1alpha1.L3VNIStatus{},
				},
			},
		},
		{
			name: "disconnected L2VNI is skipped in subnet overlap checks",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L2VNISpec{
						VNI: 1001,
					},
				},
			},
			l3vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
					Spec: v1alpha1.L3VNISpec{
						VNI:         1002,
						VRF:         "vni1",
						HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := FilterValidVRFSubnets(tt.l3vnis, tt.l2vnis)
			if tt.wantErrStr == "" && err != nil {
				t.Errorf("FilterValidVRFSubnets() no error expected but got errors = %v", err)
			}
			if tt.wantErrStr != "" {
				if err == nil {
					t.Errorf("FilterValidVRFSubnets() error expected but got none, wantErr %q", tt.wantErrStr)
				} else if !strings.Contains(err.Error(), tt.wantErrStr) {
					t.Errorf("FilterValidVRFSubnets() error = %q, wantErr %q", err, tt.wantErrStr)
				}
			}
		})
	}
}

func TestFilterUniqueVRFs_DuplicateVRF(t *testing.T) {
	l3vnis := []v1alpha1.L3VNI{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "vni1", Namespace: "test"},
			Spec: v1alpha1.L3VNISpec{
				VNI:         1001,
				VRF:         "vni1",
				HostSession: &v1alpha1.HostSession{ASN: 65001, HostASN: new(int64(65002)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.1.0/24")}},
			},
			Status: &v1alpha1.L3VNIStatus{},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "vni2", Namespace: "test"},
			Spec: v1alpha1.L3VNISpec{
				VNI:         1002,
				HostSession: &v1alpha1.HostSession{ASN: 65003, HostASN: new(int64(65004)), LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.168.2.0/24")}},
				VRF:         "vni1",
			},
			Status: &v1alpha1.L3VNIStatus{},
		},
	}

	valid, err := FilterUniqueVRFs(l3vnis)
	if err == nil {
		t.Fatal("expected error for duplicate VRF")
	}
	wantErr := "more than one L3VNI detected in VRF \"vni1\": \"test/vni1\" already exists"
	if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("error = %q, want %q", err, wantErr)
	}
	if len(valid) != 1 || valid[0].Name != "vni1" {
		t.Errorf("expected first L3VNI to be valid, got %v", valid)
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

func TestValidateRouteTarget(t *testing.T) {
	tests := []struct {
		name          string
		routeTarget   string
		wantErrString string
	}{
		{
			name:        "ASN route target",
			routeTarget: "65000:100",
		},
		{
			name:        "single IP route target",
			routeTarget: "10.0.0.1:100",
		},
		{
			name:        "4-byte ASN route target",
			routeTarget: "100000:100",
		},
		{
			name:          "invalid route target missing colon",
			routeTarget:   "65000",
			wantErrString: "RT \"65000\" must have one of the following formats: 'ASN:MN' or 'IPv4Address:MN'",
		},
		{
			name:          "invalid route target non-numeric ASN",
			routeTarget:   "abc:100",
			wantErrString: "RT format must have ASN:MN: abc:100",
		},
		{
			name:          "invalid route target non-numeric member number",
			routeTarget:   "65000:abc",
			wantErrString: "RT format must have ASN:MN where MN is a number: 65000:abc",
		},
		{
			name:          "invalid IP route target member number exceeds 65535",
			routeTarget:   "10.0.0.1:65536",
			wantErrString: "RT format must have A.B.C.D:MN where MN <= 65535: 10.0.0.1:65536",
		},
		{
			name:          "invalid 4-byte ASN route target member number exceeds 65535",
			routeTarget:   "100000:65536",
			wantErrString: "RT format with 4-byte ASN must have ASN:MN where MN <= 65535: 100000:65536",
		},
		{
			name:          "invalid route target 'invalid'",
			routeTarget:   "invalid",
			wantErrString: "RT \"invalid\" must have one of the following formats: 'ASN:MN' or 'IPv4Address:MN'",
		},
		{
			name:          "empty string",
			routeTarget:   "",
			wantErrString: "RT \"\" must have one of the following formats: 'ASN:MN' or 'IPv4Address:MN'",
		},
		{
			name:          "multiple colons",
			routeTarget:   "65000:100:200",
			wantErrString: "RT \"65000:100:200\" must have one of the following formats: 'ASN:MN' or 'IPv4Address:MN'",
		},
		{
			name:          "invalid IP route target non-numeric member number",
			routeTarget:   "10.0.0.1:abc",
			wantErrString: "RT format must have A.B.C.D:MN where MN <= 65535: 10.0.0.1:abc",
		},
		{
			name:        "2-byte ASN with max member number",
			routeTarget: "65535:4294967295",
		},
		{
			name:          "2-byte ASN with member number exceeding max",
			routeTarget:   "65535:4294967296",
			wantErrString: "RT format with 2-byte ASN must have ASN:MN where MN <= 4294967295: 65535:4294967296",
		},
		{
			name:        "IP route target with max member number",
			routeTarget: "10.0.0.1:65535",
		},
		{
			name:        "4-byte ASN with max member number",
			routeTarget: "100000:65535",
		},
		{
			name:          "invalid IP route target invalid IP address",
			routeTarget:   "999.999.999.999:100",
			wantErrString: "RT format must have A.B.C.D:MN where A.B.C.D is a valid IPv4 address: 999.999.999.999:100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRouteTarget(tt.routeTarget)
			if tt.wantErrString != "" {
				if err == nil {
					t.Fatalf("validateRouteTarget() expected error %q, got nil", tt.wantErrString)
				}
				if err.Error() != tt.wantErrString {
					t.Fatalf("validateRouteTarget() error = %q, want %q", err.Error(), tt.wantErrString)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateRouteTarget() expected no error but got: %v", err)
			}
		})
	}
}
