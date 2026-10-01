// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
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
