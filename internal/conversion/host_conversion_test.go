// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"reflect"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAPItoHostConfig(t *testing.T) {
	tests := []struct {
		name            string
		nodeIndex       int
		targetNS        string
		underlays       []v1alpha1.Underlay
		vnis            []v1alpha1.L3VNI
		l2vnis          []v1alpha1.L2VNI
		l3Passthrough   []v1alpha1.L3Passthrough
		wantUnderlay    hostnetwork.UnderlayParams
		wantL2VNIParams []hostnetwork.L2VNIParams
		wantL3VNIParams []hostnetwork.L3VNIParams
		wantPassthrough *hostnetwork.PassthroughParams
		wantErr         bool
	}{
		{
			name:            "no underlays",
			nodeIndex:       0,
			targetNS:        "namespace",
			underlays:       []v1alpha1.Underlay{},
			vnis:            []v1alpha1.L3VNI{},
			l2vnis:          []v1alpha1.L2VNI{},
			l3Passthrough:   []v1alpha1.L3Passthrough{},
			wantUnderlay:    hostnetwork.UnderlayParams{},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "multiple underlays",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth1"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.1.0/24"}}}},
			},
			vnis:            []v1alpha1.L3VNI{},
			l2vnis:          []v1alpha1.L2VNI{},
			l3Passthrough:   []v1alpha1.L3Passthrough{},
			wantUnderlay:    hostnetwork.UnderlayParams{},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         true,
		},
		{
			name:      "ipv4 only",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("10.1.0.0/24")}}, VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
					HostVeth: &hostnetwork.Veth{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "two underlay interfaces",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Nics: []string{"eth0", "eth1"},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"10.0.0.0/24"},
						},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					Spec: v1alpha1.L3VNISpec{
						VRF: "red",
						HostSession: &v1alpha1.HostSession{
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("10.1.0.0/24"),
							},
						},
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
				},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0", "eth1"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
					HostVeth: &hostnetwork.Veth{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "ipv6 only",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: new("2001:db8::/64")}}, VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
					HostVeth: &hostnetwork.Veth{
						HostIPv6: "2001:db8::2/64",
						NSIPv6:   "2001:db8::1/64",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "dual stack",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("10.1.0.0/24"), IPv6: new("2001:db8::/64")}}, VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
					HostVeth: &hostnetwork.Veth{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
						HostIPv6: "2001:db8::2/64",
						NSIPv6:   "2001:db8::1/64",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "l2 vni input",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 200, VXLanPort: new(int32(4789))}},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       200,
						VXLanPort: new(int32(4789)),
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
			},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "disconnected l2 vni gets empty VRF in host config",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-l2vni"},
					Spec:       v1alpha1.L2VNISpec{VNI: 200, VXLanPort: new(int32(4789))},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					Name: "my-l2vni",
					VNIParams: hostnetwork.VNIParams{
						VRF:       "",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       200,
						VXLanPort: new(int32(4789)),
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
			},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "l2 vni with hostmaster and l2gatewayips",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 201, VXLanPort: new(int32(4789)), HostMaster: &v1alpha1.HostMaster{Type: "linux-bridge", LinuxBridge: &v1alpha1.LinuxBridgeConfig{Name: new("br0")}}, L2GatewayIPs: []string{"192.168.100.1/24"}}},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       201,
						VXLanPort: new(int32(4789)),
					},
					L2GatewayIPs: []string{"192.168.100.1/24"},
					HostMaster:   &hostnetwork.HostMaster{Name: new("br0"), Type: "linux-bridge"},
				},
			},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "l3 vni without hostsession",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
					HostVeth: nil,
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "underlay without evpn",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}}},
			},
			vnis:          []v1alpha1.L3VNI{},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "L3 passthrough dual stack",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}}},
			},
			vnis:   []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							ASN: 65000,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.168.2.0/24"),
								IPv6: new("2001:db8::/64"),
							},
						},
					},
				},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: &hostnetwork.PassthroughParams{
				TargetNS: "namespace",
				HostVeth: hostnetwork.Veth{
					HostIPv4: "192.168.2.2/24",
					NSIPv4:   "192.168.2.1/24",
					HostIPv6: "2001:db8::2/64",
					NSIPv6:   "2001:db8::1/64",
				},
			},
			wantErr: false,
		},
		{
			name:      "underlay without evpn with dual-stack tunnel endpoints",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Nics: []string{"eth0"},
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
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: []string{"eth0"},
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "192.168.2.0/32",
					IPv6CIDR: "2001:db8:192:168::/128",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "underlay with IPv6 tunnel endpoints only is not supported",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Nics: []string{"eth0"},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{
								"2001:db8:192:168::/64",
							},
						},
					}},
			},
			vnis:            []v1alpha1.L3VNI{},
			l2vnis:          []v1alpha1.L2VNI{},
			l3Passthrough:   []v1alpha1.L3Passthrough{},
			wantUnderlay:    hostnetwork.UnderlayParams{},
			wantL3VNIParams: nil,
			wantL2VNIParams: nil,
			wantPassthrough: nil,
			wantErr:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := APIConfigData{
				Underlays:     tt.underlays,
				L3VNIs:        tt.vnis,
				L2VNIs:        tt.l2vnis,
				L3Passthrough: tt.l3Passthrough,
			}

			gotHostConfig, err := APItoHostConfig(tt.nodeIndex, tt.targetNS, apiConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("APItoHostConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotHostConfig.Underlay, tt.wantUnderlay) {
				t.Errorf("APItoHostConfig() gotUnderlay = %+v, want %+v", gotHostConfig.Underlay, tt.wantUnderlay)
			}
			if !reflect.DeepEqual(gotHostConfig.L3VNIs, tt.wantL3VNIParams) {
				t.Errorf("APItoHostConfig() gotL3VNIParams = %+v, want %+v", gotHostConfig.L3VNIs, tt.wantL3VNIParams)
			}
			if !reflect.DeepEqual(gotHostConfig.L2VNIs, tt.wantL2VNIParams) {
				t.Errorf("APItoHostConfig() gotL2VNIParams = %+v, want %+v", gotHostConfig.L2VNIs, tt.wantL2VNIParams)
			}
			if !reflect.DeepEqual(gotHostConfig.L3Passthrough, tt.wantPassthrough) {
				t.Errorf("APItoHostConfig() gotPassthrough = %+v, want %+v", gotHostConfig.L3Passthrough, tt.wantPassthrough)
			}
		})
	}
}

