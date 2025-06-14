// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"
	"regexp"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

var interfaceNameRegexp *regexp.Regexp

func init() {
	interfaceNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
}

func ValidateVNIs(vnis []v1alpha1.L3VNI) error {
	existingVrfs := map[string]string{}  // a map between the given VRF and the VNI instance it's configured in
	existingVNIs := map[uint32]string{}  // a map between the given VNI number and the VNI instance it's configured in
	existingCIDRs := map[string]string{} // a map between the given local cidr and the VNI instance it's configured in

	for _, vni := range vnis {
		vrfName := vni.VRFName()
		if err := isValidInterfaceName(vrfName); err != nil {
			return fmt.Errorf("invalid vrf name for vni %s: %s - %w", vni.Name, vrfName, err)
		}
		existing, ok := existingVrfs[vrfName]
		if ok {
			return fmt.Errorf("duplicate vrf %s: %s - %s", vrfName, existing, vni.Name)
		}
		existingVrfs[vrfName] = vni.Name

		existingVNI, ok := existingVNIs[vni.Spec.VNI]
		if ok {
			return fmt.Errorf("duplicate vni %d:%s - %s", vni.Spec.VNI, existingVNI, vni.Name)
		}
		existingVNIs[vni.Spec.VNI] = vni.Name

		for cidr, cidrVNI := range existingCIDRs {
			overlap, err := cidrsOverlap(cidr, vni.Spec.LocalCIDR)
			if err != nil {
				return err
			}
			if overlap {
				return fmt.Errorf("overlapping cidrs %s - %s for vnis %s - %s", cidr, vni.Spec.LocalCIDR, cidrVNI, vni.Name)
			}
		}
		existingCIDRs[vni.Spec.LocalCIDR] = vni.Name
	}

	return nil
}

func cidrsOverlap(cidr1, cidr2 string) (bool, error) {
	net1, ipNet1, err1 := net.ParseCIDR(cidr1)
	if err1 != nil {
		return false, fmt.Errorf("invalid CIDR %s: %v", cidr1, err1)
	}

	net2, ipNet2, err2 := net.ParseCIDR(cidr2)
	if err2 != nil {
		return false, fmt.Errorf("invalid CIDR %s: %v", cidr2, err2)
	}

	if ipNet1.Contains(net2) || ipNet2.Contains(net1) {
		return true, nil
	}

	return false, nil
}

func isValidInterfaceName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("interface name cannot be empty")
	}
	if len(name) > 15 {
		return fmt.Errorf("interface name %s can't be longer than 15 characters", name)
	}

	if !interfaceNameRegexp.MatchString(name) {
		return fmt.Errorf("interface name %s contains invalid characters", name)
	}
	return nil
}
