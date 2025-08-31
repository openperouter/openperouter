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
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "192.168.1.0/24"},
					Nics:     []string{"eth0"},
					ASN:      65001,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid VTEP CIDR",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "invalidCIDR"},
					Nics:     []string{"eth0", "eth1"},
					ASN:      65001,
				},
			},
			wantErr: true,
		},
		{
			name: "empty VTEP CIDR",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: ""},
					Nics:     []string{"eth0", "eth1"},
					ASN:      65001,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid NIC name",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "192.168.1.0/24"},
					Nics:     []string{"eth0", "1$^&invalid"},
					ASN:      65001,
				},
			},
			wantErr: true,
		},
		{
			name: "zero nics",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "192.168.1.0/24"},
					Nics:     []string{},
					ASN:      65001,
				},
			},
			wantErr: false,
		},
		{
			name: "valid underlay with no nics",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "192.168.1.0/24"},
					Nics:     nil,
					ASN:      65001,
				},
			},
			wantErr: false,
		},
		{
			name: "more than one nic",
			underlay: v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "192.168.1.0/24"},
					Nics:     []string{"eth0", "eth1"},
					ASN:      65001,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUnderlays([]v1alpha1.Underlay{tt.underlay})
			if (err != nil) != tt.wantErr {
				t.Errorf("validateUnderlay() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	// Additional test: more than one underlay should error
	t.Run("multiple underlays", func(t *testing.T) {
		underlays := []v1alpha1.Underlay{
			{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "192.168.1.0/24"},
					Nics:     []string{"eth0"},
					ASN:      65001,
				},
			},
			{
				Spec: v1alpha1.UnderlaySpec{
					EVPN: &v1alpha1.EVPNConfig{VTEPCIDR: "192.168.2.0/24"},
					Nics:     []string{"eth1"},
					ASN:      65002,
				},
			},
		}
		err := ValidateUnderlays(underlays)
		if err == nil {
			t.Errorf("expected error for multiple underlays, got nil")
		}
	})
}
