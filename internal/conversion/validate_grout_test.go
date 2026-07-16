// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestValidateGroutL2VNI(t *testing.T) {
	err := ValidateGroutL2VNI(v1alpha1.L2VNI{})
	if err == nil {
		t.Error("ValidateGroutL2VNI() expected error, got nil")
	}
}

func TestValidateGroutL3VNI(t *testing.T) {
	err := ValidateGroutL3VNI(v1alpha1.L3VNI{})
	if err == nil {
		t.Error("ValidateGroutL3VNI() expected error, got nil")
	}
}

func TestValidateGroutL3Passthrough(t *testing.T) {
	err := ValidateGroutL3Passthrough(v1alpha1.L3Passthrough{})
	if err != nil {
		t.Errorf("ValidateGroutL3Passthrough() unexpected error: %v", err)
	}
}

func TestValidateGroutUnderlay(t *testing.T) {
	tests := []struct {
		name    string
		nics    []string
		wantErr bool
	}{
		{
			name:    "no nics",
			nics:    nil,
			wantErr: false,
		},
		{
			name:    "valid nic name",
			nics:    []string{"eth0"},
			wantErr: false,
		},
		{
			name:    "nic name at 13 char limit",
			nics:    []string{"1234567890123"},
			wantErr: false,
		},
		{
			name:    "nic name over 13 char limit",
			nics:    []string{"12345678901234"},
			wantErr: true,
		},
		{
			name:    "multiple nics, one too long",
			nics:    []string{"eth0", "12345678901234"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			underlay := v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					Interfaces: []v1alpha1.UnderlayInterface{},
				},
			}

			for _, nic := range tt.nics {
				underlay.Spec.Interfaces = append(underlay.Spec.Interfaces, v1alpha1.UnderlayInterface{
					Type: v1alpha1.UnderlayInterfaceTypeNetworkDevice,
					NetworkDevice: &v1alpha1.NetworkDevice{
						InterfaceName: nic,
					},
				})
			}

			err := ValidateGroutUnderlay(underlay)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGroutUnderlay() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
