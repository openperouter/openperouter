// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestKernelDatapathRejectsAcceleratedConfig(t *testing.T) {
	validator := &KernelDatapathConfigValidator{}
	err := validator.Validate(APIConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				Spec: v1alpha1.UnderlaySpec{
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type: v1alpha1.UnderlayInterfaceTypeNetworkDevice,
							NetworkDevice: &v1alpha1.NetworkDevice{
								InterfaceName:     "enp3s0f0v0",
								AcceleratedConfig: &v1alpha1.AcceleratedConfig{},
							},
						},
					},
				},
			},
		},
	})
	require.ErrorContains(t, err, "acceleratedConfig requires grout")
}

func TestKernelDatapathAllowsNetworkDeviceWithoutAcceleratedConfig(t *testing.T) {
	validator := &KernelDatapathConfigValidator{}
	err := validator.Validate(APIConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				Spec: v1alpha1.UnderlaySpec{
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type: v1alpha1.UnderlayInterfaceTypeNetworkDevice,
							NetworkDevice: &v1alpha1.NetworkDevice{
								InterfaceName: "eth0",
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
}

func TestGroutDatapathAllowsAcceleratedConfig(t *testing.T) {
	validator := &GroutDatapathConfigValidator{}
	err := validator.Validate(APIConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				Spec: v1alpha1.UnderlaySpec{
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type: v1alpha1.UnderlayInterfaceTypeNetworkDevice,
							NetworkDevice: &v1alpha1.NetworkDevice{
								InterfaceName:     "enp3s0f0v0",
								AcceleratedConfig: &v1alpha1.AcceleratedConfig{},
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
}

func TestFilterL2TrunksForDatapath(t *testing.T) {
	first := sriovTrunk("first", "eth0", 1)
	second := sriovTrunk("second", "eth1", 256)
	tests := []struct {
		name        string
		filter      func([]v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error)
		want        []v1alpha1.L2Trunk
		failedNames []string
	}{
		{name: "kernel rejects each trunk", filter: (&KernelDatapathConfigValidator{}).FilterL2Trunks,
			want: []v1alpha1.L2Trunk{}, failedNames: []string{"first", "second"}},
		{name: "grout keeps supported trunks", filter: (&GroutDatapathConfigValidator{}).FilterL2Trunks,
			want: []v1alpha1.L2Trunk{first}, failedNames: []string{"second"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.filter([]v1alpha1.L2Trunk{first, second})
			require.Equal(t, tt.want, got)
			failures := openpeerrors.CollectFailures(err)
			var names []string
			for _, failure := range failures {
				require.Equal(t, openpeerrors.KindL2Trunk, failure.Kind)
				require.Equal(t, v1alpha1.FailedResourceReasonValidationFailed, failure.Reason)
				names = append(names, failure.Name)
			}
			require.Equal(t, tt.failedNames, names)
		})
	}
}
