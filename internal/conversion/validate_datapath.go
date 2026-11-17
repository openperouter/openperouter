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
	for _, trunk := range apiConfig.L2Trunks {
		resourceErrors = append(resourceErrors, ValidateGroutL2Trunk(trunk))
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
	if len(apiConfig.L2Trunks) > 0 {
		return fmt.Errorf("l2trunk %s: L2Trunks are only supported with the grout datapath",
			apiConfig.L2Trunks[0].Name)
	}
	return nil
}

func hasAcceleratedConfig(iface v1alpha1.UnderlayInterface) bool {
	return iface.NetworkDevice != nil && iface.NetworkDevice.AcceleratedConfig != nil
}

// FilterL2Trunks removes trunks unsupported by the grout datapath.
func (g *GroutDatapathConfigValidator) FilterL2Trunks(trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	valid := trunks[:0]
	errs := make([]error, 0, len(trunks))
	for _, trunk := range trunks {
		if err := ValidateGroutL2Trunk(trunk); err != nil {
			errs = append(errs, l2TrunkValidationError(trunk.Name, err.Error()))
			continue
		}
		valid = append(valid, trunk)
	}
	return valid, errors.Join(errs...)
}

// FilterL2Trunks removes all trunks because the kernel datapath cannot carry them.
func (k *KernelDatapathConfigValidator) FilterL2Trunks(trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	errs := make([]error, 0, len(trunks))
	for _, trunk := range trunks {
		errs = append(errs, l2TrunkValidationError(trunk.Name,
			fmt.Sprintf("l2trunk %s: L2Trunks are only supported with the grout datapath", trunk.Name)))
	}
	return trunks[:0], errors.Join(errs...)
}
