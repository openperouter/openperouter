// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
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

func (k *KernelDatapathConfigValidator) Validate(_ APIConfigData) error {
	return nil
}
