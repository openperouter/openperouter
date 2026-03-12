// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"bytes"
	"fmt"
	"net"
	"regexp"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/ipfamily"
)

var interfaceNameRegexp *regexp.Regexp

func init() {
	interfaceNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)
}

func ValidateL3VNIsForNodes(nodes []corev1.Node, underlays []v1alpha1.L3VNI) error {
	for _, node := range nodes {
		filteredL3VNIs, err := filter.L3VNIsForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		if err := ValidateL3VNIs(filteredL3VNIs); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func ValidateL3VNIs(l3Vnis []v1alpha1.L3VNI) error {
	vnis, err := vnisFromL3VNIs(l3Vnis, false)
	if err != nil {
		return err
	}

	if err := validateVNIs(vnis); err != nil {
		return err
	}

	existingVrfs := map[string]string{} // a map between the given VRF and the VNI instance it's configured in
	for _, vni := range vnis {
		existing, ok := existingVrfs[vni.vrfName]
		if ok {
			return fmt.Errorf("duplicate vrf %s: %s - %s", vni.vrfName, existing, vni.name)
		}
		existingVrfs[vni.vrfName] = vni.name
	}

	return nil
}

func ValidateL2VNIsForNodes(nodes []corev1.Node, l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI) error {
	for _, node := range nodes {
		filteredL2VNIs, err := filter.L2VNIsForNode(&node, l2vnis)
		if err != nil {
			return fmt.Errorf("failed to filter l2vnis for node %q: %w", node.Name, err)
		}
		filteredL3VNIs, err := filter.L3VNIsForNode(&node, l3vnis)
		if err != nil {
			return fmt.Errorf("failed to filter l3vnis for node %q: %w", node.Name, err)
		}
		if err := ValidateL2VNIs(filteredL2VNIs, filteredL3VNIs); err != nil {
			return fmt.Errorf("failed to validate l2vnis for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func ValidateL2VNIs(l2Vnis []v1alpha1.L2VNI, l3Vnis []v1alpha1.L3VNI) error {
	// Convert L2VNIs to vni structs
	vnis, err := vnisFromL2VNIs(l2Vnis)
	if err != nil {
		return err
	}

	// Perform common validation
	if err := validateVNIs(vnis); err != nil {
		return err
	}

	// Perform L2-specific validation (HostMaster and L2GatewayIPs validation)
	for _, vni := range l2Vnis {
		if vni.Spec.HostMaster != nil {
			if err := validateHostMaster(vni.Name, vni.Spec.HostMaster); err != nil {
				return err
			}
		}
		if len(vni.Spec.L2GatewayIPs) > 0 {
			_, err := ipfamily.ForCIDRStrings(vni.Spec.L2GatewayIPs...)
			if err != nil {
				return fmt.Errorf("invalid l2gatewayips for vni %q = %v: %w", vni.Name, vni.Spec.L2GatewayIPs, err)
			}
		}
	}

	// Convert L3VNIs to vni structs
	vnisL3, err := vnisFromL3VNIs(l3Vnis, true)
	if err != nil {
		return err
	}
	// Check if there's any overlap between the subnets.
	if err := hasSubnetOverlapInVRF(append(vnis, vnisL3...)); err != nil {
		return err
	}

	return nil
}

// vni holds VNI validation data
type vni struct {
	name     string
	vni      uint32
	vrfName  string
	subnetV4 *net.IPNet
	subnetV6 *net.IPNet
}

// vnisFromL3VNIs converts L3VNIs to vni slice. If getSubnetInfo is false, subnetV4 and subnetV6 will not be populated.
func vnisFromL3VNIs(l3vnis []v1alpha1.L3VNI, getSubnetInfo bool) ([]vni, error) {
	result := make([]vni, len(l3vnis))
	for i, l3vni := range l3vnis {
		result[i] = vni{
			name:    l3vni.Name,
			vni:     l3vni.Spec.VNI,
			vrfName: l3vni.Spec.VRF,
		}

		if !getSubnetInfo || l3vni.Spec.HostSession == nil {
			continue
		}

		// Parse IPv4 subnet if present
		if ipv4CIDR := l3vni.Spec.HostSession.LocalCIDR.IPv4; ipv4CIDR != "" {
			_, ipnetV4, err := net.ParseCIDR(ipv4CIDR)
			if err != nil {
				return nil, fmt.Errorf("issue parsing L3VNI %s/%s LocalCIDR IPv4, err: %w",
					l3vni.Namespace, l3vni.Name, err)
			}
			result[i].subnetV4 = ipnetV4
		}

		// Parse IPv6 subnet if present
		if ipv6CIDR := l3vni.Spec.HostSession.LocalCIDR.IPv6; ipv6CIDR != "" {
			_, ipnetV6, err := net.ParseCIDR(ipv6CIDR)
			if err != nil {
				return nil, fmt.Errorf("issue parsing L3VNI %s/%s LocalCIDR IPv6, err: %w",
					l3vni.Namespace, l3vni.Name, err)
			}
			result[i].subnetV6 = ipnetV6
		}
	}
	return result, nil
}

// vnisFromL2VNIs converts L2VNIs to vni slice
func vnisFromL2VNIs(l2vnis []v1alpha1.L2VNI) ([]vni, error) {
	result := make([]vni, len(l2vnis))
	for i, l2vni := range l2vnis {
		result[i] = vni{
			name:    l2vni.Name,
			vni:     l2vni.Spec.VNI,
			vrfName: l2vni.VRFName(),
		}

		for _, subnet := range l2vni.Spec.L2GatewayIPs {
			_, ipnet, err := net.ParseCIDR(subnet)
			if err != nil {
				return nil, fmt.Errorf("invalid l2gatewayips for vni %q = %v: %w", l2vni.Name, l2vni.Spec.L2GatewayIPs, err)
			}
			if ipfamily.ForCIDR(ipnet) == ipfamily.IPv4 {
				if result[i].subnetV4 != nil {
					return nil, fmt.Errorf("invalid l2gatewayips for vni %q = %v: contains more than 1 IPv4 Subnet",
						l2vni.Name, l2vni.Spec.L2GatewayIPs)
				}
				result[i].subnetV4 = ipnet
				continue
			}
			if result[i].subnetV6 != nil {
				return nil, fmt.Errorf("invalid l2gatewayips for vni %q = %v: contains more than 1 IPv6 Subnet",
					l2vni.Name, l2vni.Spec.L2GatewayIPs)
			}
			result[i].subnetV6 = ipnet
		}
	}
	return result, nil
}

// validateVNIs performs common validation logic for VNIs
func validateVNIs(vnis []vni) error {
	existingVNIs := map[uint32]string{} // a map between the given VNI number and the VNI instance it's configured in

	for _, vni := range vnis {
		if err := isValidInterfaceName(vni.vrfName); err != nil {
			return fmt.Errorf("invalid vrf name for vni %s: %s - %w", vni.name, vni.vrfName, err)
		}

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

func validateHostMaster(vniName string, hostConfig *v1alpha1.HostMaster) error {
	var name string
	switch hostConfig.Type {
	case v1alpha1.LinuxBridge:
		if hostConfig.LinuxBridge != nil {
			name = hostConfig.LinuxBridge.Name
		}
	case v1alpha1.OVSBridge:
		if hostConfig.OVSBridge != nil {
			name = hostConfig.OVSBridge.Name
		}
	default:
		return fmt.Errorf("invalid hostmaster type %q", hostConfig.Type)
	}

	if name == "" {
		return nil
	}

	if err := isValidInterfaceName(name); err != nil {
		return fmt.Errorf("invalid hostmaster name for vni %s: %s - %w", vniName, name, err)
	}

	return nil
}

// hasSubnetOverlapInVRF checks if any subnets for a given VRF overlap.
func hasSubnetOverlapInVRF(vnis []vni) error {
	vrfSubnetsV4 := make(map[string][]net.IPNet)
	vrfSubnetsV6 := make(map[string][]net.IPNet)
	for _, vni := range vnis {
		if vni.subnetV4 != nil {
			vrfSubnetsV4[vni.vrfName] = append(vrfSubnetsV4[vni.vrfName], *vni.subnetV4)
		}
		if vni.subnetV6 != nil {
			vrfSubnetsV6[vni.vrfName] = append(vrfSubnetsV6[vni.vrfName], *vni.subnetV6)
		}
	}
	for vrf, subnets := range vrfSubnetsV4 {
		if err := hasIPOverlap(subnets); err != nil {
			return fmt.Errorf("IP overlap in VRF %s, err: %w", vrf, err)
		}
	}
	for vrf, subnets := range vrfSubnetsV6 {
		if err := hasIPOverlap(subnets); err != nil {
			return fmt.Errorf("IP overlap in VRF %s, err: %w", vrf, err)
		}
	}
	return nil
}

// hasIPOverlap takes a slice of net.IPNets and checks if any of them overlap.
//
// The algorithm works by:
// 1. Sorting subnets by network address (starting IP), then by prefix length
// 2. Iterating through sorted subnets once, checking if each subnet overlaps with the next
func hasIPOverlap(ipNets []net.IPNet) error {
	if len(ipNets) <= 1 {
		return nil
	}

	// Sort by network address first, then by prefix length (longer prefixes first)
	slices.SortStableFunc(ipNets, func(a, b net.IPNet) int {
		cmp := bytes.Compare(a.IP, b.IP)
		if cmp != 0 {
			return cmp
		}
		// If network addresses are equal, sort by prefix length (longer first)
		prefixLengthA, _ := a.Mask.Size()
		prefixLengthB, _ := b.Mask.Size()
		if prefixLengthA > prefixLengthB {
			return -1
		}
		if prefixLengthA < prefixLengthB {
			return 1
		}
		return 0
	})

	// Check for overlaps by comparing each subnet with the next
	for i := 0; i < len(ipNets)-1; i++ {
		current := ipNets[i]
		next := ipNets[i+1]

		// Check if current contains next's first IP (if next is not in current, we know that none of the following
		// is in current, because all are sorted).
		if current.Contains(next.IP) {
			return fmt.Errorf("IPNet %s overlaps with IPNet %s", next.String(), current.String())
		}
	}
	return nil
}
