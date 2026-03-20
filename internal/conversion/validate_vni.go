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

// ValidateL3VNIs runs L3VNI specific validation, per Node.
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

// ValidateL3VNIs runs L3VNI specific validation.
func ValidateL3VNIs(l3Vnis []v1alpha1.L3VNI) error {
	vnis := vnisFromL3VNIs(l3Vnis)
	if err := validateVNIs(vnis); err != nil {
		return err
	}
	return nil
}

// ValidateL2VNIs runs L2VNI specific validation, per Node.
func ValidateL2VNIsForNodes(nodes []corev1.Node, underlays []v1alpha1.L2VNI) error {
	for _, node := range nodes {
		filteredL2VNIs, err := filter.L2VNIsForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		if err := ValidateL2VNIs(filteredL2VNIs); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}
	return nil
}

// ValidateL2VNIs runs L2VNI specific validation.
func ValidateL2VNIs(l2Vnis []v1alpha1.L2VNI) error {
	// Convert L2VNIs to vni structs
	vnis := vnisFromL2VNIs(l2Vnis)

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

	return nil
}

// ValidateVRF validates that the information in each VRF as a whole is correct, per Node.
func ValidateVRFsForNodes(nodes []corev1.Node, l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI) error {
	for _, node := range nodes {
		filteredL2VNIs, err := filter.L2VNIsForNode(&node, l2vnis)
		if err != nil {
			return fmt.Errorf("failed to filter l2vnis for node %q: %w", node.Name, err)
		}
		filteredL3VNIs, err := filter.L3VNIsForNode(&node, l3vnis)
		if err != nil {
			return fmt.Errorf("failed to filter l3vnis for node %q: %w", node.Name, err)
		}
		if err := ValidateVRFs(filteredL2VNIs, filteredL3VNIs); err != nil {
			return fmt.Errorf("failed to validate VRFs for node %q: %w", node.Name, err)
		}
	}
	return nil
}

