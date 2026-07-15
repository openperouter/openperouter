// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/networklayerprotocol"
)

func TestAPItoFRR(t *testing.T) {
	tests := []struct {
		name          string
		nodeIndex     int
		underlays     []v1alpha1.Underlay
		vnis          []v1alpha1.L3VNI
		l2vnis        []v1alpha1.L2VNI
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:                  "65001@192.168.1.1",
							ASN:                   mustNewPeerASNFromNumber(65001),
							Addr:                  "192.168.1.1",
							ID:                    "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
							EBGPMultiHop:          false,
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
			name:      "parse underlay external",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), Type: new("external")}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:                  "external@192.168.1.1",
							ASN:                   mustNewPeerASNFromType("external"),
							Addr:                  "192.168.1.1",
							ID:                    "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
							EBGPMultiHop:          false,
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
			name:      "parse underlay internal",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("192.168.1.1"),
								Type:    new("internal"),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:                  "internal@192.168.1.1",
							ASN:                   mustNewPeerASNFromType("internal"),
							Addr:                  "192.168.1.1",
							ID:                    "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
							EBGPMultiHop:          false,
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
			name:      "parse underlay internal numeric",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("192.168.1.1"),
								ASN:     new(int64(65000)),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:                  "65000@192.168.1.1",
							ASN:                   mustNewPeerASNFromNumber(65000),
							Addr:                  "192.168.1.1",
							ID:                    "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
							EBGPMultiHop:          false,
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
								IPv4: new("192.168.2.0/24"),
							},
							HostASN: new(int64(65001)),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
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
							ID:   "192.168.2.2",
							ASN:  mustNewPeerASNFromNumber(65001),
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{{
							Address: new("2001:db8::1:192:168:1:1"),
							ASN:     new(int64(65001))}},
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
								IPv6: new("2001:db8::/64"),
							},
							HostASN: new(int64(65001)),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@2001:db8::1:192:168:1:1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::1:192:168:1:1",
							ID:   "2001:db8::1:192:168:1:1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop:    false,
							ExtendedNexthop: true,
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
							ID:   "2001:db8::2",
							ASN:  mustNewPeerASNFromNumber(65001),
						},
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
							HostASN: new(int64(65001)),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
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
							ID:   "192.168.2.2",
							ASN:  mustNewPeerASNFromNumber(65001),
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
					},
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
							ASN:  mustNewPeerASNFromNumber(65001),
						},
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("192.168.1.100"),
								ASN:     new(int64(65001)),
								BFD: &v1alpha1.BFDSettings{
									ReceiveInterval:  new(int32(300)),
									TransmitInterval: new(int32(300)),
									DetectMultiplier: new(int32(3)),
									EchoMode:         new(false),
									PassiveMode:      new(false),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:                  "65001@192.168.1.100",
							ASN:                   mustNewPeerASNFromNumber(65001),
							Addr:                  "192.168.1.100",
							ID:                    "192.168.1.100",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
							EBGPMultiHop:          false,
							BFDEnabled:            true,
							BFDProfile:            "neighbor-192.168.1.100",
						},
					},
				},
				VNIs: []frr.L3VNIConfig{},
				VPNs: []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{
					{
						Name:             "neighbor-192.168.1.100",
						ReceiveInterval:  new(int32(300)),
						TransmitInterval: new(int32(300)),
						DetectMultiplier: new(int32(3)),
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("192.168.1.100"),
								ASN:     new(int64(65001)),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:                  "65001@192.168.1.100",
							ASN:                   mustNewPeerASNFromNumber(65001),
							Addr:                  "192.168.1.100",
							ID:                    "192.168.1.100",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
							EBGPMultiHop:          false,
							BFDEnabled:            true,
							BFDProfile:            "",
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:       65000,
						VNI:       200,
						VRF:       "vrf1",
						RouterID:  "10.0.0.1",
						ExportRTs: []string{},
						ImportRTs: []string{},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "ipv4 with route targets",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
								IPv4: new("192.168.2.0/24"),
							},
							HostASN: new(int64(65001)),
						},
						VRF:       "vrf1",
						VNI:       200,
						ExportRTs: []v1alpha1.RouteTarget{"65000:1000"},
						ImportRTs: []v1alpha1.RouteTarget{"65000:2000", "10.0.0.1:3000"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
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
							ID:   "192.168.2.2",
							ASN:  mustNewPeerASNFromNumber(65001),
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						ExportRTs:       []string{"65000:1000"},
						ImportRTs:       []string{"65000:2000", "10.0.0.1:3000"},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "vni without host session with route targets",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF:       "vrf1",
						VNI:       200,
						ExportRTs: []v1alpha1.RouteTarget{"65000:100"},
						ImportRTs: []v1alpha1.RouteTarget{"65000:200"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:       65000,
						VNI:       200,
						VRF:       "vrf1",
						RouterID:  "10.0.0.1",
						ExportRTs: []string{"65000:100"},
						ImportRTs: []string{"65000:200"},
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
								IPv4: new("192.168.2.0/24"),
							},
							HostASN: new(int64(65001)),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
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
							ID:   "192.168.2.2",
							ASN:  mustNewPeerASNFromNumber(65001),
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "hostsession external",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
								IPv4: new("192.168.2.0/24"),
							},
							HostASN:  new(int64(0)),
							HostType: new("external"),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
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
							ID:   "192.168.2.2",
							ASN:  mustNewPeerASNFromType("external"),
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "hostsession internal",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
								IPv4: new("192.168.2.0/24"),
							},
							HostASN:  new(int64(0)),
							HostType: new("internal"),
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
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
							ID:   "192.168.2.2",
							ASN:  mustNewPeerASNFromType("internal"),
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
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
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
							Name:                  "65001@192.168.1.1",
							ASN:                   mustNewPeerASNFromNumber(65001),
							Addr:                  "192.168.1.1",
							ID:                    "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
							EBGPMultiHop:          false,
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
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
							},
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(65001),
						Addr:        "192.168.2.2",
						ID:          "192.168.2.2",
						ConnectTime: new(int64(5)),
					},
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(65001),
						Addr:        "2001:db8::2",
						ID:          "2001:db8::2",
						ConnectTime: new(int64(5)),
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
			name:      "L3 passthrough with IPv4 only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(65001),
						Addr:        "192.168.2.2",
						ID:          "192.168.2.2",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{"192.168.2.2/32"},
					ToAdvertiseIPv6: []string{},
				},
				VNIs:        []frr.L3VNIConfig{},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "L3 passthrough with IPv6 only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("2001:db8::1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv6: new("2001:db8::/64"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@2001:db8::1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::1",
							ID:   "2001:db8::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
							},
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(65001),
						Addr:        "2001:db8::2",
						ID:          "2001:db8::2",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{},
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
			name:      "L3 passthrough with IPv4 only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(65001),
						Addr:        "192.168.2.2",
						ID:          "192.168.2.2",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{"192.168.2.2/32"},
					ToAdvertiseIPv6: []string{},
				},
				VNIs:        []frr.L3VNIConfig{},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "L3 passthrough with IPv6 only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("2001:db8::1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv6: new("2001:db8::/64"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@2001:db8::1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::1",
							ID:   "2001:db8::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
							},
							EBGPMultiHop:    false,
							ExtendedNexthop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(65001),
						Addr:        "2001:db8::2",
						ID:          "2001:db8::2",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{},
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
			name:      "L3 passthrough with IPv6 underlay, IPv4 overlay and IPv4 local neighbors, ipv4unicast AF override",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("2001:db8::1"),
								ASN:     new(int64(65001)),
								AddressFamilies: []v1alpha1.NeighborAddressFamily{
									{Type: "ipv4unicast"},
								},
							},
						},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@2001:db8::1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::1",
							ID:   "2001:db8::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
							ExtendedNexthop: true,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(65001),
						Addr:        "192.168.2.2",
						ID:          "192.168.2.2",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{"192.168.2.2/32"},
					ToAdvertiseIPv6: []string{},
				},
				VNIs:        []frr.L3VNIConfig{},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "L3 passthrough with IPv6 underlay, IPv4 overlay and IPv4 local neighbors, invalid AF override",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("2001:db8::1"),
								ASN:     new(int64(65001)),
								AddressFamilies: []v1alpha1.NeighborAddressFamily{
									{Type: "invalid"},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name:      "L3 passthrough with external",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN:  new(int64(0)),
							HostType: new("external"),
							ASN:      65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
							},
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromType("external"),
						Addr:        "192.168.2.2",
						ID:          "192.168.2.2",
						ConnectTime: new(int64(5)),
					},
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromType("external"),
						Addr:        "2001:db8::2",
						ID:          "2001:db8::2",
						ConnectTime: new(int64(5)),
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
			name:      "L3 passthrough with internal",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN:  new(int64(0)),
							HostType: new("internal"),
							ASN:      65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
						},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
							},
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromType("internal"),
						Addr:        "192.168.2.2",
						ID:          "192.168.2.2",
						ConnectTime: new(int64(5)),
					},
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromType("internal"),
						Addr:        "2001:db8::2",
						ID:          "2001:db8::2",
						ConnectTime: new(int64(5)),
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
			name:      "l3vni with matching L2 gateway IPv4",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF: "red",
						VNI: 200,
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						VNI:          100,
						L2GatewayIPs: []string{"192.168.100.1/24"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:             65000,
						VNI:             200,
						VRF:             "red",
						RouterID:        "10.0.0.1",
						ToAdvertiseIPv4: []string{"192.168.100.0/24"},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "l3vni with matching L2 gateway dual-stack",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF: "red",
						VNI: 200,
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						VNI:          100,
						L2GatewayIPs: []string{"10.0.0.1/24", "2001:db8::1/64"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:             65000,
						VNI:             200,
						VRF:             "red",
						RouterID:        "10.0.0.1",
						ToAdvertiseIPv4: []string{"10.0.0.0/24"},
						ToAdvertiseIPv6: []string{"2001:db8::/64"},
						ExportRTs:       []string{},
						ImportRTs:       []string{},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "l3vni with non-matching L2 gateway VRF",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF: "red",
						VNI: 200,
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("blue"),
						VNI:          100,
						L2GatewayIPs: []string{"192.168.100.1/24"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:       65000,
						VNI:       200,
						VRF:       "red",
						RouterID:  "10.0.0.1",
						ExportRTs: []string{},
						ImportRTs: []string{},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "l3vpn with matching L2 gateway IPv4",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "red",
						RDAssignedNumber: 200,
						ImportRTs:        []v1alpha1.RouteTarget{"65000:200"},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						VNI:          100,
						L2GatewayIPs: []string{"192.168.100.1/24"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{},
				VPNs: []frr.L3VPNConfig{
					{
						ASN:                65000,
						VRF:                "red",
						RouterID:           "10.0.0.1",
						ToAdvertiseIPv4:    []string{"192.168.100.0/24"},
						ExportRTs:          []string{"65000:200"},
						ImportRTs:          []string{"65000:200"},
						RouteDistinguisher: "10.0.0.1:200",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "l3vpn with matching L2 gateway dual-stack",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "red",
						RDAssignedNumber: 200,
						ImportRTs:        []v1alpha1.RouteTarget{"65000:200"},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						VNI:          100,
						L2GatewayIPs: []string{"10.0.0.1/24", "2001:db8::1/64"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{},
				VPNs: []frr.L3VPNConfig{
					{
						ASN:                65000,
						VRF:                "red",
						RouterID:           "10.0.0.1",
						ToAdvertiseIPv4:    []string{"10.0.0.0/24"},
						ToAdvertiseIPv6:    []string{"2001:db8::/64"},
						ExportRTs:          []string{"65000:200"},
						ImportRTs:          []string{"65000:200"},
						RouteDistinguisher: "10.0.0.1:200",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "l3vpn with non-matching L2 gateway VRF",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
					},
				},
			},
			vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VPNSpec{
						VRF:              "red",
						RDAssignedNumber: 200,
						ImportRTs:        []v1alpha1.RouteTarget{"65000:200"},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("blue"),
						VNI:          100,
						L2GatewayIPs: []string{"192.168.100.1/24"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{},
				VPNs: []frr.L3VPNConfig{
					{
						ASN:                65000,
						VRF:                "red",
						RouterID:           "10.0.0.1",
						ExportRTs:          []string{"65000:200"},
						ImportRTs:          []string{"65000:200"},
						RouteDistinguisher: "10.0.0.1:200",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "Neighbor Address and Interface cannot both be set",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{{
							Address:   new("192.168.1.1"),
							Interface: new("test"),
							ASN:       new(int64(65001))}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			wantErr:       true,
		},
		{
			name:      "Neighbor Address and Interface cannot both be empty",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{{
							Address:   new(""),
							Interface: new(""),
							ASN:       new(int64(65001))}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			wantErr:       true,
		},
		{
			name:      "Interface is set",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.1.0/24"},
						},
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Interface: new("test"), ASN: new(int64(65001))}},
					},
				},
			},
			vnis:          []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.1.0/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name:      "65001@test",
							ASN:       mustNewPeerASNFromNumber(65001),
							Interface: "test",
							ID:        "test",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
							EBGPMultiHop:    false,
							ExtendedNexthop: true,
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
			name:      "underlay without evpn with dual-stack tunnel endpoints",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{
								"192.168.2.0/24",
								"2001:db8:192:168::/64",
							},
						},
					}},
			},
			vnis:          []v1alpha1.L3VNI{},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:     65000,
					Neighbors: []frr.NeighborConfig{},
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.2.0/32",
						IPv6CIDR: "2001:db8:192:168::/128",
					},
					RouterID: "10.0.0.1",
				},
				VNIs:        []frr.L3VNIConfig{},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
			},
			wantErr: false,
		},
		{
			name:      "underlay with IPv6 tunnel endpoints only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{
								"2001:db8:192:168::/64",
							},
						},
					}},
			},
			vnis:          []v1alpha1.L3VNI{},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					Neighbors: []frr.NeighborConfig{},
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv6CIDR: "2001:db8:192:168::/128",
					},
					RouterID: "10.0.0.1",
				},
				VNIs:        []frr.L3VNIConfig{},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
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
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},

						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
							{
								Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth10"},
							},
						},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(1)),
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: &frr.UnderlayISIS{
						Name:  isisProcessName,
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "eth0", IPv6: true},
							{Name: "eth10", IPv6: true},
							{Name: "lo", IPv6: true, IsPassive: true},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
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
		// When an overwrite interface is present, everything is off by default for that interface.
		{
			name:      "ISIS interface overwrites",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
							{
								Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth10"},
							},
						},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(1)),
							Interfaces: []v1alpha1.ISISInterface{
								{
									Name: "lo",
								},
								{
									Name:     "eth0",
									IPFamily: new(v1alpha1.IPFamilyDualStack),
								},
								{
									Name:     "eth1",
									IPFamily: new(v1alpha1.IPFamilyIPv4),
									Features: []v1alpha1.ISISInterfaceFeature{passiveInterface},
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
					ISIS: &frr.UnderlayISIS{
						Name:  isisProcessName,
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "eth0", IPv4: true, IPv6: true},
							{Name: "eth1", IPv4: true, IsPassive: true},
							{Name: "eth10", IPv6: true},
							{Name: "lo"},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
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
			name:      "ISIS enable passive only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(1)),
							Features: []v1alpha1.ISISFeature{
								advertisePassiveOnly,
							},
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
								{Name: "eth1", IPFamily: new(v1alpha1.IPFamilyIPv6)},
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
					ISIS: &frr.UnderlayISIS{
						Name:                 isisProcessName,
						Net:                  frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level:                1,
						AdvertisePassiveOnly: true,
						Interfaces: []frr.ISISInterface{
							{Name: "eth0", IPv4: true, IPv6: true},
							{Name: "eth1", IPv4: false, IPv6: true},
							{Name: "lo", IPv6: true, IsPassive: true},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
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
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(2)),
						},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: &frr.UnderlayISIS{
						Name:  isisProcessName,
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 2,
						Interfaces: []frr.ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
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
			name:      "ISIS invalid net",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.000",
							Level:   new(int32(1)),
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
								{Name: "eth1", IPFamily: new(v1alpha1.IPFamilyIPv6)},
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
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
						ISIS: &v1alpha1.ISISConfig{
							Level: new(int32(1)),
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
								{Name: "eth1", IPFamily: new(v1alpha1.IPFamilyIPv6)},
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
			name:      "ISIS invalid level",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(3)),
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
								{Name: "eth1", IPFamily: new(v1alpha1.IPFamilyIPv6)},
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
			name:      "SRV6 with L3VPN only",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("2001:db8:192:168:1::1"),
								ASN:     new(int64(65001)),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"2001:db8:1234:5678::/64"},
						},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(1)),
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
							},
						},
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fd00:0:32::/48",
								Format:     "usid-f3216",
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
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
							HostASN: new(int64(65001)),
						},
						VRF:              "vrf1",
						ExportRTs:        []v1alpha1.RouteTarget{"65000:100", "11110:100"},
						ImportRTs:        []v1alpha1.RouteTarget{"65001:100", "11111:100"},
						RDAssignedNumber: 100,
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: &frr.UnderlayISIS{
						Name:  isisProcessName,
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "eth0", IPv4: true, IPv6: true},
							{Name: "lo", IPv6: true, IsPassive: true},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@2001:db8:192:168:1::1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8:192:168:1::1",
							ID:   "2001:db8:192:168:1::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN},
							},
							UpdateSource:    "2001:db8:1234:5678::",
							ExtendedNexthop: true,
						},
					},
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv6CIDR: "2001:db8:1234:5678::/128",
					},
					SegmentRouting: &frr.UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1234:5678::",
						Locator: frr.SRV6Locator{
							Name:     locatorName,
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
						ASN:             65000,
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						LocalNeighbor: &frr.NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.2.2",
							ID:   "192.168.2.2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100", "11110:100"},
						ImportRTs:          []string{"65001:100", "11111:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						LocalNeighbor: &frr.NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100", "11110:100"},
						ImportRTs:          []string{"65001:100", "11111:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "SRV6 dual-stack without explicit export Route Targets",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("2001:db8:192:168:1::1"),
								ASN:     new(int64(65001)),
							},
						},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(1)),
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"2001:db8:1234:5678::/64"},
						},
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fd00:0:32::/48",
								Format:     "usid-f3216",
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
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
							HostASN: new(int64(65001)),
						},
						VRF:              "vrf1",
						RDAssignedNumber: 100,
						ImportRTs:        []v1alpha1.RouteTarget{"65001:100"},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: &frr.UnderlayISIS{
						Name:  isisProcessName,
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "eth0", IPv4: true, IPv6: true},
							{Name: "lo", IPv6: true, IsPassive: true},
						},
					},
					RouterID: "10.0.0.1",
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv6CIDR: "2001:db8:1234:5678::/128",
					},
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@2001:db8:192:168:1::1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8:192:168:1::1",
							ID:   "2001:db8:192:168:1::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN},
							},
							UpdateSource:    "2001:db8:1234:5678::",
							ExtendedNexthop: true,
						},
					},
					SegmentRouting: &frr.UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1234:5678::",
						Locator: frr.SRV6Locator{
							Name:     locatorName,
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
						ASN:             65000,
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						LocalNeighbor: &frr.NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.2.2",
							ID:   "192.168.2.2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100"},
						ImportRTs:          []string{"65001:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						LocalNeighbor: &frr.NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100"},
						ImportRTs:          []string{"65001:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "SRV6 dual-stack without any explicit Route Targets",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("2001:db8:192:168:1::1"),
								ASN:     new(int64(65001)),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"2001:db8:1234:5678::/64"},
						},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(1)),
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
							},
						},
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fd00:0:32::/48",
								Format:     "usid-f3216",
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
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
							HostASN: new(int64(65001)),
						},
						VRF:              "vrf1",
						RDAssignedNumber: 100,
					},
				},
			},
			logLevel: "debug",
			wantErr:  true,
		},
		{
			name:      "SRV6 with L3VPN and L2VNI with hostsession",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("192.168.122.1"),
								ASN:     new(int64(65001)),
							},
							{
								Address: new("2001:db8:192:168:1::1"),
								ASN:     new(int64(65001)),
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"192.168.123.0/24", "2001:db8:1234:5678::/64"},
						},
						ISIS: &v1alpha1.ISISConfig{
							BaseNet: "49.0001.0002.0003.0004.00",
							Level:   new(int32(1)),
							Interfaces: []v1alpha1.ISISInterface{
								{Name: "eth0", IPFamily: new(v1alpha1.IPFamilyDualStack)},
							},
						},
						SRV6: &v1alpha1.SRV6Config{
							Locator: v1alpha1.SRV6Locator{
								BasePrefix: "fd00:0:32::/48",
								Format:     "usid-f3216",
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
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
							HostASN: new(int64(65001)),
						},
						VRF:              "vrf1",
						ExportRTs:        []v1alpha1.RouteTarget{"65000:100", "11110:100"},
						ImportRTs:        []v1alpha1.RouteTarget{"65001:100", "11111:100"},
						RDAssignedNumber: 100,
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("vrf1"),
						VNI:          101,
						L2GatewayIPs: []string{"192.168.100.1/24", "2001:db8:1::/64"},
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: &frr.UnderlayISIS{
						Name:  isisProcessName,
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "eth0", IPv4: true, IPv6: true},
							{Name: "lo", IPv6: true, IsPassive: true},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.122.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.122.1",
							ID:   "192.168.122.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop:    false,
							ExtendedNexthop: false,
						},
						{
							Name: "65001@2001:db8:192:168:1::1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8:192:168:1::1",
							ID:   "2001:db8:192:168:1::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN},
							},
							EBGPMultiHop:    false,
							ExtendedNexthop: true,
							UpdateSource:    "2001:db8:1234:5678::",
						},
					},
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.123.0/32",
						IPv6CIDR: "2001:db8:1234:5678::/128",
					},
					SegmentRouting: &frr.UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1234:5678::",
						Locator: frr.SRV6Locator{
							Name:     locatorName,
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
						ASN: 65000,
						ToAdvertiseIPv4: []string{
							"192.168.2.2/32",
							"192.168.100.0/24",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8:1::/64",
						},
						LocalNeighbor: &frr.NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.2.2",
							ID:   "192.168.2.2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100", "11110:100"},
						ImportRTs:          []string{"65001:100", "11111:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
					{
						ASN: 65000,
						ToAdvertiseIPv4: []string{
							"192.168.100.0/24",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8::2/128",
							"2001:db8:1::/64",
						},
						LocalNeighbor: &frr.NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100", "11110:100"},
						ImportRTs:          []string{"65001:100", "11111:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
				},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
		{
			name:      "multiple L2VNIs with same VRF accumulate gateway IPs",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: new("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: new("192.168.1.1"),
								ASN:     new(int64(65001)),
							},
						},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF: "red",
						VNI: 200,
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni100"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						VNI:          100,
						L2GatewayIPs: []string{"192.168.100.1/24", "2001:db8:100::1/64"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "l2vni101"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						VNI:          101,
						L2GatewayIPs: []string{"192.168.101.1/24", "2001:db8:101::1/64"},
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			logLevel:      "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							Name: "65001@192.168.1.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.1.1",
							ID:   "192.168.1.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      200,
						VRF:      "red",
						RouterID: "10.0.0.1",
						ToAdvertiseIPv4: []string{
							"192.168.100.0/24",
							"192.168.101.0/24",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8:100::/64",
							"2001:db8:101::/64",
						},
						ExportRTs: []string{},
						ImportRTs: []string{},
					},
				},
				VPNs:        []frr.L3VPNConfig{},
				BFDProfiles: []frr.BFDProfile{},
				Loglevel:    "debug",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := APIConfigData{
				Underlays:     tt.underlays,
				L3VNIs:        tt.vnis,
				L2VNIs:        tt.l2vnis,
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
				RouterIDCIDR: new("10.0.0.0/24"),
				Neighbors:    []v1alpha1.Neighbor{{Address: new("192.168.1.1"), ASN: new(int64(65001))}},
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
				{Config: "ip prefix-list test seq 10 permit 10.0.0.0/8"},
			},
		},
		{
			name: "multiple raw configs sorted by priority",
			rawFRRConfigs: []v1alpha1.RawFRRConfig{
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  new(int32(20)),
						RawConfig: "high priority config",
					},
				},
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  new(int32(5)),
						RawConfig: "low priority config",
					},
				},
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  new(int32(10)),
						RawConfig: "mid priority config",
					},
				},
			},
			wantSnippets: []frr.RawFRRSnippet{
				{Priority: new(int32(5)), Config: "low priority config"},
				{Priority: new(int32(10)), Config: "mid priority config"},
				{Priority: new(int32(20)), Config: "high priority config"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := APIConfigData{
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
				Priority:  new(int32(10)),
				RawConfig: "ip prefix-list test seq 10 permit 10.0.0.0/8",
			},
		},
		{
			Spec: v1alpha1.RawFRRConfigSpec{
				Priority:  new(int32(5)),
				RawConfig: "route-map test permit 10",
			},
		},
	}

	wantConfig := frr.Config{
		Loglevel: "debug",
		RawConfig: []frr.RawFRRSnippet{
			{Priority: new(int32(5)), Config: "route-map test permit 10"},
			{Priority: new(int32(10)), Config: "ip prefix-list test seq 10 permit 10.0.0.0/8"},
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
								IPv4: new("192.168.2.0/24"),
							},
							HostASN: new(int64(65001)),
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
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
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
								IPv4: new("192.168.2.0/24"),
							},
							HostASN: new(int64(65001)),
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
							HostASN: new(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := APIConfigData{
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

func TestAPItoFRRGracefulRestart(t *testing.T) {
	baseUnderlay := v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{Name: "underlay", Namespace: "openperouter-system"},
		Spec: v1alpha1.UnderlaySpec{
			ASN: 64514,
			Neighbors: []v1alpha1.Neighbor{
				{ASN: new(int64(64517)), Address: new("192.168.11.2")},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"100.65.0.0/24"}},
		},
	}

	tests := []struct {
		name string
		gr   *v1alpha1.GracefulRestartConfig
		want *frr.GracefulRestart
	}{
		{
			name: "GR disabled (nil)",
			gr:   nil,
			want: nil,
		},
		{
			name: "GR enabled with defaults",
			gr:   &v1alpha1.GracefulRestartConfig{},
			want: &frr.GracefulRestart{RestartTime: 120, StalePathTime: 360},
		},
		{
			name: "GR enabled with custom timers",
			gr:   &v1alpha1.GracefulRestartConfig{RestartTimeSeconds: new(int64(90)), StalePathTimeSeconds: new(int64(180))},
			want: &frr.GracefulRestart{RestartTime: 90, StalePathTime: 180},
		},
		{
			name: "GR enabled with partial custom timers",
			gr:   &v1alpha1.GracefulRestartConfig{RestartTimeSeconds: new(int64(60))},
			want: &frr.GracefulRestart{RestartTime: 60, StalePathTime: 360},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := baseUnderlay.DeepCopy()
			u.Spec.GracefulRestart = tt.gr

			config := APIConfigData{
				Underlays: []v1alpha1.Underlay{*u},
			}
			got, err := APItoFRR(config, 0, "")
			if err != nil {
				t.Fatalf("APItoFRR() unexpected error: %v", err)
			}

			if !cmp.Equal(got.Underlay.GracefulRestart, tt.want) {
				t.Errorf("GracefulRestart diff: %s", cmp.Diff(tt.want, got.Underlay.GracefulRestart))
			}

			if tt.want != nil {
				for _, n := range got.Underlay.Neighbors {
					if n.ConnectTime == nil || *n.ConnectTime != 5 {
						t.Errorf("expected ConnectTime=5 when GR enabled, got %v", n.ConnectTime)
					}
				}
			}
		})
	}
}

