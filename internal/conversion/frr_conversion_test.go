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
		vnis      []v1alpha1.VNI
		logLevel  string
		want      frr.Config
		wantErr   bool
	}{
		{
			name:      "no underlays",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{},
			vnis:      []v1alpha1.VNI{{}},
			wantErr:   true,
		},
		{
			name:      "no vnis",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:       65000,
						VTEPCIDR:  "192.168.1.0/24",
						Neighbors: []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis:    []v1alpha1.VNI{},
			wantErr: true,
		},
		{
			name:      "valid input",
			nodeIndex: 0,
			underlays: []v1alpha1.Underlay{
				{
					Spec: v1alpha1.UnderlaySpec{
						ASN:       65000,
						VTEPCIDR:  "192.168.1.0/24",
						Neighbors: []v1alpha1.Neighbor{{Address: "192.168.1.1", ASN: 65001}},
					},
				},
			},
			vnis: []v1alpha1.VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.VNISpec{
						ASN:       65000,
						LocalCIDR: "192.168.2.0/24",
						HostASN:   ptr.To(uint32(65001)),
						VRF:       ptr.To("vrf1"),
						VNI:       200,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.VNISpec{
						ASN:       65000,
						LocalCIDR: "192.168.3.0/24",
						VNI:       300,
					},
				},
			},
			logLevel: "debug",
			want: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					VTEP:  "192.168.1.0/32",
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
				VNIs: []frr.VNIConfig{
					{
						ASN: 65000,
						VNI: 200,
						VRF: "vrf1",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.2.1",
							ASN:  65001,
						},
						ToAdvertise: []string{"192.168.2.1/32"},
					},
					{
						ASN: 65000,
						VNI: 300,
						VRF: "vni2",
						LocalNeighbor: &frr.NeighborConfig{
							Addr: "192.168.3.1",
							ASN:  65000,
						},
						ToAdvertise: []string{"192.168.3.1/32"},
					},
				},
				Loglevel: "debug",
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
