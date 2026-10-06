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

	for _, underlay := range apiConfig.Underlays {
		resourceErrors = append(resourceErrors, ValidateGroutUnderlay(underlay))
	}
	return errors.Join(resourceErrors...)
}

type KernelDatapathConfigValidator struct{}

func (k *KernelDatapathConfigValidator) Validate(_ APIConfigData) error {
	return nil
}
