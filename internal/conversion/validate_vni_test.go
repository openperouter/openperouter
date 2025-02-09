// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateVNIs(t *testing.T) {
	tests := []struct {
		name    string
		vnis    []v1alpha1.VNI
		wantErr bool
	}{
		{
			name: "valid VNIs",
			vnis: []v1alpha1.VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.1.0/24",
					},
					Status: v1alpha1.VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.VNISpec{
						VNI:       1002,
						LocalCIDR: "192.168.2.0/24",
					},
					Status: v1alpha1.VNIStatus{},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate VRF name",
			vnis: []v1alpha1.VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.1.0/24",
					},
					Status: v1alpha1.VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.VNISpec{
						VNI:       1002,
						LocalCIDR: "192.168.2.0/24",
					},
					Status: v1alpha1.VNIStatus{},
				},
			},
			wantErr: true,
		},
		{
			name: "overlapping CIDRs",
			vnis: []v1alpha1.VNI{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni1"},
					Spec: v1alpha1.VNISpec{
						VNI:       1001,
						LocalCIDR: "192.168.1.0/24",
					},
					Status: v1alpha1.VNIStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "vni2"},
					Spec: v1alpha1.VNISpec{
						VNI:       1002,
						LocalCIDR: "192.168.1.128/25",
					},
					Status: v1alpha1.VNIStatus{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVNIs(tt.vnis)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateVNIs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
