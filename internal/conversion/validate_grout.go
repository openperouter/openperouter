// SPDX-License-Identifier:Apache-2.0

package conversion

import "fmt"

func ValidateGrout(groutEnabled bool, apiConfig ApiConfigData) error {
	if !groutEnabled {
		return nil
	}
	if len(apiConfig.L2VNIs) > 0 {
		return fmt.Errorf("L2VNI resources are not supported when grout datapath is enabled")
	}
	if len(apiConfig.L3VNIs) > 0 {
		return fmt.Errorf("L3VNI resources are not supported when grout datapath is enabled")
	}
	return nil
}
