// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func ValidateUnderlays(underlays []v1alpha1.Underlay) error {
	for _, underlay := range underlays {
		_, _, err := net.ParseCIDR(underlay.Spec.VTEPCIDR)
		if err != nil {
			return fmt.Errorf("invalid vtep CIDR format for underlay %s: %s - %w", underlay.Name, underlay.Spec.VTEPCIDR, err)
		}

		for _, n := range underlay.Spec.Nics {
			if err := isValidInterfaceName(n); err != nil {
				return fmt.Errorf("invalid nic name for underlay %s: %s - %w", underlay.Name, n, err)
			}
		}
	}
	return nil
}
