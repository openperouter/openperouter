// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestValidateUnderlay(t *testing.T) {
	tests := []struct {
		name     string
		underlay v1alpha1.Underlay
		wantErr  bool
	}{
		{
			name: "valid underlay",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					VTEPCIDR: "192.168.1.0/24",
					Nics:     []string{"eth0", "eth1"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid VTEP CIDR",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					VTEPCIDR: "invalidCIDR",
					Nics:     []string{"eth0", "eth1"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid NIC name",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					VTEPCIDR: "192.168.1.0/24",
					Nics:     []string{"eth0", "1$^&invalid"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUnderlay(tt.underlay)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUnderlay() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
