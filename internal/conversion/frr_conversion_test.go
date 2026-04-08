// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"k8s.io/utils/ptr"
)

func TestAPItoFRR(t *testing.T) {
	tests := []struct {
		name          string
		nodeIndex     int
		underlays     []v1alpha1.Underlay
		vnis          []v1alpha1.L3VNI
		vpns          []v1alpha1.L3VPN
		l3Passthrough []v1alpha1.L3Passthrough
		logLevel      string
		want          frr.Config
		wantErr       bool
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs: []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
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
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "vtepInterface sets EVPN with empty VTEP",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: "eth1",
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
					MyASN:    65000,
					EVPN:     &frr.UnderlayEvpn{},
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
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "ISIS",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Type: 1,
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
							{
								Name: "ISIS2",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: []frr.UnderlayISIS{
						{
							Name: "ISIS",
							Net:  []frr.ISISNet{frr.MustParseISISNet("49.0001.0002.0003.0004.00")},
							Type: 1,
							Interfaces: []frr.ISISInterface{
								{Name: "eth0", IPv4: true, IPv6: true},
								{Name: "eth1", IPv4: false, IPv6: true},
							},
						},
						{
							Name: "ISIS2",
							Net:  []frr.ISISNet{frr.MustParseISISNet("49.0001.0002.0003.0004.00")},
							Type: 0,
							Interfaces: []frr.ISISInterface{
								{Name: "eth0", IPv4: true, IPv6: true},
								{Name: "eth1", IPv4: false, IPv6: true},
							},
						},
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
				Passthrough: nil,
				VNIs:        []frr.L3VNIConfig{},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "ISIS without interface configuration",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Type: 2,
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: []frr.UnderlayISIS{
						{
							Name:       "ISIS",
							Net:        []frr.ISISNet{frr.MustParseISISNet("49.0001.0002.0003.0004.00")},
							Type:       2,
							Interfaces: []frr.ISISInterface{},
						},
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
				Passthrough: nil,
				VNIs:        []frr.L3VNIConfig{},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "ISIS name unset",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Type: 1,
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want:          frr.Config{},
			wantErr:       true,
		},
		{
			name:      "ISIS invalid net",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.000"},
								Type: 1,
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want:          frr.Config{},
			wantErr:       true,
		},
		{
			name:      "ISIS net unset",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS",
								Type: 1,
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want:          frr.Config{},
			wantErr:       true,
		},
		{
			name:      "ISIS duplicate interface names",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Type: 1,
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth0", IPv4: false, IPv6: true},
								},
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want:          frr.Config{},
			wantErr:       true,
		},
		{
			name:      "ISIS invalid type",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Type: 3,
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want:          frr.Config{},
			wantErr:       true,
		},
		{
			name:      "ISIS duplicate process names",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS1",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
							{
								Name: "ISIS1",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
									{Name: "eth1", IPv4: false, IPv6: true},
								},
							},
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want:          frr.Config{},
			wantErr:       true,
		},
		{
			name:      "SRV6 dual-stack",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						ISIS: []v1alpha1.ISISConfig{
							{
								Name: "ISIS",
								Net:  []v1alpha1.ISISNet{"49.0001.0002.0003.0004.00"},
								Type: 1,
								Interfaces: []v1alpha1.ISISInterface{
									{Name: "eth0", IPv4: true, IPv6: true},
								},
							},
						},
						SRV6: &v1alpha1.SRV6Config{
							Source: v1alpha1.SRV6Source{
								CIDR: "2001:db8:1234:5678::/64",
							},
							Locator: v1alpha1.SRV6Locator{
								Name:   "MAIN",
								Prefix: "fd00:0:32::/48",
								Format: "usid-f3216",
							},
						},
					},
				},
			},
			vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VPNSpec{
						HostSession: &v1alpha1.HostSession{
							ASN: 65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.2.0/24",
								IPv6: "2001:db8::/64",
							},
							HostASN: 65001,
						},
						VRF:                      "vrf1",
						RouteTarget:              "65000:100",
						RouteDistinguisherSuffix: 100,
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: []frr.UnderlayISIS{
						{
							Name: "ISIS",
							Net:  []frr.ISISNet{frr.MustParseISISNet("49.0001.0002.0003.0004.00")},
							Type: 1,
							Interfaces: []frr.ISISInterface{
								{Name: "eth0", IPv4: true, IPv6: true},
							},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.None,
							EBGPMultiHop: false,
						},
					},
					SegmentRouting: &frr.UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1234:5678::",
						Locator: frr.SRV6Locator{
							Name:     "MAIN",
							Prefix:   "fd00:0:32::/48",
							BlockLen: 32,
							NodeLen:  16,
							Behavior: "usid",
							Format:   "usid-f3216",
						},
					},
				},
				Passthrough: nil,
				VNIs:        []frr.L3VNIConfig{},
				VPNs: []frr.L3VPNConfig{
					{
						ASN:                65000,
						ToAdvertiseIPv4:    []string{"192.168.2.2/32"},
						ToAdvertiseIPv6:    []string{},
						LocalNeighbor:      &frr.NeighborConfig{ASN: 65001, Addr: "192.168.2.2"},
						VRF:                "vrf1",
						RouteTarget:        "65000:100",
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
					{
						ASN:                65000,
						ToAdvertiseIPv4:    []string{},
						ToAdvertiseIPv6:    []string{"2001:db8::2/128"},
						LocalNeighbor:      &frr.NeighborConfig{ASN: 65001, Addr: "2001:db8::2"},
						VRF:                "vrf1",
						RouteTarget:        "65000:100",
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := ApiConfigData{
				Underlays:     tt.underlays,
				L3VNIs:        tt.vnis,
				L3Passthrough: tt.l3Passthrough,
				L3VPNs:        tt.vpns,
			}
			got, err := APItoFRR(apiConfig, tt.nodeIndex, tt.logLevel)
			if (err != nil) != tt.wantErr {
				t.Errorf("APItoFRR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if !cmp.Equal(got, tt.want) {
				t.Errorf("APItoFRR() = %v, diff %s", got, cmp.Diff(got, tt.want))
			}
		})
	}
}

