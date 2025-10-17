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
	interfaceNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)
}

func ValidateL3VNIs(l3Vnis []v1alpha1.L3VNI) error {
	vnis := vnisFromL3VNIs(l3Vnis)
	if err := validateVNIs(vnis); err != nil {
		return err
	}
	return nil
}

func ValidateL2VNIs(l2Vnis []v1alpha1.L2VNI) error {
	// Convert L2VNIs to vni structs
	vnis := vnisFromL2VNIs(l2Vnis)

	// Perform common validation
	if err := validateVNIs(vnis); err != nil {
		return err
	}

	// Perform L2-specific validation (HostMaster and L2GatewayIP validation)
	for _, vni := range l2Vnis {
		if vni.Spec.HostMaster != nil && vni.Spec.HostMaster.Name != "" {
			if err := isValidInterfaceName(vni.Spec.HostMaster.Name); err != nil {
				return fmt.Errorf("invalid hostmaster name for vni %s: %s - %w", vni.Name, vni.Spec.HostMaster.Name, err)
			}
		}
		if vni.Spec.L2GatewayIP != "" {
			if err := isValidCIDR(vni.Spec.L2GatewayIP); err != nil {
				return fmt.Errorf("invalid l2gatewayip for vni %s: %s - %w", vni.Name, vni.Spec.L2GatewayIP, err)
			}
		}
	}

	return nil
}

// vni holds VNI validation data
type vni struct {
	name    string
	vni     uint32
	vrfName string
}

// vnisFromL3VNIs converts L3VNIs to vni slice
func vnisFromL3VNIs(l3vnis []v1alpha1.L3VNI) []vni {
	result := make([]vni, len(l3vnis))
	for i, l3vni := range l3vnis {
		result[i] = vni{
			name:    l3vni.Name,
			vni:     l3vni.Spec.VNI,
			vrfName: l3vni.Spec.VRF,
		}
	}
	return result
}

// vnisFromL2VNIs converts L2VNIs to vni slice
func vnisFromL2VNIs(l2vnis []v1alpha1.L2VNI) []vni {
	result := make([]vni, len(l2vnis))
	for i, l2vni := range l2vnis {
		result[i] = vni{
			name:    l2vni.Name,
			vni:     l2vni.Spec.VNI,
			vrfName: l2vni.VRFName(),
		}
	}
	return result
}

// validateVNIs performs common validation logic for VNIs
func validateVNIs(vnis []vni) error {
	existingVrfs := map[string]string{} // a map between the given VRF and the VNI instance it's configured in
	existingVNIs := map[uint32]string{} // a map between the given VNI number and the VNI instance it's configured in

	for _, vni := range vnis {
		if err := isValidInterfaceName(vni.vrfName); err != nil {
			return fmt.Errorf("invalid vrf name for vni %s: %s - %w", vni.name, vni.vrfName, err)
		}
		existing, ok := existingVrfs[vni.vrfName]
		if ok {
			return fmt.Errorf("duplicate vrf %s: %s - %s", vni.vrfName, existing, vni.name)
		}
		existingVrfs[vni.vrfName] = vni.name

		existingVNI, ok := existingVNIs[vni.vni]
		if ok {
			return fmt.Errorf("duplicate vni %d:%s - %s", vni.vni, existingVNI, vni.name)
		}
		existingVNIs[vni.vni] = vni.name
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

func isValidCIDR(cidr string) error {
	if cidr == "" {
		return fmt.Errorf("CIDR cannot be empty")
	}
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		return fmt.Errorf("invalid CIDR: %s - %w", cidr, err)
	}
	return nil
}
