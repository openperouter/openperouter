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
		name          string
		nodeIndex     int
		targetNS      string
		underlays     []v1alpha1.Underlay
		vnis          []v1alpha1.VNI
		wantUnderlay  hostnetwork.UnderlayParams
		wantVNIParams []hostnetwork.VNIParams
		wantErr       bool
	}{
		{
			name:          "no underlays",
			nodeIndex:     0,
			targetNS:      "namespace",
			underlays:     []v1alpha1.Underlay{},
			vnis:          []v1alpha1.VNI{},
			wantUnderlay:  hostnetwork.UnderlayParams{},
			wantVNIParams: nil,
			wantErr:       false,
		},
		{
			name:      "multiple underlays",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth1"}, VTEPCIDR: "10.0.1.0/24"}},
			},
			vnis:          []v1alpha1.VNI{},
			wantUnderlay:  hostnetwork.UnderlayParams{},
			wantVNIParams: nil,
			wantErr:       true,
		},
		{
			name:      "valid input",
			nodeIndex: 0,
			targetNS:  "namespace",
			underlays: []v1alpha1.Underlay{
				{Spec: v1alpha1.UnderlaySpec{Nics: []string{"eth0"}, VTEPCIDR: "10.0.0.0/24"}},
			},
			vnis: []v1alpha1.VNI{
				{Spec: v1alpha1.VNISpec{VRF: ptr.String("red"), LocalCIDR: "10.1.0.0/24", VNI: 100, VXLanPort: 4789}},
			},
			wantUnderlay: hostnetwork.UnderlayParams{
				UnderlayInterface: "eth0",
				TargetNS:          "namespace",
				VtepIP:            "10.0.0.0",
			},
			wantVNIParams: []hostnetwork.VNIParams{
				{
					VRF:        "red",
					TargetNS:   "namespace",
					VTEPIP:     "10.0.0.0",
					VNI:        100,
					VethHostIP: "10.1.0.1/24",
					VethNSIP:   "10.1.0.0/24",
					VXLanPort:  4789,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUnderlay, gotVNIParams, err := APItoHostConfig(tt.nodeIndex, tt.targetNS, tt.underlays, tt.vnis)
			if (err != nil) != tt.wantErr {
				t.Errorf("APItoHostConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotUnderlay, tt.wantUnderlay) {
				t.Errorf("APItoHostConfig() gotUnderlay = %v, want %v", gotUnderlay, tt.wantUnderlay)
			}
			if !reflect.DeepEqual(gotVNIParams, tt.wantVNIParams) {
				t.Errorf("APItoHostConfig() gotVNIParams = %v, want %v", gotVNIParams, tt.wantVNIParams)
			}
		})
	}
}