// ValidateVRFs validates that the information in each VRF as a whole is correct.
func ValidateVRFs(l2Vnis []v1alpha1.L2VNI, l3Vnis []v1alpha1.L3VNI) error {
	// Make sure that there's only a single l3Vni in a given VRF.
	existingVrfs := map[string]string{} // a map between the given VRF and the VNI instance it's configured in
	for _, l3Vni := range l3Vnis {
		namespaceName := fmt.Sprintf("%s/%s", l3Vni.Namespace, l3Vni.Name)
		existingVNI, doesVRFAlreadyExist := existingVrfs[l3Vni.Spec.VRF]
		if doesVRFAlreadyExist {
			return fmt.Errorf("more than one L3VNI detected in VRF %q: %s - %s", l3Vni.Spec.VRF, existingVNI, namespaceName)
		}
		existingVrfs[l3Vni.Spec.VRF] = namespaceName
	}

	// Make sure that there are no subnet overlaps in the VRFs.
	v4SubnetsListL2, v6SubnetsListL2, err := vniSubnetsFromL2VNIs(l2Vnis)
	if err != nil {
		return err
	}
	v4SubnetsListL3, v6SubnetsListL3, err := vniSubnetsFromL3VNIs(l3Vnis)
	if err != nil {
		return err
	}
	v4VrfSubnetList := append(v4SubnetsListL2, v4SubnetsListL3...).byVRF()
	for vrf, subnetList := range v4VrfSubnetList {
		if err := subnetList.sort().hasSubnetOverlap(); err != nil {
			return fmt.Errorf("subnet overlap in VRF %q: %w", vrf, err)
		}
	}
	v6VrfSubnetList := append(v6SubnetsListL2, v6SubnetsListL3...).byVRF()
	for vrf, subnetList := range v6VrfSubnetList {
		if err := subnetList.sort().hasSubnetOverlap(); err != nil {
			return fmt.Errorf("subnet overlap in VRF %q: %w", vrf, err)
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

// vniSubnetsFromL3VNIs extracts the IPv4 and IPv6 subnets for each l3VNI.
func vniSubnetsFromL3VNIs(l3vnis []v1alpha1.L3VNI) (v4, v6 vniSubnetsList, err error) {
	v4 = make([]vniSubnet, 0, len(l3vnis))
	v6 = make([]vniSubnet, 0, len(l3vnis))
	for _, l3vni := range l3vnis {
		if l3vni.Spec.HostSession == nil {
			continue
		}

		if ipv4CIDR := l3vni.Spec.HostSession.LocalCIDR.IPv4; ipv4CIDR != "" {
			_, ipnetV4, err := net.ParseCIDR(ipv4CIDR)
			if err != nil {
				return nil, nil, fmt.Errorf("issue parsing L3VNI %s/%s LocalCIDR IPv4, err: %w",
					l3vni.Namespace, l3vni.Name, err)
			}
			v4 = append(v4, vniSubnet{
				vrfName: l3vni.Spec.VRF,
				source:  fmt.Sprintf("L3VNI %s/%s", l3vni.Namespace, l3vni.Name),
				subnet:  ipnetV4,
			})
		}

		if ipv6CIDR := l3vni.Spec.HostSession.LocalCIDR.IPv6; ipv6CIDR != "" {
			_, ipnetV6, err := net.ParseCIDR(ipv6CIDR)
			if err != nil {
				return nil, nil, fmt.Errorf("issue parsing L3VNI %s/%s LocalCIDR IPv6, err: %w",
					l3vni.Namespace, l3vni.Name, err)
			}
			v6 = append(v6, vniSubnet{
				vrfName: l3vni.Spec.VRF,
				source:  fmt.Sprintf("L3VNI %s/%s", l3vni.Namespace, l3vni.Name),
				subnet:  ipnetV6,
			})
		}
	}
	return v4, v6, err
}

// vniSubnetsFromL2VNIs extracts the IPv4 and IPv6 subnets for each l3VNI and returns an error if any have duplicate
// IPv4 or IPv6 information.
func vniSubnetsFromL2VNIs(l2vnis []v1alpha1.L2VNI) (v4, v6 vniSubnetsList, err error) {
	v4 = make([]vniSubnet, 0, len(l2vnis))
	v6 = make([]vniSubnet, 0, len(l2vnis))
	for _, l2vni := range l2vnis {
		var v4Added, v6Added bool
		for _, subnet := range l2vni.Spec.L2GatewayIPs {
			_, ipnet, err := net.ParseCIDR(subnet)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid l2gatewayips for vni %q = %v: %w", l2vni.Name, l2vni.Spec.L2GatewayIPs, err)
			}
			if ipfamily.ForCIDR(ipnet) == ipfamily.IPv4 {
				if v4Added {
					return nil, nil, fmt.Errorf("invalid l2gatewayips for vni %q = %v: contains more than 1 IPv4 Subnet",
						l2vni.Name, l2vni.Spec.L2GatewayIPs)
				}
				v4 = append(v4, vniSubnet{
					vrfName: l2vni.VRFName(),
					source:  fmt.Sprintf("L2VNI %s/%s", l2vni.Namespace, l2vni.Name),
					subnet:  ipnet,
				})
				continue
			}
			if v6Added {
				return nil, nil, fmt.Errorf("invalid l2gatewayips for vni %q = %v: contains more than 1 IPv6 Subnet",
					l2vni.Name, l2vni.Spec.L2GatewayIPs)
			}
			v6 = append(v6, vniSubnet{
				vrfName: l2vni.VRFName(),
				source:  fmt.Sprintf("L2VNI %s/%s", l2vni.Namespace, l2vni.Name),
				subnet:  ipnet,
			})
		}
	}
	return v4, v6, err
}

// vniSubnet holds subnet information for a single VNI and IP address family and the
// vrfName to uniquely identify the subnets.
type vniSubnet struct {
	vrfName string
	source  string
	subnet  *net.IPNet
}

type vniSubnetsList []vniSubnet

// byVRF creates a map that groups the vniSubnetsList by VRF name.
func (vniSubnets vniSubnetsList) byVRF() map[string]vniSubnetsList {
	vrfSubnets := make(map[string]vniSubnetsList)
	for _, vniSubnet := range vniSubnets {
		vrfSubnets[vniSubnet.vrfName] = append(vrfSubnets[vniSubnet.vrfName], vniSubnet)
	}
	return vrfSubnets
}

// sort sorts the vniSubnets in place by network address (starting IP), then by prefix length (longer first), then by
// the source string (not relevant for the algorithm itself, but for stable error messages).
// For convenience, sort returns the sorted vniSubnetsList.
func (vniSubnets vniSubnetsList) sort() vniSubnetsList {
	slices.SortStableFunc(vniSubnets, func(a, b vniSubnet) int {
		// Sort by network address first.
		cmp := bytes.Compare(a.subnet.IP, b.subnet.IP)
		if cmp != 0 {
			return cmp
		}
		// If network addresses are equal, sort by prefix length (longer first).
		prefixLengthA, _ := a.subnet.Mask.Size()
		prefixLengthB, _ := b.subnet.Mask.Size()
		if prefixLengthA > prefixLengthB {
			return -1
		}
		if prefixLengthA < prefixLengthB {
			return 1
		}
		// Not relevant for the actual algorithm, but needed for stable error messages.
		if a.source < b.source {
			return -1
		}
		if a.source > b.source {
			return 1
		}
		return 0
	})
	return vniSubnets
}

// hasIPOverlap takes a vniSubnetsList and checks if any of its subnets overlap. The list must be sorted with sort().
// The algorithm works by: Iterating through sorted subnets once, checking if each subnet overlaps with the next.
func (vniSubnets vniSubnetsList) hasSubnetOverlap() error {
	if len(vniSubnets) <= 1 {
		return nil
	}

	// Check for overlaps by comparing each subnet with the next
	for i := 0; i < len(vniSubnets)-1; i++ {
		current := vniSubnets[i]
		next := vniSubnets[i+1]

		// Check if current contains next's first IP (if next is not in current, we know that none of the following
		// is in current, because all are sorted).
		if current.subnet.Contains(next.subnet.IP) {
			return fmt.Errorf("IPNet %s (%s) overlaps with IPNet %s (%s)",
				next.subnet.String(), next.source, current.subnet.String(), current.source)
		}
	}
	return nil
}
