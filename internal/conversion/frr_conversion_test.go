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
		name      string
		nodeIndex int
		underlays []v1alpha1.Underlay
		vnis      []v1alpha1.L3VNI
		logLevel  string
		want      frr.Config
		wantErr   bool
	}{
		{
			name:      "no underlays",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{},
			vnis:      []v1alpha1.L3VNI{{}},
			wantErr:   true,
		},
		{
			name:      "no vnis",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
					},
				},
			},
			vnis:     []v1alpha1.L3VNI{},
			logLevel: "debug",
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
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
				},
				VRFs:        []frr.VRFConfig{},
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
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
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
						VRF: ptr.To("vrf1"),
						VNI: 200,
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
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
				},
				VRFs: []frr.VRFConfig{
					{
						ASN:      65000,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						EVPN: &frr.VRFEvpn{
							VNI: 200,
						},
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
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
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
						VRF: ptr.To("vrf1"),
						VNI: 200,
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
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
				},
				VRFs: []frr.VRFConfig{
					{
						ASN:      65000,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "2001:db8::2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						EVPN: &frr.VRFEvpn{
							VNI: 200,
						},
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
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
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
						VRF: ptr.To("vrf1"),
						VNI: 200,
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
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
				},
				VRFs: []frr.VRFConfig{
					{
						ASN:      65000,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						EVPN: &frr.VRFEvpn{
							VNI: 200,
						},
					},
					{
						ASN:      65000,
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "2001:db8::2",
							ASN:  65001,
						},
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						EVPN: &frr.VRFEvpn{
							VNI: 200,
						},
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
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
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
			vnis:     []v1alpha1.L3VNI{},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
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
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
				},
				VRFs: []frr.VRFConfig{},
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
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
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
			vnis:     []v1alpha1.L3VNI{},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    65000,
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
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
				},
				VRFs:        []frr.VRFConfig{},
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
						ASN:          65000,
						RouterIDCIDR: "10.0.0.0/24",
						Neighbors:    []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: "192.168.1.0/24",
						},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.L3VNISpec{
						VRF: ptr.To("vrf1"),
						VNI: 200,
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
							Name:         "65001@192.168.1.1",
							ASN:          65001,
							Addr:         "192.168.1.1",
							IPFamily:     ipfamily.IPv4,
							EBGPMultiHop: false,
						},
					},
					EVPN: &frr.UnderlayEvpn{
						VTEP: "192.168.1.0/32",
					},
				},
				VRFs: []frr.VRFConfig{
					{
						VRF:      "vrf1",
						RouterID: "10.0.0.1",
						EVPN: &frr.VRFEvpn{
							VNI: 200,
						},
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
			got, err := APItoFRR(tt.nodeIndex, tt.underlays, tt.vnis, tt.logLevel)
			if (err != nil) != tt.wantErr {
				t.Errorf("APItoFRR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !cmp.Equal(got, tt.want) {
				t.Errorf("APItoFRR() = %v, diff %s", got, cmp.Diff(got, tt.want))
			}
		})
	}
}
