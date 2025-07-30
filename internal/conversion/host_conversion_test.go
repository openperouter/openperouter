// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"reflect"
	"testing"

	"github.com/aws/smithy-go/ptr"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

func TestAPItoHostConfig(t *testing.T) {
	tests := []struct {
		name            string
		nodeIndex       int
		targetNS        string
		underlays       []v1alpha1.Underlay
		vnis            []v1alpha1.L3VNI
		l2vnis          []v1alpha1.L2VNI
		wantUnderlay    hostnetwork.UnderlayParams
		wantL2VNIParams []hostnetwork.L2VNIParams
		wantL3VNIParams []hostnetwork.L3VNIParams
		wantErr         bool
	}{
		{
			name:         "no underlays",
			nodeIndex:    0,
			targetNS:     "namespace",
			underlays:    []v1alpha1.Underlay{},
			vnis:         []v1alpha1.L3VNI{},
			wantUnderlay: hostnetwork.UnderlayParams{},
			wantErr:      false,
		},
		{
			name:      "multiple underlays",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth1"}, VTEPCIDR: "10.0.1.0/24"}},
			},
			vnis:         []v1alpha1.L3VNI{},
			wantUnderlay: hostnetwork.UnderlayParams{},
			wantErr:      true,
		},
		{
			name:      "ipv4 only",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: ptr.String("red"), HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "10.1.0.0/24"}}, VNI: 100, VXLanPort: 4789}},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				VtepIP:            "10.0.0.0/32",
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
					VethHostIPv4: "10.1.0.2/24",
					VethNSIPv4:   "10.1.0.1/24",
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantErr:         false,
		},
		{
			name:      "ipv6 only",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: ptr.String("red"), HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv6: "2001:db8::/64"}}, VNI: 100, VXLanPort: 4789}},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				VtepIP:            "10.0.0.0/32",
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
					VethHostIPv6: "2001:db8::2/64",
					VethNSIPv6:   "2001:db8::1/64",
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantErr:         false,
		},
		{
			name:      "dual stack",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
			},
			vnis: []v1alpha1.L3VNI{
				{Spec: v1alpha1.L3VNISpec{VRF: ptr.String("red"), HostSession: &v1alpha1.HostSession{LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: "10.1.0.0/24", IPv6: "2001:db8::/64"}}, VNI: 100, VXLanPort: 4789}},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				VtepIP:            "10.0.0.0/32",
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
					VethHostIPv4: "10.1.0.2/24",
					VethNSIPv4:   "10.1.0.1/24",
					VethHostIPv6: "2001:db8::2/64",
					VethNSIPv6:   "2001:db8::1/64",
				},
			},
			wantL2VNIParams: []hostnetwork.L2VNIParams{},
			wantErr:         false,
		},
		{
			name:      "l2 vni input",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
			},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 200, VXLanPort: 4789}},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				VtepIP:            "10.0.0.0/32",
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
					L2GatewayIP: nil,
					HostMaster:  nil,
				},
			},
			wantErr: false,
		},
		{
			name:      "l2 vni with hostmaster and l2gatewayip",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
			},
			l2vnis: []v1alpha1.L2VNI{
				{Spec: v1alpha1.L2VNISpec{VNI: 201, VXLanPort: 4789, HostMaster: &v1alpha1.HostMaster{Name: "br0"}, L2GatewayIP: "192.168.100.1/24"}},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				VtepIP:            "10.0.0.0/32",
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
					L2GatewayIP: ptr.String("192.168.100.1/24"),
					HostMaster:  &hostnetwork.HostMaster{Name: "br0"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUnderlay, gotL3VNIParams, gotL2VNIParams, err := APItoHostConfig(tt.nodeIndex, tt.targetNS, tt.underlays, tt.vnis, tt.l2vnis)
			if (err != nil) != tt.wantErr {
				t.Errorf("APItoHostConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotUnderlay, tt.wantUnderlay) {
				t.Errorf("APItoHostConfig() gotUnderlay = %v, want %v", gotUnderlay, tt.wantUnderlay)
			}
			if !reflect.DeepEqual(gotL3VNIParams, tt.wantL3VNIParams) {
				t.Errorf("APItoHostConfig() gotL3VNIParams = %v, want %v", gotL3VNIParams, tt.wantL3VNIParams)
			}
			if !reflect.DeepEqual(gotL2VNIParams, tt.wantL2VNIParams) {
				t.Errorf("APItoHostConfig() gotL2VNIParams = %v, want %v", gotL2VNIParams, tt.wantL2VNIParams)
			}
		})
	}
}