// TestUnderlayInterfacesToHostNAD covers the NAD-backed underlay paths: when
// cidrs are set openperouter assigns a per-node IP (handed to IPAM via the "ips"
// capability); when omitted (e.g. dhcp IPAM) no addresses are produced and the
// NAD config is passed through unchanged.
func TestUnderlayInterfacesToHostNAD(t *testing.T) {
	const nadConfig = `{"cniVersion":"1.0.0","name":"underlay-nad","plugins":[{"type":"macvlan","master":"toswitch1","mode":"bridge","ipam":{"type":"dhcp"}}]}`
	tests := []struct {
		name      string
		cidrs     []string
		nodeIndex int
		wantAddrs []string
	}{
		{
			name:      "dhcp (no cidrs) omits addresses",
			cidrs:     nil,
			nodeIndex: 1,
			wantAddrs: nil,
		},
		{
			name:      "static cidrs assign per-node ip",
			cidrs:     []string{"192.168.11.10/24"},
			nodeIndex: 1,
			wantAddrs: []string{"192.168.11.11/24"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			underlay := v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					NetworkAttachmentDefinition: &v1alpha1.NADUnderlay{Name: "underlay-nad", CIDRs: tt.cidrs},
				},
			}
			apiConfig := APIConfigData{
				UnderlayNAD: &UnderlayNAD{Config: nadConfig},
				CNIBinDirs:  []string{"/opt/cni/bin"},
				CNICacheDir: "/var/lib/cni",
			}
			out := hostnetwork.UnderlayParams{}
			if err := underlayInterfacesToHost(tt.nodeIndex, underlay, &apiConfig, &out); err != nil {
				t.Fatalf("underlayInterfacesToHost() error = %v", err)
			}
			if out.CNI == nil {
				t.Fatalf("expected CNI params to be set")
			}
			if !reflect.DeepEqual(out.CNI.Addresses, tt.wantAddrs) {
				t.Errorf("Addresses = %v, want %v", out.CNI.Addresses, tt.wantAddrs)
			}
			if out.CNI.IfName != underlayCNIIfName {
				t.Errorf("IfName = %q, want %q", out.CNI.IfName, underlayCNIIfName)
			}
			if string(out.CNI.Config) != nadConfig {
				t.Errorf("Config was not passed through unchanged")
			}
		})
	}
}
