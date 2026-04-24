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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
							ASN:          mustNewPeerASNFromNumber(65001),
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
			name:      "parse underlay external",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", Type: ptr.To("external")}},
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
							Name:         "external@192.168.1.1",
							ASN:          mustNewPeerASNFromType("external"),
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
			name:      "parse underlay internal",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: "192.168.1.1",
								Type:    ptr.To("internal"),
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
							Name:         "internal@192.168.1.1",
							ASN:          mustNewPeerASNFromType("internal"),
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
			name:      "parse underlay internal numeric",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: "192.168.1.1",
								ASN:     ptr.To(int64(65000)),
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
							Name:         "65000@192.168.1.1",
							ASN:          mustNewPeerASNFromNumber(65000),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
								IPv4: ptr.To("192.168.2.0/24"),
							},
							HostASN: ptr.To(int64(65001)),
						},
						VRF: "vrf1",
						VNI: ptr.To(int64(200)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  mustNewPeerASNFromNumber(65001),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
								IPv6: ptr.To("2001:db8::/64"),
							},
							HostASN: ptr.To(int64(65001)),
						},
						VRF: "vrf1",
						VNI: ptr.To(int64(200)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "2001:db8::2",
							ASN:  mustNewPeerASNFromNumber(65001),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
								IPv4: ptr.To("192.168.2.0/24"),
								IPv6: ptr.To("2001:db8::/64"),
							},
							HostASN: ptr.To(int64(65001)),
						},
						VRF: "vrf1",
						VNI: ptr.To(int64(200)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  mustNewPeerASNFromNumber(65001),
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
					},
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "2001:db8::2",
							ASN:  mustNewPeerASNFromNumber(65001),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: "192.168.1.100",
								ASN:     ptr.To(int64(65001)),
								BFD: &v1alpha1.BFDSettings{
									ReceiveInterval:  ptr.To(int32(300)),
									TransmitInterval: ptr.To(int32(300)),
									DetectMultiplier: ptr.To(int32(3)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
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
						ReceiveInterval:  ptr.To(int32(300)),
						TransmitInterval: ptr.To(int32(300)),
						DetectMultiplier: ptr.To(int32(3)),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors: []v1alpha1.Neighbor{
							{
								Address: "192.168.1.100",
								ASN:     ptr.To(int64(65001)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF: "vrf1",
						VNI: ptr.To(int64(200)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To(""),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
								IPv4: ptr.To("192.168.2.0/24"),
							},
							HostASN: ptr.To(int64(65001)),
						},
						VRF: "vni1",
						VNI: ptr.To(int64(200)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
						VRF:      "vni1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  mustNewPeerASNFromNumber(65001),
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
			name:      "hostsession external",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To(""),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
								IPv4: ptr.To("192.168.2.0/24"),
							},
							HostASN:  ptr.To(int64(0)),
							HostType: ptr.To("external"),
						},
						VRF: "vni1",
						VNI: ptr.To(int64(200)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
						VRF:      "vni1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  mustNewPeerASNFromType("external"),
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
			name:      "hostsession internal",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To(""),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
								IPv4: ptr.To("192.168.2.0/24"),
							},
							HostASN:  ptr.To(int64(0)),
							HostType: ptr.To("internal"),
						},
						VRF: "vni1",
						VNI: ptr.To(int64(200)),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				VNIs: []frr.L3VNIConfig{
					{
						ASN:      65000,
						VNI:      ptr.To(int64(200)),
						VRF:      "vni1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  mustNewPeerASNFromType("internal"),
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
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
							ASN:          mustNewPeerASNFromNumber(65001),
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
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: ptr.To(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: ptr.To("192.168.2.0/24"),
								IPv6: ptr.To("2001:db8::/64"),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:  mustNewPeerASNFromNumber(65001),
						Addr: "192.168.2.2",
					},
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:  mustNewPeerASNFromNumber(65001),
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
		{
			name:      "L3 passthrough with external",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN:  ptr.To(int64(0)),
							HostType: ptr.To("external"),
							ASN:      65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: ptr.To("192.168.2.0/24"),
								IPv6: ptr.To("2001:db8::/64"),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:  mustNewPeerASNFromType("external"),
						Addr: "192.168.2.2",
					},
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:  mustNewPeerASNFromType("external"),
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
		{
			name:      "L3 passthrough with internal",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: ptr.To("192.168.1.0/24"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN:  ptr.To(int64(0)),
							HostType: ptr.To("internal"),
							ASN:      65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: ptr.To("192.168.2.0/24"),
								IPv6: ptr.To("2001:db8::/64"),
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
							ASN:          mustNewPeerASNFromNumber(65001),
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:  mustNewPeerASNFromType("internal"),
						Addr: "192.168.2.2",
					},
					LocalNeighborV6: &frr.NeighborConfig{
						ASN:  mustNewPeerASNFromType("internal"),
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
		{
			name:      "vtepInterface sets EVPN with empty VTEP",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						EVPN: &v1alpha1.EVPNConfig{
							VTEPInterface: ptr.To("eth1"),
						},
						RouterIDCIDR: ptr.To("10.0.0.0/24"),
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
							ASN:          mustNewPeerASNFromNumber(65001),
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
			apiConfig := APIConfigData{
				Underlays:     tt.underlays,
				L3VNIs:        tt.vnis,
				L3Passthrough: tt.l3Passthrough,
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
				RouterIDCIDR: ptr.To("10.0.0.0/24"),
				Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: ptr.To(int64(65001))}},
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
						Priority:  ptr.To(int32(20)),
						RawConfig: "high priority config",
					},
				},
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  ptr.To(int32(5)),
						RawConfig: "low priority config",
					},
				},
				{
					Spec: v1alpha1.RawFRRConfigSpec{
						Priority:  ptr.To(int32(10)),
						RawConfig: "mid priority config",
					},
				},
			},
			wantSnippets: []frr.RawFRRSnippet{
				{Priority: ptr.To(int32(5)), Config: "low priority config"},
				{Priority: ptr.To(int32(10)), Config: "mid priority config"},
				{Priority: ptr.To(int32(20)), Config: "high priority config"},
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
				Priority:  ptr.To(int32(10)),
				RawConfig: "ip prefix-list test seq 10 permit 10.0.0.0/8",
			},
		},
		{
			Spec: v1alpha1.RawFRRConfigSpec{
				Priority:  ptr.To(int32(5)),
				RawConfig: "route-map test permit 10",
			},
		},
	}

	wantConfig := frr.Config{
		Loglevel: "debug",
		RawConfig: []frr.RawFRRSnippet{
			{Priority: ptr.To(int32(5)), Config: "route-map test permit 10"},
			{Priority: ptr.To(int32(10)), Config: "ip prefix-list test seq 10 permit 10.0.0.0/8"},
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
								IPv4: ptr.To("192.168.2.0/24"),
							},
							HostASN: ptr.To(int64(65001)),
						},
						VRF: "vrf1",
						VNI: ptr.To(int64(200)),
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
							HostASN: ptr.To(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: ptr.To("192.168.2.0/24"),
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
								IPv4: ptr.To("192.168.2.0/24"),
							},
							HostASN: ptr.To(int64(65001)),
						},
						VRF: "vrf1",
						VNI: ptr.To(int64(200)),
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							HostASN: ptr.To(int64(65001)),
							ASN:     65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: ptr.To("192.168.2.0/24"),
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

func mustNewPeerASNFromNumber(number uint32) frr.PeerASN {
	if number == 0 {
		panic("number must be > 0")
	}
	asn, err := frr.NewPeerASN(int64(number), "")
	if err != nil {
		panic(err)
	}
	return asn
}

func mustNewPeerASNFromType(t string) frr.PeerASN {
	asn, err := frr.NewPeerASN(0, t)
	if err != nil {
		panic(err)
	}
	return asn
}
