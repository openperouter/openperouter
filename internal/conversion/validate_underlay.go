// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func ValidateUnderlay(underlay v1alpha1.Underlay) error {
	_, _, err := net.ParseCIDR(underlay.Spec.VTEPCIDR)
	if err != nil {
		return fmt.Errorf("invalid vtep CIDR format: %s - %w", underlay.Spec.VTEPCIDR, err)
	}

	for _, n := range underlay.Spec.Nics {
		if !isValidInterfaceName(n) {
			return fmt.Errorf("invalid nic: %s", n)
		}
	}
	return nil
}
