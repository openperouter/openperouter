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
	resourceErrors := make([]error, 0, len(apiConfig.Underlays)+len(apiConfig.L2Trunks)+len(apiConfig.L2VNIs))

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
	for _, trunk := range apiConfig.L2Trunks {
		if trunk.Spec.Type != v1alpha1.L2TrunkTypeHost {
			return fmt.Errorf("l2trunk %s: the kernel datapath only supports type Host", trunk.Name)
		}
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

// FilterL2Trunks removes the trunks the kernel datapath cannot carry: only
// Host trunks are supported.
func (k *KernelDatapathConfigValidator) FilterL2Trunks(trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	valid := make([]v1alpha1.L2Trunk, 0, len(trunks))
	errs := make([]error, 0, len(trunks))
	for _, trunk := range trunks {
		if trunk.Spec.Type != v1alpha1.L2TrunkTypeHost {
			errs = append(errs, l2TrunkValidationError(trunk.Name,
				fmt.Sprintf("l2trunk %s: the kernel datapath only supports type Host", trunk.Name)))
			continue
		}
		valid = append(valid, trunk)
	}
	return valid, errors.Join(errs...)
}
