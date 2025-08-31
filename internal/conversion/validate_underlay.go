// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func ValidateUnderlays(underlays []v1alpha1.Underlay) error {
	if len(underlays) > 1 {
		return fmt.Errorf("can't have more than one underlay")
	}

	for _, underlay := range underlays {
		if underlay.Spec.ASN == 0 {
			return fmt.Errorf("underlay %s must have a valid ASN", underlay.Name)
		}

		if underlay.Spec.EVPN != nil {
			_, _, err := net.ParseCIDR(underlay.Spec.EVPN.VTEPCIDR)
			if err != nil {
				return fmt.Errorf("invalid vtep CIDR format for underlay %s: %s - %w", underlay.Name, underlay.Spec.EVPN.VTEPCIDR, err)
			}
		}

		if len(underlay.Spec.Nics) > 1 {
			return fmt.Errorf("underlay %s can only have one nic, found %d", underlay.Name, len(underlay.Spec.Nics))
		}

		for _, n := range underlay.Spec.Nics {
			if err := isValidInterfaceName(n); err != nil {
				return fmt.Errorf("invalid nic name for underlay %s: %s - %w", underlay.Name, n, err)
			}
		}
	}
	return nil
}
