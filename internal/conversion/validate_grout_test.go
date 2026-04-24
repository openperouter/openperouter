// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestValidateGrout(t *testing.T) {
	tests := []struct {
		name         string
		groutEnabled bool
		apiConfig    ApiConfigData
		wantErr      bool
	}{
		{
			name:         "grout disabled, no VNIs",
			groutEnabled: false,
			apiConfig:    ApiConfigData{},
			wantErr:      false,
		},
		{
			name:         "grout disabled, L2VNIs present",
			groutEnabled: false,
			apiConfig: ApiConfigData{
				L2VNIs: []v1alpha1.L2VNI{{}},
			},
			wantErr: false,
		},
		{
			name:         "grout disabled, L3VNIs present",
			groutEnabled: false,
			apiConfig: ApiConfigData{
				L3VNIs: []v1alpha1.L3VNI{{}},
			},
			wantErr: false,
		},
		{
			name:         "grout enabled, no VNIs",
			groutEnabled: true,
			apiConfig:    ApiConfigData{},
			wantErr:      false,
		},
		{
			name:         "grout enabled, L2VNIs present",
			groutEnabled: true,
			apiConfig: ApiConfigData{
				L2VNIs: []v1alpha1.L2VNI{{}},
			},
			wantErr: true,
		},
		{
			name:         "grout enabled, L3VNIs present",
			groutEnabled: true,
			apiConfig: ApiConfigData{
				L3VNIs: []v1alpha1.L3VNI{{}},
			},
			wantErr: true,
		},
		{
			name:         "grout enabled, both L2 and L3 VNIs present",
			groutEnabled: true,
			apiConfig: ApiConfigData{
				L2VNIs: []v1alpha1.L2VNI{{}},
				L3VNIs: []v1alpha1.L3VNI{{}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGrout(tt.groutEnabled, tt.apiConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGrout() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