func TestTunnelEndpointToFRRIPv6Only(t *testing.T) {
	const (
		ipv6TestCIDR = "2001:db8::/64"
		ipv6TestVTEP = "2001:db8::/128"
	)

	tunnelEndpoint := &v1alpha1.TunnelEndpointConfig{
		CIDRs: []string{ipv6TestCIDR},
	}

	got, err := tunnelEndpointToFRR(tunnelEndpoint, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil tunnel endpoint")
	}
	if got.IPv4CIDR != "" {
		t.Errorf("IPv4CIDR = %q, want empty", got.IPv4CIDR)
	}
	if got.IPv6CIDR != ipv6TestVTEP {
		t.Errorf("IPv6CIDR = %q, want %q", got.IPv6CIDR, ipv6TestVTEP)
	}
}

func TestTunnelEndpointToFRRDualStack(t *testing.T) {
	const (
		ipv4TestCIDR = "10.0.0.0/24"
		ipv4TestVTEP = "10.0.0.0/32"
		ipv6TestCIDR = "2001:db8::/64"
		ipv6TestVTEP = "2001:db8::/128"
	)

	tunnelEndpoint := &v1alpha1.TunnelEndpointConfig{
		CIDRs: []string{ipv4TestCIDR, ipv6TestCIDR},
	}

	got, err := tunnelEndpointToFRR(tunnelEndpoint, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil tunnel endpoint")
	}
	if got.IPv4CIDR != ipv4TestVTEP {
		t.Errorf("IPv4CIDR = %q, want %q", got.IPv4CIDR, ipv4TestVTEP)
	}
	if got.IPv6CIDR != ipv6TestVTEP {
		t.Errorf("IPv6CIDR = %q, want %q", got.IPv6CIDR, ipv6TestVTEP)
	}
}

