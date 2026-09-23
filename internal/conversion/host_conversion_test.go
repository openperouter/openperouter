// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
		l3vpns          []v1alpha1.L3VPN
		l3Passthrough   []v1alpha1.L3Passthrough
		wantUnderlay    hostnetwork.UnderlayParams
		wantL2VNIParams []hostnetwork.L2VNIParams
		wantL3VNIParams []hostnetwork.L3VNIParams
		wantL3VPNParams []hostnetwork.L3VPNParams
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
				{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}},
					},
				},
				{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth1"},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.1.0/24"}},
					},
				},
			},
			wantErr: true,
		},
		{
			name:      "ipv4 only",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDRs: []string{"10.1.0.0/24"}}, VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            100,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
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
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth1"},
							},
						},
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
							LocalCIDRs: []string{"10.1.0.0/24"},
						},
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
				},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0", "eth1"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            100,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "SRV6 + EVPN L2",
			nodeIndex: 0,
			targetNS:  "namespace",
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
							CIDRs: []string{"10.0.0.0/24", "2001:db8::/128"},
						},
						SRV6: &v1alpha1.SRV6Config{},
						ISIS: &v1alpha1.ISISConfig{},
					},
				},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vpn1",
					},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "red",
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"10.1.0.0/24"},
						},
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
						ExportRTs:        []v1alpha1.RouteTarget{"65001:100"},
						RDAssignedNumber: 100,
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{
					VNI:           200,
					VXLanPort:     new(int32(4789)),
					RoutingDomain: l3vpnRoutingDomain("vpn1"),
				}},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
					IPv6CIDR: "2001:db8::/128",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{
				{
					Name:             "vpn1",
					VRF:              "red",
					RDAssignedNumber: 100,
					TargetNS:         "namespace",
					// SRv6 normal encaps (64) > VXLan (50), so the L2VNI in this VRF inherits SRv6 overhead.
					TunnelOverhead: hostnetwork.SRv6Overhead,
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       200,
						VXLanPort: new(int32(4789)),
						// SRv6 normal encaps (64) > VXLan (50), so the L2VNI in this VRF inherits SRv6 overhead.
						TunnelOverhead: hostnetwork.SRv6Overhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
			},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "ipv6 only",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDRs: []string{"2001:db8::/64"}}, VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            100,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv6: "2001:db8::2/64",
						NSIPv6:   "2001:db8::1/64",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "dual stack",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDRs: []string{"10.1.0.0/24", "2001:db8::/64"}}, VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            100,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
						HostIPv6: "2001:db8::2/64",
						NSIPv6:   "2001:db8::1/64",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "l2 vni input",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 200, VXLanPort: new(int32(4789))}},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            200,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
			},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "disconnected l2 vni gets empty VRF in host config",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
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
				UnderlayInterfaces: netdevInterfaces("eth0"),
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
						VRF:            "",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            200,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
			},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "l2 vni with hostmaster and gatewayips",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "gw-l3"}, Spec: v1alpha1.L3VNISpec{VRF: "red", VNI: 300}},
			},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VNI,
						L3VNI: &v1alpha1.L3VNIReference{Name: "gw-l3"},
					},
					VNI: 201, VXLanPort: new(int32(4789)),
					HostMaster: &v1alpha1.HostMaster{Type: "LinuxBridge", LinuxBridge: &v1alpha1.LinuxBridgeConfig{Name: new("br0")}},
					GatewayIPs: []string{"192.168.100.1/24"},
				}},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            300,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					Name: "gw-l3",
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            201,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					L2GatewayIPs: []string{"192.168.100.1/24"},
					HostMaster:   &hostnetwork.HostMaster{Name: new("br0"), Type: "LinuxBridge", AutoCreate: new(false)},
				},
			},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "l3 vni without hostsession",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}, TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"10.0.0.0/24"}}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", VNI: 100, VXLanPort: new(int32(4789))}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            100,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					LinkIPs: nil,
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "underlay without evpn or srv6",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}}},
			},
			vnis:          []v1alpha1.L3VNI{},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantErr:         false,
		},
		{
			name:      "L3 passthrough dual stack",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}}}},
			},
			vnis:   []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{
				{
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							ASN:        65000,
							LocalCIDRs: []string{"192.168.2.0/24", "2001:db8::/64"},
						},
					},
				},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: &hostnetwork.PassthroughParams{
				TargetNS: "namespace",
				LinkIPs: hostnetwork.LinkIPs{
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
						Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}},
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
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "192.168.2.0/32",
					IPv6CIDR: "2001:db8:192:168::/128",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
		{
			name:      "underlay with IPv6 tunnel endpoints only",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}}},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{
								"2001:db8:192:168::/64",
							},
						},
					}},
			},
			vnis:   []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{},
			wantUnderlay: hostnetwork.UnderlayParams{
				TargetNS:           "namespace",
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv6CIDR: "2001:db8:192:168::/128",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantL3VPNParams: []hostnetwork.L3VPNParams{},
			wantErr:         false,
		},
		{
			name:      "l2vnis without EVPN",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 200, VXLanPort: new(int32(4789))}},
			},
			wantErr: true,
		},
		{
			name:      "l3vnis without EVPN",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{
					VRF: "red",
					HostSession: &v1alpha1.HostSession{
						LocalCIDRs: []string{"10.1.0.0/24"},
					},
					VNI:       100,
					VXLanPort: new(int32(4789)),
				},
				},
			},
			wantErr: true,
		},
		{
			name:      "MTU configuration with encap reduced",
			nodeIndex: 0,
			targetNS:  "namespace",
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
							CIDRs: []string{"10.0.0.0/24", "2001:db8::/128"},
						},
						SRV6: &v1alpha1.SRV6Config{
							EncapBehavior: new(v1alpha1.HEncapsRed),
						},
						ISIS: &v1alpha1.ISISConfig{},
					},
				},
			},
			vnis: []v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vni1",
					},
					Spec: v1alpha1.L3VNISpec{
						VRF: "red",
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"10.1.0.0/24"},
						},
						VNI:       100,
						VXLanPort: new(int32(4789)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vni2",
					},
					Spec: v1alpha1.L3VNISpec{
						VRF: "yellow",
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"10.1.0.0/24"},
						},
						VNI:       101,
						VXLanPort: new(int32(4789)),
					},
				},
			},
			l3vpns: []v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vpn1",
					},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "blue",
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"10.1.0.0/24"},
						},
						ImportRTs:        []v1alpha1.RouteTarget{"65000:150"},
						ExportRTs:        []v1alpha1.RouteTarget{"65001:150"},
						RDAssignedNumber: 150,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vpn2",
					},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "green",
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"10.1.0.0/24"},
						},
						ImportRTs:        []v1alpha1.RouteTarget{"65000:151"},
						ExportRTs:        []v1alpha1.RouteTarget{"65001:151"},
						RDAssignedNumber: 151,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vpn3",
					},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "purple",
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"10.1.0.0/24"},
						},
						ImportRTs:        []v1alpha1.RouteTarget{"65000:152"},
						ExportRTs:        []v1alpha1.RouteTarget{"65001:152"},
						RDAssignedNumber: 152,
					},
				},
			},
			l2vnis: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-for-vni1",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   200,
						VXLanPort:             new(int32(4789)),
						RoutingDomain:         l3vniRoutingDomain("vni1"),
						UnderlayAddressFamily: new("IPv4"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-for-vni2-a",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   201,
						VXLanPort:             new(int32(4789)),
						RoutingDomain:         l3vniRoutingDomain("vni2"),
						UnderlayAddressFamily: new("IPv4"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-for-vni2-b",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   202,
						VXLanPort:             new(int32(4789)),
						RoutingDomain:         l3vniRoutingDomain("vni2"),
						UnderlayAddressFamily: new("IPv6"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-for-vpn1",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   250,
						VXLanPort:             new(int32(4789)),
						RoutingDomain:         l3vpnRoutingDomain("vpn1"),
						UnderlayAddressFamily: new("IPv4"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-for-vpn2-a",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   251,
						VXLanPort:             new(int32(4789)),
						RoutingDomain:         l3vpnRoutingDomain("vpn2"),
						UnderlayAddressFamily: new("IPv4"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-for-vpn2-b",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   252,
						VXLanPort:             new(int32(4789)),
						RoutingDomain:         l3vpnRoutingDomain("vpn2"),
						UnderlayAddressFamily: new("IPv6"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-ipv4",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   253,
						VXLanPort:             new(int32(4789)),
						UnderlayAddressFamily: new("IPv4"),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "l2-ipv6",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:                   254,
						VXLanPort:             new(int32(4789)),
						UnderlayAddressFamily: new("IPv6"),
					},
				},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterfaces: netdevInterfaces("eth0"),
				TargetNS:           "namespace",
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "10.0.0.0/32",
					IPv6CIDR: "2001:db8::/128",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					Name: "vni1",
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            100,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
				{
					Name: "vni2",
					VNIParams: hostnetwork.VNIParams{
						VRF:       "yellow",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       101,
						VXLanPort: new(int32(4789)),
						// Mixed IPv4 VXLAN and IPv6 VXLAN, so the IPv6VXLAN overhead wins.
						TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
					},
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
			},
			wantL3VPNParams: []hostnetwork.L3VPNParams{
				{
					Name:             "vpn1",
					VRF:              "blue",
					RDAssignedNumber: 150,
					TargetNS:         "namespace",
					// VXLan overhead (50) > SRv6 encaps reduced (40), so the L2VNI in this VRF bumps both to VXLan.
					TunnelOverhead: hostnetwork.VXLanOverhead,
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
				{
					Name:             "vpn2",
					VRF:              "green",
					RDAssignedNumber: 151,
					TargetNS:         "namespace",
					// IPv6 VXLan overhead (70) > SRv6 encaps reduced (40), so the L2VNI in this VRF bumps both to IPv6 VXLan.
					TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
				{
					Name:             "vpn3",
					VRF:              "purple",
					RDAssignedNumber: 152,
					TargetNS:         "namespace",
					// No L2VNI in this VRF, so the native SRv6 encaps reduced overhead applies.
					TunnelOverhead: hostnetwork.SRv6OverheadEncapReduced,
					LinkIPs: &hostnetwork.LinkIPs{
						HostIPv4: "10.1.0.2/24",
						NSIPv4:   "10.1.0.1/24",
					},
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					Name: "l2-for-vni1",
					VNIParams: hostnetwork.VNIParams{
						VRF:            "red",
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            200,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
				{
					Name: "l2-for-vni2-a",
					VNIParams: hostnetwork.VNIParams{
						VRF:       "yellow",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       201,
						VXLanPort: new(int32(4789)),
						// In a mixed environment with IPv4 and IPv6 VXLAN tunnels, we must pick IPv6 VXLAN.
						TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
				{
					Name: "l2-for-vni2-b",
					VNIParams: hostnetwork.VNIParams{
						VRF:       "yellow",
						TargetNS:  "namespace",
						VTEPIP:    "2001:db8::/128",
						VNI:       202,
						VXLanPort: new(int32(4789)),
						// In a mixed environment with IPv4 and IPv6 VXLAN tunnels, we must pick IPv6 VXLAN.
						TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
				{
					Name: "l2-for-vpn1",
					VNIParams: hostnetwork.VNIParams{
						VRF:       "blue",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       250,
						VXLanPort: new(int32(4789)),
						// IPv4 VXLAN overhead is larger than SRv6 encaps reduced.
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
				{
					Name: "l2-for-vpn2-a",
					VNIParams: hostnetwork.VNIParams{
						VRF:       "green",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       251,
						VXLanPort: new(int32(4789)),
						// In a mixed environment with SRv6, IPv4 and IPv6 VXLAN tunnels, we must pick IPv6 VXLAN.
						TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
				{
					Name: "l2-for-vpn2-b",
					VNIParams: hostnetwork.VNIParams{
						VRF:       "green",
						TargetNS:  "namespace",
						VTEPIP:    "2001:db8::/128",
						VNI:       252,
						VXLanPort: new(int32(4789)),
						// In a mixed environment with SRv6, IPv4 and IPv6 VXLAN tunnels, we must pick IPv6 VXLAN.
						TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
				{
					Name: "l2-ipv4",
					VNIParams: hostnetwork.VNIParams{
						TargetNS:       "namespace",
						VTEPIP:         "10.0.0.0/32",
						VNI:            253,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
				{
					Name: "l2-ipv6",
					VNIParams: hostnetwork.VNIParams{
						TargetNS:       "namespace",
						VTEPIP:         "2001:db8::/128",
						VNI:            254,
						VXLanPort:      new(int32(4789)),
						TunnelOverhead: hostnetwork.IPv6VXLanOverhead,
					},
					L2GatewayIPs: nil,
					HostMaster:   nil,
				},
			},
			wantPassthrough: nil,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := APIConfigData{
				Underlays:     tt.underlays,
				L3VNIs:        tt.vnis,
				L2VNIs:        tt.l2vnis,
				L3VPNs:        tt.l3vpns,
				L3Passthrough: tt.l3Passthrough,
			}

			gotHostConfig, err := APItoHostConfig(tt.nodeIndex, tt.targetNS, apiConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("APItoHostConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotHostConfig.Underlay, tt.wantUnderlay) {
				t.Errorf("APItoHostConfig() gotUnderlay = %s, want %s", mustMarshal(gotHostConfig.Underlay), mustMarshal(tt.wantUnderlay))
			}
			if !reflect.DeepEqual(gotHostConfig.L3VNIs, tt.wantL3VNIParams) {
				t.Errorf("APItoHostConfig() gotL3VNIParams = %+v, want %+v", gotHostConfig.L3VNIs, tt.wantL3VNIParams)
			}
			if !reflect.DeepEqual(gotHostConfig.L2VNIs, tt.wantL2VNIParams) {
				t.Errorf("APItoHostConfig() gotL2VNIParams = %+v, want %+v", gotHostConfig.L2VNIs, tt.wantL2VNIParams)
			}
			if !reflect.DeepEqual(gotHostConfig.L3VPNs, tt.wantL3VPNParams) {
				t.Errorf("APItoHostConfig() gotL3VPNParams = %+v, want %+v", gotHostConfig.L3VPNs, tt.wantL3VPNParams)
			}
			if !reflect.DeepEqual(gotHostConfig.L3Passthrough, tt.wantPassthrough) {
				t.Errorf("APItoHostConfig() gotPassthrough = %+v, want %+v", gotHostConfig.L3Passthrough, tt.wantPassthrough)
			}
		})
	}
}

func TestResolveVTEPIP(t *testing.T) {
	tests := []struct {
		name           string
		addressFamily  *string
		tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams
		want           string
		wantErr        string
	}{
		{
			name:          "ipv4 only, no field set, returns ipv4",
			addressFamily: nil,
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{
				IPv4CIDR: "10.0.0.1/32",
			},
			want: "10.0.0.1/32",
		},
		{
			name:          "ipv6 only, no field set, returns ipv6",
			addressFamily: nil,
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{
				IPv6CIDR: "2001:db8::1/128",
			},
			want: "2001:db8::1/128",
		},
		{
			name:          "dual-stack, no field set, defaults to ipv4",
			addressFamily: nil,
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{
				IPv4CIDR: "10.0.0.1/32",
				IPv6CIDR: "2001:db8::1/128",
			},
			want: "10.0.0.1/32",
		},
		{
			name:          "dual-stack, field=ipv6, returns ipv6",
			addressFamily: new("IPv6"),
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{
				IPv4CIDR: "10.0.0.1/32",
				IPv6CIDR: "2001:db8::1/128",
			},
			want: "2001:db8::1/128",
		},
		{
			name:          "dual-stack, field=ipv4, returns ipv4",
			addressFamily: new("IPv4"),
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{
				IPv4CIDR: "10.0.0.1/32",
				IPv6CIDR: "2001:db8::1/128",
			},
			want: "10.0.0.1/32",
		},
		{
			name:          "ipv4 only, field=ipv6, error",
			addressFamily: new("IPv6"),
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{
				IPv4CIDR: "10.0.0.1/32",
			},
			wantErr: "IPv6 address family requested but no IPv6 VTEP IP available",
		},
		{
			name:          "ipv6 only, field=ipv4, error",
			addressFamily: new("IPv4"),
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{
				IPv6CIDR: "2001:db8::1/128",
			},
			wantErr: "IPv4 address family requested but no IPv4 VTEP IP available",
		},
		{
			name:           "empty tunnel endpoint, no field set, error",
			addressFamily:  nil,
			tunnelEndpoint: hostnetwork.UnderlayTunnelEndpointParams{},
			wantErr:        "no VTEP IP available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveVTEPIP(tt.addressFamily, tt.tunnelEndpoint)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveVTEPIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTunnelEndpointToHost(t *testing.T) {
	tests := []struct {
		name     string
		cidrs    []string
		wantIPv4 string
		wantIPv6 string
		wantErr  string
	}{
		{
			name:     "ipv6 only",
			cidrs:    []string{"2001:db8::/64"},
			wantIPv6: "2001:db8::/128",
		},
		{
			name:     "ipv4 only",
			cidrs:    []string{"10.0.0.0/24"},
			wantIPv4: "10.0.0.0/32",
		},
		{
			name:     "dual-stack",
			cidrs:    []string{"10.0.0.0/24", "2001:db8::/64"},
			wantIPv4: "10.0.0.0/32",
			wantIPv6: "2001:db8::/128",
		},
		{
			name:    "empty cidrs",
			cidrs:   []string{},
			wantErr: "no VTEP IP available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tunnelEndpoint := &v1alpha1.TunnelEndpointConfig{CIDRs: tt.cidrs}
			got, err := tunnelEndpointToHost(tunnelEndpoint, 0)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.IPv4CIDR != tt.wantIPv4 {
				t.Errorf("IPv4CIDR = %q, want %q", got.IPv4CIDR, tt.wantIPv4)
			}
			if got.IPv6CIDR != tt.wantIPv6 {
				t.Errorf("IPv6CIDR = %q, want %q", got.IPv6CIDR, tt.wantIPv6)
			}
		})
	}
}

func TestAPItoHostConfigAddressFamily(t *testing.T) {
	tests := []struct {
		name       string
		cidrs      []string
		l3VNI      *v1alpha1.L3VNI
		l2VNI      *v1alpha1.L2VNI
		wantVTEPIP string
		wantErr    string
	}{
		{
			name:  "dual-stack underlay with ipv6 L3VNI",
			cidrs: []string{"10.0.0.0/24", "2001:db8::/64"},
			l3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{Name: "vni-ipv6"},
				Spec: v1alpha1.L3VNISpec{
					VRF: "red", VNI: 100, VXLanPort: new(int32(4789)),
					UnderlayAddressFamily: new("IPv6"),
				},
			},
			wantVTEPIP: "2001:db8::/128",
		},
		{
			name:  "ipv6-only underlay with L2VNI defaults to ipv6",
			cidrs: []string{"2001:db8::/64"},
			l2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{Name: "l2-ipv6"},
				Spec:       v1alpha1.L2VNISpec{VNI: 200, VXLanPort: new(int32(4789))},
			},
			wantVTEPIP: "2001:db8::/128",
		},
		{
			name:  "ipv4-only underlay with ipv6 L3VNI errors",
			cidrs: []string{"10.0.0.0/24"},
			l3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{Name: "vni-mismatch"},
				Spec: v1alpha1.L3VNISpec{
					VRF: "red", VNI: 100,
					UnderlayAddressFamily: new("IPv6"),
				},
			},
			wantErr: "L3VNI \"vni-mismatch\", err: IPv6 address family requested but no IPv6 VTEP IP available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l3VNIs []v1alpha1.L3VNI
			var l2VNIs []v1alpha1.L2VNI
			if tt.l3VNI != nil {
				l3VNIs = []v1alpha1.L3VNI{*tt.l3VNI}
			}
			if tt.l2VNI != nil {
				l2VNIs = []v1alpha1.L2VNI{*tt.l2VNI}
			}

			apiConfig := APIConfigData{
				Underlays: []v1alpha1.Underlay{{
					Spec: v1alpha1.UnderlaySpec{
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: tt.cidrs},
					},
				}},
				L3VNIs: l3VNIs,
				L2VNIs: l2VNIs,
			}

			got, err := APItoHostConfig(0, "namespace", apiConfig)
			checkAddressFamilyResult(t, got, err, tt.wantErr, tt.wantVTEPIP, tt.l3VNI != nil, tt.l2VNI != nil)
		})
	}
}

func checkAddressFamilyResult(t *testing.T, got HostConfigData, err error, wantErr, wantVTEPIP string, hasL3VNIs, hasL2VNIs bool) {
	t.Helper()
	if wantErr != "" {
		if err == nil {
			t.Fatalf("expected error containing %q, got nil", wantErr)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Errorf("expected error containing %q, got %q", wantErr, err.Error())
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hasL3VNIs {
		if len(got.L3VNIs) != 1 {
			t.Fatalf("expected 1 L3VNI, got %d", len(got.L3VNIs))
		}
		if got.L3VNIs[0].VTEPIP != wantVTEPIP {
			t.Errorf("L3VNI VTEPIP = %q, want %q", got.L3VNIs[0].VTEPIP, wantVTEPIP)
		}
	}
	if hasL2VNIs {
		if len(got.L2VNIs) != 1 {
			t.Fatalf("expected 1 L2VNI, got %d", len(got.L2VNIs))
		}
		if got.L2VNIs[0].VTEPIP != wantVTEPIP {
			t.Errorf("L2VNI VTEPIP = %q, want %q", got.L2VNIs[0].VTEPIP, wantVTEPIP)
		}
	}
}

func TestAPItoHostConfigCNIInterfaces(t *testing.T) {
	rawConfig := `{"cniVersion":"1.0.0","name":"macvlan-underlay","type":"macvlan","master":"eth1"}`
	underlayWithInterfaces := func(interfaces ...v1alpha1.UnderlayInterface) []v1alpha1.Underlay {
		return []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{Interfaces: interfaces}}}
	}

	tests := []struct {
		name         string
		underlays    []v1alpha1.Underlay
		wantUnderlay hostnetwork.UnderlayParams
		wantErr      string
	}{
		{
			name: "cni interface with runtime config",
			underlays: underlayWithInterfaces(v1alpha1.UnderlayInterface{
				Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
				CNIDevice: &v1alpha1.CNIDevice{
					Type:          v1alpha1.CNIConfigTypeRawConfig,
					RawConfig:     &apiextensionsv1.JSON{Raw: []byte(rawConfig)},
					InterfaceName: new("underlay0"),
					RuntimeConfig: &apiextensionsv1.JSON{Raw: []byte(`{"mac":"02:42:c0:a8:01:0a","ips":["192.168.1.10/24"]}`)},
				},
			}),
			wantUnderlay: hostnetwork.UnderlayParams{
				TargetNS: "namespace",
				UnderlayInterfaces: []hostnetwork.UnderlayInterface{
					{
						InterfaceName: "underlay0",
						Kind:          hostnetwork.UnderlayInterfaceCNIDev,
						CNI: &hostnetwork.CNIDeviceParams{
							Config: []byte(rawConfig),
							CapabilityArgs: map[string]any{
								"mac": "02:42:c0:a8:01:0a",
								"ips": []any{"192.168.1.10/24"},
							},
						},
					},
				},
			},
		},
		{
			name: "cni interface name defaults to net1",
			underlays: underlayWithInterfaces(v1alpha1.UnderlayInterface{
				Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
				CNIDevice: &v1alpha1.CNIDevice{
					Type:      v1alpha1.CNIConfigTypeRawConfig,
					RawConfig: &apiextensionsv1.JSON{Raw: []byte(rawConfig)},
				},
			}),
			wantUnderlay: hostnetwork.UnderlayParams{
				TargetNS: "namespace",
				UnderlayInterfaces: []hostnetwork.UnderlayInterface{
					{
						InterfaceName: "net1",
						Kind:          hostnetwork.UnderlayInterfaceCNIDev,
						CNI:           &hostnetwork.CNIDeviceParams{Config: []byte(rawConfig)},
					},
				},
			},
		},
		{
			name: "cni interface without cniDevice",
			underlays: underlayWithInterfaces(v1alpha1.UnderlayInterface{
				Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
			}),
			wantErr: "cniDevice configuration is missing",
		},
		{
			name: "cni interface without rawConfig",
			underlays: underlayWithInterfaces(v1alpha1.UnderlayInterface{
				Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
				CNIDevice: &v1alpha1.CNIDevice{
					Type:          v1alpha1.CNIConfigTypeRawConfig,
					InterfaceName: new("underlay0"),
				},
			}),
			wantErr: "rawConfig is missing",
		},
		{
			name: "cni interface with invalid runtime config",
			underlays: underlayWithInterfaces(v1alpha1.UnderlayInterface{
				Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
				CNIDevice: &v1alpha1.CNIDevice{
					Type:          v1alpha1.CNIConfigTypeRawConfig,
					RawConfig:     &apiextensionsv1.JSON{Raw: []byte(rawConfig)},
					RuntimeConfig: &apiextensionsv1.JSON{Raw: []byte(`"notanobject"`)},
				},
			}),
			wantErr: "invalid runtimeConfig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := APIConfigData{
				Underlays: tt.underlays,
			}

			got, err := APItoHostConfig(0, "namespace", apiConfig)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("APItoHostConfig() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got.Underlay, tt.wantUnderlay) {
				t.Errorf("APItoHostConfig() gotUnderlay = %+v, want %+v", got.Underlay, tt.wantUnderlay)
			}
		})
	}
}

func TestOverheadForTunnels(t *testing.T) {
	ipv4VTEP := "10.0.0.1/32"
	ipv6VTEP := "2001:db8::1/128"

	tests := []struct {
		name      string
		apiConfig APIConfigData
		vtepIPs   tunnelVtepIPs
		want      tunnelOverheads
		wantErr   string
	}{
		{
			name: "L3VNI only, IPv4",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{"vni1": ipv4VTEP},
				forL2VNIs: map[string]string{},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{"vni1": hostnetwork.VXLanOverhead},
				forL3VPNs: map[string]int{},
				forL2VNIs: map[string]int{},
			},
		},
		{
			name: "L3VNI only, IPv6",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{"vni1": ipv6VTEP},
				forL2VNIs: map[string]string{},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{"vni1": hostnetwork.IPv6VXLanOverhead},
				forL3VPNs: map[string]int{},
				forL2VNIs: map[string]int{},
			},
		},
		{
			name: "L3VNI routing domain, all IPv4",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("vni1")}},
					{ObjectMeta: metav1.ObjectMeta{Name: "l2b"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("vni1")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{"vni1": ipv4VTEP},
				forL2VNIs: map[string]string{"l2a": ipv4VTEP, "l2b": ipv4VTEP},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{"vni1": hostnetwork.VXLanOverhead},
				forL3VPNs: map[string]int{},
				forL2VNIs: map[string]int{"l2a": hostnetwork.VXLanOverhead, "l2b": hostnetwork.VXLanOverhead},
			},
		},
		{
			name: "L3VNI routing domain, mixed IPv4 and IPv6 L2VNIs",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("vni1")}},
					{ObjectMeta: metav1.ObjectMeta{Name: "l2b"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("vni1")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{"vni1": ipv4VTEP},
				forL2VNIs: map[string]string{"l2a": ipv4VTEP, "l2b": ipv6VTEP},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{"vni1": hostnetwork.IPv6VXLanOverhead},
				forL3VPNs: map[string]int{},
				forL2VNIs: map[string]int{"l2a": hostnetwork.IPv6VXLanOverhead, "l2b": hostnetwork.IPv6VXLanOverhead},
			},
		},
		{
			name: "L3VNI routing domain, IPv6 L3VNI with IPv4 L2VNIs",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("vni1")}},
					{ObjectMeta: metav1.ObjectMeta{Name: "l2b"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("vni1")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{"vni1": ipv6VTEP},
				forL2VNIs: map[string]string{"l2a": ipv4VTEP, "l2b": ipv4VTEP},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{"vni1": hostnetwork.IPv6VXLanOverhead},
				forL3VPNs: map[string]int{},
				forL2VNIs: map[string]int{"l2a": hostnetwork.IPv6VXLanOverhead, "l2b": hostnetwork.IPv6VXLanOverhead},
			},
		},
		{
			name: "L3VPN SRv6 full overhead with IPv4 L2VNI, SRv6 wins",
			apiConfig: APIConfigData{
				Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{SRV6: &v1alpha1.SRV6Config{}}}},
				L3VPNs: []v1alpha1.L3VPN{
					{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}},
				},
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vpnRoutingDomain("vpn1")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{"l2a": ipv4VTEP},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{},
				forL3VPNs: map[string]int{"vpn1": hostnetwork.SRv6Overhead},
				forL2VNIs: map[string]int{"l2a": hostnetwork.SRv6Overhead},
			},
		},
		{
			name: "L3VPN SRv6 full overhead with IPv6 L2VNI, IPv6 VXLAN wins",
			apiConfig: APIConfigData{
				Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{SRV6: &v1alpha1.SRV6Config{}}}},
				L3VPNs: []v1alpha1.L3VPN{
					{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}},
				},
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vpnRoutingDomain("vpn1")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{"l2a": ipv6VTEP},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{},
				forL3VPNs: map[string]int{"vpn1": hostnetwork.IPv6VXLanOverhead},
				forL2VNIs: map[string]int{"l2a": hostnetwork.IPv6VXLanOverhead},
			},
		},
		{
			name: "L3VPN SRv6 reduced overhead with IPv4 L2VNI, VXLAN wins",
			apiConfig: APIConfigData{
				Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{
					SRV6: &v1alpha1.SRV6Config{EncapBehavior: new(v1alpha1.HEncapsRed)},
				}}},
				L3VPNs: []v1alpha1.L3VPN{
					{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}},
				},
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vpnRoutingDomain("vpn1")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{"l2a": ipv4VTEP},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{},
				forL3VPNs: map[string]int{"vpn1": hostnetwork.VXLanOverhead},
				forL2VNIs: map[string]int{"l2a": hostnetwork.VXLanOverhead},
			},
		},
		{
			name: "L3VPN with no L2VNIs keeps pure SRv6 overhead",
			apiConfig: APIConfigData{
				Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{SRV6: &v1alpha1.SRV6Config{}}}},
				L3VPNs: []v1alpha1.L3VPN{
					{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{},
				forL3VPNs: map[string]int{"vpn1": hostnetwork.SRv6Overhead},
				forL2VNIs: map[string]int{},
			},
		},
		{
			name: "L3VPN with no L2VNIs keeps pure SRv6 reduced overhead",
			apiConfig: APIConfigData{
				Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{
					SRV6: &v1alpha1.SRV6Config{EncapBehavior: new(v1alpha1.HEncapsRed)},
				}}},
				L3VPNs: []v1alpha1.L3VPN{
					{ObjectMeta: metav1.ObjectMeta{Name: "vpn1"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{},
				forL3VPNs: map[string]int{"vpn1": hostnetwork.SRv6OverheadEncapReduced},
				forL2VNIs: map[string]int{},
			},
		},
		{
			name: "standalone L2VNIs without routing domain get individual overhead",
			apiConfig: APIConfigData{
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2-v4"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "l2-v6"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{"l2-v4": ipv4VTEP, "l2-v6": ipv6VTEP},
			},
			want: tunnelOverheads{
				forL3VNIs: map[string]int{},
				forL3VPNs: map[string]int{},
				forL2VNIs: map[string]int{"l2-v4": hostnetwork.VXLanOverhead, "l2-v6": hostnetwork.IPv6VXLanOverhead},
			},
		},
		{
			name: "L3VNI missing from VTEP IPs map",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{},
			},
			wantErr: "L3VNI vni1: not found in map of tunnel VTEP IPs: map[]",
		},
		{
			name: "L2VNI in L3VNI routing domain missing from VTEP IPs map",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("vni1")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{"vni1": ipv4VTEP},
				forL2VNIs: map[string]string{},
			},
			wantErr: "L2VNI l2a: not found in map of tunnel VTEP IPs: map[]",
		},
		{
			name: "L2VNI references nonexistent L3VPN",
			apiConfig: APIConfigData{
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vpnRoutingDomain("missing")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{"l2a": ipv4VTEP},
			},
			wantErr: "L2VNI l2a: could not find corresponding L3VPN \"missing\" in tunnel overheads map[]",
		},
		{
			name: "L2VNI references nonexistent L3VNI",
			apiConfig: APIConfigData{
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}, Spec: v1alpha1.L2VNISpec{RoutingDomain: l3vniRoutingDomain("missing")}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{"l2a": ipv4VTEP},
			},
			wantErr: "L2VNI l2a: could not find corresponding L3VNI \"missing\" in tunnel overheads map[]",
		},
		{
			name: "L3VNI with unparseable VTEP IP",
			apiConfig: APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "vni1"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{"vni1": "not-a-cidr"},
				forL2VNIs: map[string]string{},
			},
			wantErr: `L3VNI vni1: could not determine AF of tunnel VTEP IP: "not-a-cidr"`,
		},
		{
			name: "standalone L2VNI with unparseable VTEP IP",
			apiConfig: APIConfigData{
				L2VNIs: []v1alpha1.L2VNI{
					{ObjectMeta: metav1.ObjectMeta{Name: "l2a"}},
				},
			},
			vtepIPs: tunnelVtepIPs{
				forL3VNIs: map[string]string{},
				forL2VNIs: map[string]string{"l2a": "garbage"},
			},
			wantErr: `L2VNI l2a: could not determine AF of tunnel VTEP IP: "garbage"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := overheadForTunnels(tt.apiConfig, tt.vtepIPs)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("overheadForTunnels()\ngot  = %+v\nwant = %+v", got, tt.want)
			}
		})
	}
}

func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// netdevInterfaces builds hostnetwork.UnderlayInterface entries of the
// netdev kind for the given names.
func netdevInterfaces(names ...string) []hostnetwork.UnderlayInterface {
	res := make([]hostnetwork.UnderlayInterface, 0, len(names))
	for _, name := range names {
		res = append(res, hostnetwork.UnderlayInterface{
			InterfaceName: name,
			Kind:          hostnetwork.UnderlayInterfaceNetDev,
		})
	}
	return res
}
