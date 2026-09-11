// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

type DatapathConfigValidator interface {
	Validate(APIConfigData) error
}

type GroutDatapathConfigValidator struct{}

func (g *GroutDatapathConfigValidator) Validate(apiConfig APIConfigData) error {
	resourceErrors := make([]error, 0, 1)

	for _, underlay := range apiConfig.Underlays {
		resourceErrors = append(resourceErrors, ValidateGroutUnderlay(underlay))
	}
	return errors.Join(resourceErrors...)
}

type KernelDatapathConfigValidator struct{}

func (k *KernelDatapathConfigValidator) Validate(apiConfig APIConfigData) error {
	for _, underlay := range apiConfig.Underlays {
		for _, iface := range underlay.Spec.Interfaces {
			if hasAcceleratedConfig(iface) {
				return fmt.Errorf("underlay %s, interface %s: acceleratedConfig requires grout datapath",
					underlay.Name, iface.NetworkDevice.InterfaceName)
			}
		}
	}
	return nil
}

func hasAcceleratedConfig(iface v1alpha1.UnderlayInterface) bool {
	return iface.NetworkDevice != nil && iface.NetworkDevice.AcceleratedConfig != nil
}