func TestVRFsFromVNIsWithL2Gateways(t *testing.T) {
	tests := []struct {
		name   string
		l2vnis []v1alpha1.L2VNI
		want   map[string][]string
	}{
		{
			name: "single L2VNI with gateway IPs",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni100"},
					Spec: v1alpha1.L2VNISpec{
						L2GatewayIPs: []string{"192.168.0.1/24", "2001:db8::1/64"},
					},
				},
			},
			want: map[string][]string{
				"vni100": {"192.168.0.1/24", "2001:db8::1/64"},
			},
		},
		{
			name: "multiple L2VNIs with same VRF - accumulate gateway IPs",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni100"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.0.1/24", "2001:db8:1::1/64"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni101"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.1.1/24", "2001:db8:2::1/64"},
					},
				},
			},
			want: map[string][]string{
				"red": {"192.168.0.1/24", "192.168.1.1/24", "2001:db8:1::1/64", "2001:db8:2::1/64"},
			},
		},
		{
			name: "multiple L2VNIs with different VRFs",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni100"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.0.1/24", "2001:db8:1::1/64"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni200"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("blue"),
						L2GatewayIPs: []string{"192.168.1.1/24", "2001:db8:2::1/64"},
					},
				},
			},
			want: map[string][]string{
				"red":  {"192.168.0.1/24", "2001:db8:1::1/64"},
				"blue": {"192.168.1.1/24", "2001:db8:2::1/64"},
			},
		},
		{
			name: "L2VNI without gateway IPs is skipped",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni100"},
					Spec: v1alpha1.L2VNISpec{
						VRF: new("red"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni101"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.1.1/24"},
					},
				},
			},
			want: map[string][]string{
				"red": {"192.168.1.1/24"},
			},
		},
		{
			name: "accumulated gateway IPs are sorted",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni100"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.1.1/24"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni101"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.0.1/24"},
					},
				},
			},
			want: map[string][]string{
				"red": {"192.168.0.1/24", "192.168.1.1/24"},
			},
		},
		{
			name: "duplicate gateway IPs are deduplicated",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni100"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.0.1/24", "192.168.1.1/24"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni101"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.0.1/24", "192.168.2.1/24"},
					},
				},
			},
			want: map[string][]string{
				"red": {"192.168.0.1/24", "192.168.1.1/24", "192.168.2.1/24"},
			},
		},
		{
			name: "L2VNIs with and without VRF use separate keys",
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni100"},
					Spec: v1alpha1.L2VNISpec{
						VRF:          new("red"),
						L2GatewayIPs: []string{"192.168.0.1/24"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni101"},
					Spec: v1alpha1.L2VNISpec{
						L2GatewayIPs: []string{"192.168.1.1/24"},
					},
				},
			},
			want: map[string][]string{
				"red":    {"192.168.0.1/24"},
				"vni101": {"192.168.1.1/24"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vrfsFromVNIsWithL2Gateways(tt.l2vnis)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("vrfsFromVNIsWithL2Gateways() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func mustNewPeerASNFromNumber(number uint32) frr.PeerASN {
	if number == 0 {
		panic("number must be > 0")
	}
	n := int64(number)
	asn, err := frr.NewPeerASN(&n, nil)
	if err != nil {
		panic(err)
	}
	return asn
}

func mustNewPeerASNFromType(t string) frr.PeerASN {
	asn, err := frr.NewPeerASN(nil, &t)
	if err != nil {
		panic(err)
	}
	return asn
}