func TestAPItoFRRRawConfig(t *testing.T) {
	baseUnderlay := []v1alpha1.Underlay{
		{
			Spec: v1alpha1.UnderlaySpec{
				ASN:          65000,
				RouterIDCIDR: "10.0.0.0/24",
				Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
			},
		},
	}

	tests := []struct {
		name          string
		rawFRRConfigs []v1alpha1.RawFRRConfig
		wantSnippets  []frr.RawFRRSnippet
	}{
		{
			name:          "no raw configs",
			rawFRRConfigs: nil,
			wantSnippets:  nil,
		},
		{
			name: "single raw config",
			rawFRRConfigs: []v1alpha1.RawFRRConfig{
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						RawConfig: "ip prefix-list test seq 10 permit 10.0.0.0/8",
					},
				},
			},
			wantSnippets: []frr.RawFRRSnippet{
				{Priority: 0, Config: "ip prefix-list test seq 10 permit 10.0.0.0/8"},
			},
		},
		{
			name: "multiple raw configs sorted by priority",
			rawFRRConfigs: []v1alpha1.RawFRRConfig{
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  20,
						RawConfig: "high priority config",
					},
				},
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  5,
						RawConfig: "low priority config",
					},
				},
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  10,
						RawConfig: "mid priority config",
					},
				},
			},
			wantSnippets: []frr.RawFRRSnippet{
				{Priority: 5, Config: "low priority config"},
				{Priority: 10, Config: "mid priority config"},
				{Priority: 20, Config: "high priority config"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := ApiConfigData{
				Underlays:     baseUnderlay,
				RawFRRConfigs: tt.rawFRRConfigs,
			}
			got, err := APItoFRR(apiConfig, 0, "debug")
			if err != nil {
				t.Fatalf("APItoFRR() unexpected error: %v", err)
			}
			if !cmp.Equal(got.RawConfig, tt.wantSnippets) {
				t.Errorf("APItoFRR() RawConfig diff: %s", cmp.Diff(got.RawConfig, tt.wantSnippets))
			}
		})
	}
}

func TestAPItoFRRRawConfigWithoutUnderlay(t *testing.T) {
	rawConfigs := []v1alpha1.RawFRRConfig{
		{
			Spec: v1alpha1.RawFRRConfigSpec{
				Priority:  10,
				RawConfig: "ip prefix-list test seq 10 permit 10.0.0.0/8",
			},
		},
		{
			Spec: v1alpha1.RawFRRConfigSpec{
				Priority:  5,
				RawConfig: "route-map test permit 10",
			},
		},
	}

	wantConfig := frr.Config{
		Loglevel: "debug",
		RawConfig: []frr.RawFRRSnippet{
			{Priority: 5, Config: "route-map test permit 10"},
			{Priority: 10, Config: "ip prefix-list test seq 10 permit 10.0.0.0/8"},
		},
	}

	tests := []struct {
		name          string
		l3VNIs        []v1alpha1.L3VNI
		l3Passthrough []v1alpha1.L3Passthrough
	}{
		{
			name: "raw config only",
		},
		{
			name: "raw config with L3VNIs ignores VNIs",
			l3VNIs: []v1alpha1.L3VNI{
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
		},
		{
			name: "raw config with L3Passthrough ignores passthrough",
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: 65001,
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.2.0/24",
							},
						},
					},
				},
			},
		},
		{
			name: "raw config with both L3VNIs and L3Passthrough ignores both",
			l3VNIs: []v1alpha1.L3VNI{
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
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: 65001,
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.168.2.0/24",
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := ApiConfigData{
				Underlays:     []v1alpha1.Underlay{},
				RawFRRConfigs: rawConfigs,
				L3VNIs:        tt.l3VNIs,
				L3Passthrough: tt.l3Passthrough,
			}
			got, err := APItoFRR(apiConfig, 0, "debug")
			if err != nil {
				t.Fatalf("APItoFRR() unexpected error: %v", err)
			}

			if !cmp.Equal(got, wantConfig) {
				t.Errorf("APItoFRR() diff: %s", cmp.Diff(got, wantConfig))
			}
		})
	}
}
