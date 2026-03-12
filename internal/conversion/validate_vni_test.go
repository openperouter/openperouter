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
			wantErr: false,
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
		{
			name: "overlapping L2GatewayIPs CIDR",
			vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1001,
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"192.168.1.0/24"},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.L2VNISpec{
						VNI:          1002,
						VRF:          ptr.To("test"),
						L2GatewayIPs: []string{"192.168.1.128/25"},
					},
					Status: v1alpha1.L2VNIStatus{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateL2VNIs(tt.vnis, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateL2VNIs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHasIPOverlap(t *testing.T) {
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
			wantErrContains: "IPNet 192.168.1.0/24 overlaps with IPNet 192.168.0.0/16",
		},
		{
			name:            "overlap - one subnet contains another - ordered the other way",
			ipNets:          []string{"192.168.1.0/24", "192.168.0.0/16"},
			wantErrContains: "IPNet 192.168.1.0/24 overlaps with IPNet 192.168.0.0/16",
		},
		{
			name:            "overlap - IPv6 one subnet contains another",
			ipNets:          []string{"2001:db8::/32", "2001:db8:1::/64"},
			wantErrContains: "IPNet 2001:db8:1::/64 overlaps with IPNet 2001:db8::/32",
		},
		{
			name:            "overlap - identical subnets",
			ipNets:          []string{"192.168.1.0/24", "192.168.1.0/24"},
			wantErrContains: "IPNet 192.168.1.0/24 overlaps with IPNet 192.168.1.0/24",
		},
		{
			name:            "overlap - multiple subnets with both overlapping",
			ipNets:          []string{"192.168.1.0/24", "192.168.2.0/24", "192.168.0.0/16"},
			wantErrContains: "IPNet 192.168.1.0/24 overlaps with IPNet 192.168.0.0/16",
		},
		{
			name:            "overlap - multiple subnets inside a larget subnet",
			ipNets:          []string{"192.168.0.0/16", "192.168.1.0/24", "192.168.2.0/24", "192.168.3.0/24"},
			wantErrContains: "IPNet 192.168.1.0/24 overlaps with IPNet 192.168.0.0/16",
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
			ipNets := make([]net.IPNet, len(tt.ipNets))
			for i, cidr := range tt.ipNets {
				_, ipnet, err := net.ParseCIDR(cidr)
				if err != nil {
					t.Fatalf("failed to parse CIDR %s: %v", cidr, err)
				}
				ipNets[i] = *ipnet
			}

			err := hasIPOverlap(ipNets)
			if tt.wantErrContains != "" {
				if err == nil {
					t.Errorf("hasIPOverlap() error = nil, want error containing %q", tt.wantErrContains)
				} else if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("hasIPOverlap() error = %v, want error containing %q", err, tt.wantErrContains)
				}
			} else if err != nil {
				t.Errorf("hasIPOverlap() error = %v, want nil", err)
			}
		})
	}
}

func TestHasSubnetOverlapInVRF(t *testing.T) {
	tests := []struct {
		name            string
		vnis            []vni
		wantErrContains string
	}{
		{
			name: "no overlap - different VRFs different subnets",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf2",
					subnetV4: mustParseCIDR("192.168.2.0/24"),
				},
			},
		},
		{
			name: "no overlap - same VRF different subnets",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.2.0/24"),
				},
			},
		},
		{
			name: "overlap - same VRF overlapping subnets",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.0.0/16"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
				},
			},
			wantErrContains: "IP overlap in VRF vrf1, err: IPNet 192.168.1.0/24 overlaps with IPNet 192.168.0.0/16",
		},
		{
			name: "no overlap - IPv6 different VRFs",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV6: mustParseCIDR("2001:db8::/64"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf2",
					subnetV6: mustParseCIDR("2001:db9::/64"),
				},
			},
		},
		{
			name: "overlap - IPv6 same VRF",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV6: mustParseCIDR("2001:db8::/32"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf1",
					subnetV6: mustParseCIDR("2001:db8:1::/64"),
				},
			},
			wantErrContains: "IP overlap in VRF vrf1, err: IPNet 2001:db8:1::/64 overlaps with IPNet 2001:db8::/32",
		},
		{
			name: "no overlap - dual stack different VRFs",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
					subnetV6: mustParseCIDR("2001:db8::/64"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf2",
					subnetV4: mustParseCIDR("192.168.2.0/24"),
					subnetV6: mustParseCIDR("2001:db9::/64"),
				},
			},
		},
		{
			name: "overlap - dual stack same VRF IPv4 overlap",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.0.0/16"),
					subnetV6: mustParseCIDR("2001:db8::/64"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
					subnetV6: mustParseCIDR("2001:db9::/64"),
				},
			},
			wantErrContains: "IP overlap in VRF vrf1, err: IPNet 192.168.1.0/24 overlaps with IPNet 192.168.0.0/16",
		},
		{
			name: "overlap - dual stack same VRF IPv6 overlap",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
					subnetV6: mustParseCIDR("2001:db8::/32"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.2.0/24"),
					subnetV6: mustParseCIDR("2001:db8:1::/64"),
				},
			},
			wantErrContains: "IP overlap in VRF vrf1, err: IPNet 2001:db8:1::/64 overlaps with IPNet 2001:db8::/32",
		},
		{
			name: "no overlap - multiple VRFs with same subnets in different VRFs",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf2",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
				},
			},
		},
		{
			name: "no overlap - VNIs with nil subnets",
			vnis: []vni{
				{
					name:     "vni1",
					vni:      1001,
					vrfName:  "vrf1",
					subnetV4: nil,
				},
				{
					name:     "vni2",
					vni:      1002,
					vrfName:  "vrf1",
					subnetV4: mustParseCIDR("192.168.1.0/24"),
				},
			},
		},
		{
			name: "empty VNI slice",
			vnis: []vni{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := hasSubnetOverlapInVRF(tt.vnis)
			if tt.wantErrContains != "" {
				if err == nil {
					t.Errorf("hasSubnetOverlapInVRF() error = nil, want error containing %q", tt.wantErrContains)
				} else if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("hasSubnetOverlapInVRF() error = %v, want error containing %q", err, tt.wantErrContains)
				}
			} else if err != nil {
				t.Errorf("hasSubnetOverlapInVRF() error = %v, want nil", err)
			}
		})
	}
}

// mustParseCIDR is a helper function that parses a CIDR string and panics on error
func mustParseCIDR(cidr string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR: %s", cidr))
	}
	return ipnet
}
