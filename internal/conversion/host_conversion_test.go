// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"reflect"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

func TestAPItoHostConfig(t *testing.T) {
	tests := []struct {
		name               string
		nodeIndex          int
		targetNS           string
		underlayFromMultus bool
		underlays          []v1alpha1.Underlay
		vnis               []v1alpha1.L3VNI
		l2vnis             []v1alpha1.L2VNI
		l3Passthrough      []v1alpha1.L3Passthrough
		wantUnderlay       hostnetwork.UnderlayParams
		wantL2VNIParams    []hostnetwork.L2VNIParams
		wantL3VNIParams    []hostnetwork.L3VNIParams
		wantPassthrough    *hostnetwork.PassthroughParams
		wantErr            bool
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
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth1"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.1.0/24"}}},
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
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "10.1.0.0/24"}}, VNI: 100, VXLanPort: 4789}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				EVPN: &hostnetwork.UnderlayEVPNParams{
					VtepIP: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: 4789,
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
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "2001:db8::/64"}}, VNI: 100, VXLanPort: 4789}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				EVPN: &hostnetwork.UnderlayEVPNParams{
					VtepIP: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: 4789,
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
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "10.1.0.0/24", IPv6: "2001:db8::/64"}}, VNI: 100, VXLanPort: 4789}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				EVPN: &hostnetwork.UnderlayEVPNParams{
					VtepIP: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: 4789,
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
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
			},
			vnis: []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 200, VXLanPort: 4789}},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				EVPN: &hostnetwork.UnderlayEVPNParams{
					VtepIP: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       200,
						VXLanPort: 4789,
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
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
			},
			vnis: []v1alpha1.L3VNI{},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 201, VXLanPort: 4789, HostMaster: &v1alpha1.HostMaster{Type: "linux-bridge", LinuxBridge: &v1alpha1.LinuxBridgeConfig{Name: "br0"}}, L2GatewayIPs: []string{"192.168.100.1/24"}}},
			},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				EVPN: &hostnetwork.UnderlayEVPNParams{
					VtepIP: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       201,
						VXLanPort: 4789,
					},
					L2GatewayIPs: []string{"192.168.100.1/24"},
					HostMaster:   &hostnetwork.HostMaster{Name: "br0", Type: "linux-bridge"},
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
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: "red", VNI: 100, VXLanPort: 4789}},
			},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				EVPN: &hostnetwork.UnderlayEVPNParams{
					VtepIP: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{
				{
					VNIParams: hostnetwork.VNIParams{
						VRF:       "red",
						TargetNS:  "namespace",
						VTEPIP:    "10.0.0.0/32",
						VNI:       100,
						VXLanPort: 4789,
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
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
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
								IPv4: "192.168.2.0/24",
								IPv6: "2001:db8::/64",
							},
						},
					},
				},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
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
			name:               "underlay without specifying NIC, using multus",
			nodeIndex:          0,
			targetNS:           "namespace",
			underlayFromMultus: true,
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{}, EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "10.0.0.0/24"}}},
			},
			vnis:          []v1alpha1.L3VNI{},
			l2vnis:        []v1alpha1.L2VNI{},
			l3Passthrough: []v1alpha1.L3Passthrough{},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "",
				TargetNS:          "namespace",
				EVPN: &hostnetwork.UnderlayEVPNParams{
					VtepIP: "10.0.0.0/32",
				},
			},
			wantL3VNIParams: []hostnetwork.L3VNIParams{},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantPassthrough: nil,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiConfig := ApiConfigData{
				Underlays:     tt.underlays,
				L3VNIs:        tt.vnis,
				L2VNIs:        tt.l2vnis,
				L3Passthrough: tt.l3Passthrough,
			}

			gotHostConfig, err := APItoHostConfig(tt.nodeIndex, tt.targetNS, tt.underlayFromMultus, apiConfig)
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
