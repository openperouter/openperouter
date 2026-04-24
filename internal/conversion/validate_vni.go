// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"bytes"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/ipfamily"
)

var interfaceNameRegexp *regexp.Regexp

func init() {
	interfaceNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)
}

// ValidateL3VNIsForNodes runs L3VNI specific validation, per Node.
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

// ValidateL2VNIsForNodes runs L2VNI specific validation, per Node.
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

// ValidateVRFsForNodes validates that the information in each VRF as a whole is correct, per Node.
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
// Note that when ValidateVRFs is called, L3VNIs and L2VNIs should already have been verified for correctness
// by ValidateL2VNIs() and ValidateL3VNIs() (such as individual subnets parse correctly, no duplicate AF per VNI).
func ValidateVRFs(l2Vnis []v1alpha1.L2VNI, l3Vnis []v1alpha1.L3VNI) error {
	// Make sure that there's only a single l3Vni in a given VRF.
	vrfToVNI := map[string]types.NamespacedName{}
	for _, l3Vni := range l3Vnis {
		namespaceName := types.NamespacedName{Namespace: l3Vni.Namespace, Name: l3Vni.Name}
		vrfName := ptr.Deref(l3Vni.Spec.VRF, "")
		l3vni, ok := vrfToVNI[vrfName]
		if ok {
			return fmt.Errorf("more than one L3VNI detected in VRF %q: %s - %s", vrfName, l3vni, namespaceName)
		}
		vrfToVNI[vrfName] = namespaceName
	}

	// Make sure that there are no subnet overlaps in the VRFs.
	v4SubnetsForVRF := map[string]subnets{}
	v6SubnetsForVRF := map[string]subnets{}
	for _, l2vni := range l2Vnis {
		vrfName := l2vni.VRFName()
		source := fmt.Sprintf("L2VNI %s", types.NamespacedName{Namespace: l2vni.Namespace, Name: l2vni.Name})
		if subnet := v4SubnetForL2(l2vni); subnet != nil {
			v4SubnetsForVRF[vrfName] = append(v4SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
		if subnet := v6SubnetForL2(l2vni); subnet != nil {
			v6SubnetsForVRF[vrfName] = append(v6SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
	}
	for _, l3vni := range l3Vnis {
		vrfName := ptr.Deref(l3vni.Spec.VRF, "")
		source := fmt.Sprintf("L3VNI %s", types.NamespacedName{Namespace: l3vni.Namespace, Name: l3vni.Name})
		if subnet := v4SubnetForL3(l3vni); subnet != nil {
			v4SubnetsForVRF[vrfName] = append(v4SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
		if subnet := v6SubnetForL3(l3vni); subnet != nil {
			v6SubnetsForVRF[vrfName] = append(v6SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
	}
	for vrf, subnetList := range v4SubnetsForVRF {
		subnetList.sort()
		if err := hasSubnetOverlap(subnetList); err != nil {
			return fmt.Errorf("subnet overlap in VRF %q: %w", vrf, err)
		}
	}
	for vrf, subnetList := range v6SubnetsForVRF {
		subnetList.sort()
		if err := hasSubnetOverlap(subnetList); err != nil {
			return fmt.Errorf("subnet overlap in VRF %q: %w", vrf, err)
		}
	}
	return nil
}

// vni holds VNI validation data
type VNI struct {
	name      string
	vni       uint32
	vrfName   string
	exportRTs []string
	importRTs []string
}

// vnisFromL3VNIs converts L3VNIs to vni slice
func vnisFromL3VNIs(l3vnis []v1alpha1.L3VNI) []VNI {
	result := make([]VNI, len(l3vnis))
	for i, l3vni := range l3vnis {
		result[i] = VNI{
			name:      l3vni.Name,
			vni:       uint32(ptr.Deref(l3vni.Spec.VNI, 0)),
			vrfName:   ptr.Deref(l3vni.Spec.VRF, ""),
			exportRTs: l3vni.Spec.ExportRTs,
			importRTs: l3vni.Spec.ImportRTs,
		}
	}
	return result
}

// vnisFromL2VNIs converts L2VNIs to vni slice
func vnisFromL2VNIs(l2vnis []v1alpha1.L2VNI) []VNI {
	result := make([]VNI, len(l2vnis))
	for i, l2vni := range l2vnis {
		result[i] = VNI{
			name:    l2vni.Name,
			vni:     uint32(ptr.Deref(l2vni.Spec.VNI, 0)),
			vrfName: l2vni.VRFName(),
		}
	}
	return result
}

// validateVNIs performs common validation logic for VNIs
func validateVNIs(vnis []VNI) error {
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

		if err := ValidateRouteTargets(vni); err != nil {
			return fmt.Errorf("invalid route targets for vni %s: %w", vni.name, err)
		}
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
			name = ptr.Deref(hostConfig.LinuxBridge.Name, "")
		}
	case v1alpha1.OVSBridge:
		if hostConfig.OVSBridge != nil {
			name = ptr.Deref(hostConfig.OVSBridge.Name, "")
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

// v4SubnetForL2 extracts the first valid IPv4 subnet from the l2vni, or returns nil.
func v4SubnetForL2(l2vni v1alpha1.L2VNI) *net.IPNet {
	for _, subnet := range l2vni.Spec.L2GatewayIPs {
		_, ipnet, err := net.ParseCIDR(subnet)
		if err != nil {
			continue
		}
		if ipfamily.ForCIDR(ipnet) == ipfamily.IPv4 {
			return ipnet
		}
	}
	return nil
}

// v6SubnetForL2 extracts the first valid IPv6 subnet from the l2vni, or returns nil.
func v6SubnetForL2(l2vni v1alpha1.L2VNI) *net.IPNet {
	for _, subnet := range l2vni.Spec.L2GatewayIPs {
		_, ipnet, err := net.ParseCIDR(subnet)
		if err != nil {
			continue
		}
		if ipfamily.ForCIDR(ipnet) == ipfamily.IPv6 {
			return ipnet
		}
	}
	return nil
}

// v4SubnetForL3 extracts the valid IPv4 subnet from the l3vni, or returns nil.
func v4SubnetForL3(l3vni v1alpha1.L3VNI) *net.IPNet {
	if l3vni.Spec.HostSession == nil {
		return nil
	}
	ipv4 := ptr.Deref(l3vni.Spec.HostSession.LocalCIDR.IPv4, "")
	if ipv4 == "" {
		return nil
	}
	_, ipnet, err := net.ParseCIDR(ipv4)
	if err != nil {
		return nil
	}
	return ipnet
}

// v6SubnetForL3 extracts the valid IPv6 subnet from the l3vni, or returns nil.
func v6SubnetForL3(l3vni v1alpha1.L3VNI) *net.IPNet {
	if l3vni.Spec.HostSession == nil {
		return nil
	}
	ipv6 := ptr.Deref(l3vni.Spec.HostSession.LocalCIDR.IPv6, "")
	if ipv6 == "" {
		return nil
	}
	_, ipnet, err := net.ParseCIDR(ipv6)
	if err != nil {
		return nil
	}
	return ipnet
}

// subnetWithSource holds subnet information for a single IP address family
// alongside the source for logging.
type subnetWithSource struct {
	source string
	subnet *net.IPNet
}

type subnets []subnetWithSource

// sort sorts the vniSubnets in place by network address (starting IP), then by prefix length (longer first), then by
// the source string (prefix length and source string are not relevant for the algorithm itself, but for stable error
// messages).
func (vniSubnets subnets) sort() {
	slices.SortStableFunc(vniSubnets, func(a, b subnetWithSource) int {
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
}

// hasSubnetOverlap takes a vniSubnetsList and checks if any of its subnets overlap. The list must be sorted with sort().
// The algorithm works by: Iterating through sorted subnets once, checking if each subnet overlaps with the next.
func hasSubnetOverlap(vniSubnets subnets) error {
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

func ValidateRouteTargets(vni VNI) error {
	for _, rt := range vni.exportRTs {
		if err := validateRouteTarget(rt); err != nil {
			return err
		}
	}
	for _, rt := range vni.importRTs {
		if err := validateRouteTarget(rt); err != nil {
			return err
		}
	}
	return nil
}

func validateRouteTarget(rt string) error {
	rtParam := strings.Split(rt, ":")
	if len(rtParam) != 2 {
		return fmt.Errorf("RT %q must have one of the following formats: 'ASN:MN' or 'IPv4Address:MN'", rt)
	}

	if isIPv4RouteTarget(rtParam[0]) {
		memberNumber, err := parseMemberNumber(rtParam[1])
		if err != nil {
			return fmt.Errorf("RT format must have A.B.C.D:MN where MN <= 65535: %s", rt)
		}
		if memberNumber > 65535 {
			return fmt.Errorf("RT format must have A.B.C.D:MN where MN <= 65535: %s", rt)
		}
		return nil
	}

	asn, err := strconv.ParseUint(rtParam[0], 10, 32)
	if err != nil {
		return fmt.Errorf("RT format must have ASN:MN %s", rt)
	}

	memberNumber, err := parseMemberNumber(rtParam[1])
	if err != nil {
		return fmt.Errorf("RT format must have ASN:MN where MN is a number: %s", rt)
	}

	if asn <= 65535 && memberNumber > 4294967295 {
		return fmt.Errorf("RT format with 2-byte ASN must have ASN:MN where MN <= 4294967295: %s", rt)
	}
	if asn > 65535 && memberNumber > 65535 {
		return fmt.Errorf("RT format with 4-byte ASN must have ASN:MN where MN <= 65535: %s", rt)
	}

	return nil
}

func parseMemberNumber(value string) (uint64, error) {
	return strconv.ParseUint(value, 10, 64)
}

func isIPv4RouteTarget(value string) bool {
	addr, err := ipfamily.ForAddresses(value)
	return err == nil && addr == ipfamily.IPv4
}
