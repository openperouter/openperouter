// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestValidateGroutUnderlayCNI(t *testing.T) {
	tests := []struct {
		name    string
		iface   v1alpha1.UnderlayInterface
		wantErr string
	}{
		{
			name: "CNI dev underlay should be rejected on grout",
			iface: v1alpha1.UnderlayInterface{
				Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
				CNIDevice: &v1alpha1.CNIDevice{
					Type:      v1alpha1.CNIConfigTypeRawConfig,
					RawConfig: &apiextensionsv1.JSON{Raw: []byte(`{"cniVersion":"1.0.0","name":"u","type":"macvlan"}`)},
				},
			},
			wantErr: "CNI dev underlays are not supported with the grout datapath",
		},
		{
			name: "network device underlay should be accepted on grout",
			iface: v1alpha1.UnderlayInterface{
				Type: v1alpha1.UnderlayInterfaceTypeNetworkDevice,
				NetworkDevice: &v1alpha1.NetworkDevice{
					InterfaceName: "eth0",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			underlay := v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					Interfaces: []v1alpha1.UnderlayInterface{tt.iface},
				},
			}
			obtainedErr := ""
			err := ValidateGroutUnderlay(underlay)
			if err != nil {
				obtainedErr = err.Error()
			}
			if obtainedErr != tt.wantErr {
				t.Errorf("ValidateGroutUnderlay() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateGroutUnderlay(t *testing.T) {
	tests := []struct {
		name string
		nics []string
	}{
		{
			name: "no nics",
		},
		{
			name: "valid nic name",
			nics: []string{"eth0"},
		},
		{
			name: "nic name at the kernel length limit",
			nics: []string{"a23456789012345"},
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

			if err := ValidateGroutUnderlay(underlay); err != nil {
				t.Errorf("ValidateGroutUnderlay() unexpected error = %v", err)
			}
		})
	}
}
