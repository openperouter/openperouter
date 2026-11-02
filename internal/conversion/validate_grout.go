// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func ValidateGroutUnderlay(underlay v1alpha1.Underlay) error {
	for _, iface := range underlay.Spec.Interfaces {
		if iface.Type == v1alpha1.UnderlayInterfaceTypeCNIDevice {
			return fmt.Errorf("CNI dev underlays are not supported with the grout datapath")
		}
	}
	return nil
}
