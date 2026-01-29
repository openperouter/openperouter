// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/testutils"
	"k8s.io/utils/ptr"
)

const (
	vtepTestNS        = "vtepconvtest"
	vtepTestInterface = "vteptestif"
)

func vtepTestNSPath() string {
	return fmt.Sprintf("/var/run/netns/%s", vtepTestNS)
}

func TestAPItoFRR(t *testing.T) {
	tests := []struct {
		name          string
		nodeIndex     int
		underlays     []v1alpha1.Underlay
		vnis          []v1alpha1.L3VNI
		l3Passthrough []v1alpha1.L3Passthrough
		logLevel      string
		want          frr.Config
		wantErr       bool
		errContains   string
		// needsNetNS indicates the test requires a network namespace with vtepInterface.
		// When true, the test will skip if unprivileged and will setup a netns with the
		// addresses specified in vtepAddresses.
		needsNetNS    bool
		vtepAddresses []string // addresses to add to the vtep interface when needsNetNS is true
	}{
		{
			name:          "no underlays",
			nodeIndex:     0,
			underlays:     []v1alpha1.Underlay{},
			vnis:          []v1alpha1.L3VNI{{}},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantErr:       true,
		},
		{
			name:      "no vnis",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "ipv4 only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						HostSession: &v1alpha1.HostSession{
							ASN: 65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.2.0/24",
							},
							HostASN: 65001,
						},
						VRF: "vrf1",
						VNI: 200,
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "ipv6 only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						HostSession: &v1alpha1.HostSession{
							ASN: 65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv6: "2001:db8::/64",
							},
							HostASN: 65001,
						},
						VRF: "vrf1",
						VNI: 200,
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "2001:db8::2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "dual stack",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						HostSession: &v1alpha1.HostSession{
							ASN: 65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.2.0/24",
								IPv6: "2001:db8::/64",
							},
							HostASN: 65001,
						},
						VRF: "vrf1",
						VNI: 200,
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
					},
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "2001:db8::2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "BFD with custom settings",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: "192.168.1.100",
								ASN:     65001,
								BFD: &v1alpha1.BFDSettings{
									ReceiveInterval:  ptr.To(uint32(300)),
									TransmitInterval: ptr.To(uint32(300)),
									DetectMultiplier: ptr.To(uint32(3)),
									EchoMode:         ptr.To(false),
									PassiveMode:      ptr.To(false),
								},
							},
						},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.100",
							ASN:          65001,
							Addr:         "192.168.1.100",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
							BFDEnabled:   true,
							BFDProfile:   "neighbor-192.168.1.100",
						},
					},
				},
				VNIs: []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{
					{
						Name:             "neighbor-192.168.1.100",
						ReceiveInterval:  ptr.To(uint32(300)),
						TransmitInterval: ptr.To(uint32(300)),
						DetectMultiplier: ptr.To(uint32(3)),
					},
				},
				Loglevel: "debug",
			},
			wantErr: false,
		},
		{
			name:      "BFD enabled without settings",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: "192.168.1.100",
								ASN:     65001,
								BFD:     &v1alpha1.BFDSettings{},
							},
						},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.100",
							ASN:          65001,
							Addr:         "192.168.1.100",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
							BFDEnabled:   true,
							BFDProfile:   "",
						},
					},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "vni without host session",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF: "vrf1",
						VNI: 200,
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "empty routeridcidr uses default",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						HostSession: &v1alpha1.HostSession{
							ASN: 65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.2.0/24",
							},
							HostASN: 65001,
						},
						VRF: "vni1",
						VNI: 200,
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "vni1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "missing EVPN parameter",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "L3 passthrough",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: 65001,
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.2.0/24",
								IPv6: "2001:db8::/64",
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:  65001,
						Addr: "192.168.2.2",
					},
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:  65001,
						Addr: "2001:db8::2",
					},
					ToAdvertiseIPv4: []string{"192.168.2.2/32"},
					ToAdvertiseIPv6: []string{"2001:db8::2/128"},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		// VTEPInterface test cases - require network namespace
		{
			name:      "vtepInterface IPv4 address returns /32 mask",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: vtepTestInterface,
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			needsNetNS:    true,
			vtepAddresses: []string{"192.168.100.1/24"},
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.100.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "vtepInterface only IPv6 address returns error",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: vtepTestInterface,
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			needsNetNS:    true,
			vtepAddresses: []string{"fd00::1/64"},
			wantErr:       true,
			errContains:   "missing ipv4",
		},
		{
			name:      "vtepInterface dual-stack uses IPv4",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: vtepTestInterface,
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			needsNetNS:    true,
			vtepAddresses: []string{"192.168.100.1/24", "fd00::1/64"},
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.100.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "vtepInterface no addresses returns error",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: vtepTestInterface,
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			needsNetNS:    true,
			vtepAddresses: []string{},
			wantErr:       true,
			errContains:   "missing addresses",
		},
		{
			name:      "vtepInterface IPv6 first then IPv4 uses IPv4",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: vtepTestInterface,
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			needsNetNS:    true,
			vtepAddresses: []string{"fd00::1/64", "10.0.0.5/16"},
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "10.0.0.5/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "vtepInterface multiple IPv4 uses first",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: vtepTestInterface,
						},
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			needsNetNS:    true,
			vtepAddresses: []string{"192.168.1.1/24", "10.0.0.1/8"},
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs:        []frr.L3VNIConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var targetNS string

			if tt.needsNetNS {
				testutils.SkipUnlessPrivilegedT(t)

				// Setup: create network namespace with interface
				cleanupVtepTest(t)
				t.Cleanup(func() { cleanupVtepTest(t) })

				ns := createVtepTestNS(t)
				defer ns.Close()

				// Create interface with addresses inside the namespace
				err := netnamespace.In(ns, func() error {
					dummy := &netlink.Dummy{
						LinkAttrs: netlink.LinkAttrs{Name: vtepTestInterface},
					}
					if err := netlink.LinkAdd(dummy); err != nil {
						return fmt.Errorf("failed to add dummy interface: %w", err)
					}

					link, err := netlink.LinkByName(vtepTestInterface)
					if err != nil {
						return fmt.Errorf("failed to get interface: %w", err)
					}

					for _, addrStr := range tt.vtepAddresses {
						addr, err := netlink.ParseAddr(addrStr)
						if err != nil {
							return fmt.Errorf("failed to parse address %s: %w", addrStr, err)
						}
						if err := netlink.AddrAdd(link, addr); err != nil {
							return fmt.Errorf("failed to add address %s: %w", addrStr, err)
						}
					}

					if err := netlink.LinkSetUp(link); err != nil {
						return fmt.Errorf("failed to set link up: %w", err)
					}

					return nil
				})
				if err != nil {
					t.Fatalf("failed to setup test interface: %v", err)
				}

				targetNS = vtepTestNSPath()
			}

			apiConfig := ApiConfigData{
				NodeIndex:     tt.nodeIndex,
				Underlays:     tt.underlays,
				L3VNIs:        tt.vnis,
				L3Passthrough: tt.l3Passthrough,
				LogLevel:      tt.logLevel,
				TargetNS:      targetNS,
			}
			got, err := APItoFRR(apiConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("APItoFRR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("APItoFRR() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if !cmp.Equal(got, tt.want) {
				t.Errorf("APItoFRR() = %v, diff %s", got, cmp.Diff(got, tt.want))
			}
		})
	}
}

func createVtepTestNS(t *testing.T) netns.NsHandle {
	t.Helper()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	currentNs, err := netns.Get()
	if err != nil {
		t.Fatalf("failed to get current namespace: %v", err)
	}

	newNs, err := netns.NewNamed(vtepTestNS)
	if err != nil {
		t.Fatalf("failed to create test namespace: %v", err)
	}

	if err := netns.Set(currentNs); err != nil {
		t.Fatalf("failed to restore namespace: %v", err)
	}

	return newNs
}

func cleanupVtepTest(t *testing.T) {
	t.Helper()
	err := netns.DeleteNamed(vtepTestNS)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Logf("warning: failed to delete test namespace: %v", err)
	}
}
