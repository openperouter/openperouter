// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"slices"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

type DatapathConfigValidator interface {
	Validate(APIConfigData) error
}

type GroutDatapathConfigValidator struct{}

func (g *GroutDatapathConfigValidator) Validate(apiConfig APIConfigData) error {
	resourceErrors := make([]error, 0, 1)

	for _, l3Passthrough := range apiConfig.L3Passthrough {
		resourceErrors = append(resourceErrors, ValidateGroutL3Passthrough(l3Passthrough))
	}
	for _, l3VNI := range apiConfig.L3VNIs {
		resourceErrors = append(resourceErrors, ValidateGroutL3VNI(l3VNI))
	}
	for _, l2VNI := range apiConfig.L2VNIs {
		resourceErrors = append(resourceErrors, ValidateGroutL2VNI(l2VNI))
	}
	for _, underlay := range apiConfig.Underlays {
		resourceErrors = append(resourceErrors, ValidateGroutUnderlay(underlay))
	}
	return errors.Join(resourceErrors...)
}

type KernelDatapathConfigValidator struct{}

func (k *KernelDatapathConfigValidator) Validate(apiConfig APIConfigData) error {
	for _, underlay := range apiConfig.Underlays {
		if slices.ContainsFunc(underlay.Spec.Interfaces, hasAcceleratedConfig) {
			return fmt.Errorf("acceleratedConfig requires grout datapath")
		}
	}
	return nil
}

func hasAcceleratedConfig(iface v1alpha1.UnderlayInterface) bool {
	return iface.Type == v1alpha1.UnderlayInterfaceTypeNetworkDevice &&
		iface.NetworkDevice != nil && iface.NetworkDevice.AcceleratedConfig != nil
}
