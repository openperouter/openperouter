// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/ipfamily"
)

var (
	interfaceNameRegexp *regexp.Regexp
	ipv4LikeRegexp      *regexp.Regexp
)

func init() {
	interfaceNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]*$`)
	ipv4LikeRegexp = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
}

// FilterValidL3VNIs validates L3VNIs per-field and returns the valid resources
// alongside per-resource errors.
func FilterValidL3VNIs(l3Vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, error) {
	var valid []v1alpha1.L3VNI
	var allErrors []error
	for _, l3 := range l3Vnis {
		if err := validateL3VNI(l3); err != nil {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VNI", Name: l3.Name,
					Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: err.Error(),
				},
			})
			continue
		}
		valid = append(valid, l3)
	}
	return valid, errors.Join(allErrors...)
}

// validateL3VNI validates a single L3VNI's fields (VRF name, route targets).
func validateL3VNI(l3Vni v1alpha1.L3VNI) error {
	vni := vniFromL3VNI(l3Vni)
	if err := isValidInterfaceName(vni.vrfName); err != nil {
		return fmt.Errorf("invalid vrf name for vni %q, vrf %q: %w", vni.name, vni.vrfName, err)
	}
	if err := ValidateRouteTargets(vni); err != nil {
		return fmt.Errorf("invalid route targets for vni %q: %w", vni.name, err)
	}
	return nil
}

// FilterValidL2VNIs validates L2VNIs per-field and returns the valid resources
// alongside per-resource errors.
func FilterValidL2VNIs(l2Vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	var valid []v1alpha1.L2VNI
	var allErrors []error
	for _, l2 := range l2Vnis {
		if err := validateL2VNI(l2); err != nil {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L2VNI", Name: l2.Name,
					Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: err.Error(),
				},
			})
			continue
		}
		valid = append(valid, l2)
	}
	return valid, errors.Join(allErrors...)
}

// FilterUniqueL3VNIs removes L3VNIs with duplicate VNI numbers. It returns
// the filtered L3VNIs as well as a map containing the unique VNI numbers and the
// name of the corresponding L3VNI.
func FilterUniqueL3VNIs(l3Vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, map[int32]string, error) {
	existingVNIs := map[int32]string{}
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	var validL3VNI []v1alpha1.L3VNI
	for _, l3 := range l3Vnis {
		if existing, ok := existingVNIs[l3.Spec.VNI]; ok {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VNI", Name: l3.Name, Reason: reason,
					Message: fmt.Sprintf("duplicate vni %d:%s", l3.Spec.VNI, existing),
				},
			})
			continue
		}
		existingVNIs[l3.Spec.VNI] = "L3VNI/" + l3.Name
		validL3VNI = append(validL3VNI, l3)
	}

	return validL3VNI, existingVNIs, errors.Join(allErrors...)
}

// FilterUniqueL2VNIs removes L2VNIs with duplicate VNI numbers.
// L2VNIs that collide with an existing VNI or RDAssignedNumber are discarded.
func FilterUniqueL2VNIs(l2Vnis []v1alpha1.L2VNI, existingVNIs map[int32]string) ([]v1alpha1.L2VNI, error) {
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	var validL2 []v1alpha1.L2VNI
	for _, l2 := range l2Vnis {
		if existing, ok := existingVNIs[l2.Spec.VNI]; ok {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L2VNI", Name: l2.Name, Reason: reason,
					Message: fmt.Sprintf("duplicate vni %d:%s", l2.Spec.VNI, existing),
				},
			})
			continue
		}
		existingVNIs[l2.Spec.VNI] = "L2VNI/" + l2.Name
		validL2 = append(validL2, l2)
	}

	return validL2, errors.Join(allErrors...)
}

// validateL2VNI validates a single L2VNI's fields (HostMaster, GatewayIPs).
func validateL2VNI(l2Vni v1alpha1.L2VNI) error {
	if l2Vni.Spec.HostMaster != nil {
		if err := validateHostMaster(l2Vni.Name, l2Vni.Spec.HostMaster); err != nil {
			return err
		}
	}
	if len(l2Vni.Spec.GatewayIPs) > 0 {
		if _, err := ipfamily.ForCIDRStrings(l2Vni.Spec.GatewayIPs...); err != nil {
			return fmt.Errorf("invalid gatewayIPs for vni %q = %v: %w", l2Vni.Name, l2Vni.Spec.GatewayIPs, err)
		}
	}
	return nil
}

// ValidateOverlayResourcesForNodes validates that the VNI / VPN information as a whole is correct, per Node.
func ValidateOverlayResourcesForNodes(nodes []corev1.Node, l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI,
	l3vpns []v1alpha1.L3VPN) error {
	var errs []error
	for _, node := range nodes {
		if err := validateOverlayResourcesForNode(node, l2vnis, l3vnis, l3vpns); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func validateOverlayResourcesForNode(node corev1.Node, l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI,
	l3vpns []v1alpha1.L3VPN) error {
	filteredL3VNIs, err := filter.L3VNIsForNode(&node, l3vnis)
	if err != nil {
		return fmt.Errorf("failed to filter l3vnis for node %q: %w", node.Name, err)
	}

	filteredL3VPNs, err := filter.L3VPNsForNode(&node, l3vpns)
	if err != nil {
		return fmt.Errorf("failed to filter l3vnis for node %q: %w", node.Name, err)
	}

	filteredL2VNIs, err := filter.L2VNIsForNode(&node, l2vnis)
	if err != nil {
		return fmt.Errorf("failed to filter l2vnis for node %q: %w", node.Name, err)
	}

	validL3VNIs, err := FilterValidL3VNIs(filteredL3VNIs)
	if err != nil {
		return fmt.Errorf("failed to validate l3vnis for node %q: %w", node.Name, err)
	}

	validL3VPNs, err := FilterValidL3VPNs(filteredL3VPNs)
	if err != nil {
		return fmt.Errorf("failed to validate l3vpns for node %q: %w", node.Name, err)
	}

	validL2VNIs, err := FilterValidL2VNIs(filteredL2VNIs)
	if err != nil {
		return fmt.Errorf("failed to validate l2vnis for node %q: %w", node.Name, err)
	}

	var vnis map[int32]string
	validL3VNIs, vnis, err = FilterUniqueL3VNIs(validL3VNIs)
	if err != nil {
		return fmt.Errorf("duplicate L3VNIs found for node %q: %w", node.Name, err)
	}

	var rdAssignedNumbers map[int32]string
	validL3VPNs, rdAssignedNumbers, err = FilterUniqueL3VPNs(validL3VPNs)
	if err != nil {
		return fmt.Errorf("duplicate L3VPNs found for node %q: %w", node.Name, err)
	}
	maps.Copy(vnis, rdAssignedNumbers)

	validL2VNIs, err = FilterUniqueL2VNIs(validL2VNIs, vnis)
	if err != nil {
		return fmt.Errorf("duplicate VNIs found in L2VNIs for node %q: %w", node.Name, err)
	}

	validL3VNIs, err = FilterUniqueVRFsForL3VNIs(validL3VNIs)
	if err != nil {
		return fmt.Errorf("duplicate L3VNI VRFs found for node %q: %w", node.Name, err)
	}

	validL3VPNs, err = FilterUniqueVRFsForL3VPNs(validL3VPNs)
	if err != nil {
		return fmt.Errorf("duplicate L3VPN VRFs found for node %q: %w", node.Name, err)
	}

	_, _, _, err = FilterValidVRFSubnets(validL3VNIs, validL3VPNs, validL2VNIs)
	if err != nil {
		return fmt.Errorf("subnet overlaps found in VRFs for node %q: %w", node.Name, err)
	}

	return nil
}

// FilterUniqueVRFsForL3VNIs checks VRF uniqueness among L3VNIs and returns the valid
// L3VNIs alongside per-resource errors for duplicates.
func FilterUniqueVRFsForL3VNIs(l3Vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, error) {
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	vrfToVNI := map[string]types.NamespacedName{}
	var valid []v1alpha1.L3VNI
	for _, l3Vni := range l3Vnis {
		namespaceName := types.NamespacedName{Namespace: l3Vni.Namespace, Name: l3Vni.Name}
		existing, ok := vrfToVNI[l3Vni.Spec.VRF]
		if ok {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VNI", Name: l3Vni.Name, Reason: reason,
					Message: fmt.Sprintf("more than one L3VNI detected in VRF %q: %q already exists", l3Vni.Spec.VRF, existing),
				},
			})
			continue
		}
		vrfToVNI[l3Vni.Spec.VRF] = namespaceName
		valid = append(valid, l3Vni)
	}

	return valid, errors.Join(allErrors...)
}

// FilterValidVRFSubnets checks for subnet overlaps per VRF and returns valid
// L3VNIs, valid L3VPNs, valid L2VNIs, and per-resource errors. Resources in VRFs with
// overlapping subnets are excluded.
func FilterValidVRFSubnets(l3Vnis []v1alpha1.L3VNI, l3Vpns []v1alpha1.L3VPN,
	l2Vnis []v1alpha1.L2VNI) ([]v1alpha1.L3VNI, []v1alpha1.L3VPN, []v1alpha1.L2VNI, error) {
	reason := v1alpha1.FailedResourceReasonValidationFailed
	failedVRFs := ValidateVRFSubnets(l2Vnis, l3Vnis, l3Vpns)
	if len(failedVRFs) == 0 {
		return l3Vnis, l3Vpns, l2Vnis, nil
	}

	var allErrors []error
	var resultL3VNI []v1alpha1.L3VNI
	for _, l3 := range l3Vnis {
		if err, failed := failedVRFs[l3.Spec.VRF]; failed {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VNI", Name: l3.Name, Reason: reason, Message: err.Error(),
				},
			})
			continue
		}
		resultL3VNI = append(resultL3VNI, l3)
	}

	var resultL3VPN []v1alpha1.L3VPN
	for _, l3 := range l3Vpns {
		if err, failed := failedVRFs[l3.Spec.VRF]; failed {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VPN", Name: l3.Name, Reason: reason, Message: err.Error(),
				},
			})
			continue
		}
		resultL3VPN = append(resultL3VPN, l3)
	}

	vrfMap := createVRFMap(l3Vnis, l3Vpns)
	var resultL2 []v1alpha1.L2VNI
	for _, l2 := range l2Vnis {
		vrfName := resolveVRFForL2VNI(l2, vrfMap)
		if vrfName != "" && failedVRFs[vrfName] != nil {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L2VNI", Name: l2.Name, Reason: reason, Message: failedVRFs[vrfName].Error(),
				},
			})
			continue
		}
		resultL2 = append(resultL2, l2)
	}

	return resultL3VNI, resultL3VPN, resultL2, errors.Join(allErrors...)
}

// ValidateVRFSubnets checks for subnet overlaps per VRF and returns a map of
// VRF name to error for each VRF that has overlapping subnets.
func ValidateVRFSubnets(l2Vnis []v1alpha1.L2VNI, l3Vnis []v1alpha1.L3VNI, l3Vpns []v1alpha1.L3VPN) map[string]error {
	vrfMap := createVRFMap(l3Vnis, l3Vpns)
	v4SubnetsForVRF := map[string]subnets{}
	v6SubnetsForVRF := map[string]subnets{}
	for _, l2vni := range l2Vnis {
		vrfName := resolveVRFForL2VNI(l2vni, vrfMap)
		if vrfName == "" {
			continue
		}
		source := fmt.Sprintf("L2VNI %s", types.NamespacedName{Namespace: l2vni.Namespace, Name: l2vni.Name})
		if subnet := v4SubnetForL2(l2vni); subnet != nil {
			v4SubnetsForVRF[vrfName] = append(v4SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
		if subnet := v6SubnetForL2(l2vni); subnet != nil {
			v6SubnetsForVRF[vrfName] = append(v6SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
	}
	for _, l3vni := range l3Vnis {
		vrfName := l3vni.Spec.VRF
		source := fmt.Sprintf("L3VNI %s", types.NamespacedName{Namespace: l3vni.Namespace, Name: l3vni.Name})
		if subnet := v4SubnetForL3(l3vni); subnet != nil {
			v4SubnetsForVRF[vrfName] = append(v4SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
		if subnet := v6SubnetForL3(l3vni); subnet != nil {
			v6SubnetsForVRF[vrfName] = append(v6SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
	}
	for _, l3vpn := range l3Vpns {
		vrfName := l3vpn.Spec.VRF
		source := fmt.Sprintf("L3VPN %s", types.NamespacedName{Namespace: l3vpn.Namespace, Name: l3vpn.Name})
		if subnet := v4SubnetForL3VPN(l3vpn); subnet != nil {
			v4SubnetsForVRF[vrfName] = append(v4SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
		if subnet := v6SubnetForL3VPN(l3vpn); subnet != nil {
			v6SubnetsForVRF[vrfName] = append(v6SubnetsForVRF[vrfName], subnetWithSource{source, subnet})
		}
	}

	failedVRFs := map[string]error{}
	for vrf, subnetList := range v4SubnetsForVRF {
		subnetList.sort()
		if err := hasSubnetOverlap(subnetList); err != nil {
			failedVRFs[vrf] = fmt.Errorf("subnet overlap in VRF %q: %w", vrf, err)
		}
	}
	for vrf, subnetList := range v6SubnetsForVRF {
		subnetList.sort()
		if err := hasSubnetOverlap(subnetList); err != nil {
			failedVRFs[vrf] = fmt.Errorf("subnet overlap in VRF %q: %w", vrf, err)
		}
	}
	return failedVRFs
}

// vni holds VNI validation data
type VNI struct {
	name      string
	vni       uint32
	vrfName   string
	exportRTs []string
	importRTs []string
}

func vniFromL3VNI(l3vni v1alpha1.L3VNI) VNI {
	return VNI{
		name:      l3vni.Name,
		vni:       uint32(l3vni.Spec.VNI),
		vrfName:   l3vni.Spec.VRF,
		exportRTs: convertRTsToSliceOfStrings(l3vni.Spec.ExportRTs),
		importRTs: convertRTsToSliceOfStrings(l3vni.Spec.ImportRTs),
	}
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

func hasRoutingDomain(l2vni v1alpha1.L2VNI) bool {
	return l2vni.Spec.RoutingDomain != nil
}

func createVRFMap(l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN) map[string]string {
	m := make(map[string]string, len(l3vnis)+len(l3vpns))
	for _, l3 := range l3vnis {
		m[v1alpha1.RoutingDomainTypeL3VNI+"/"+l3.Name] = l3.Spec.VRF
	}
	for _, vpn := range l3vpns {
		m[v1alpha1.RoutingDomainTypeL3VPN+"/"+vpn.Name] = vpn.Spec.VRF
	}
	return m
}

func resolveVRFForL2VNI(l2vni v1alpha1.L2VNI, vrfMap map[string]string) string {
	if !hasRoutingDomain(l2vni) {
		return ""
	}
	if l2vni.Spec.RoutingDomain.L3VNI != nil {
		return vrfMap[v1alpha1.RoutingDomainTypeL3VNI+"/"+l2vni.Spec.RoutingDomain.L3VNI.Name]
	}
	if l2vni.Spec.RoutingDomain.L3VPN != nil {
		return vrfMap[v1alpha1.RoutingDomainTypeL3VPN+"/"+l2vni.Spec.RoutingDomain.L3VPN.Name]
	}
	return ""
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
	for _, subnet := range l2vni.Spec.GatewayIPs {
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
	for _, subnet := range l2vni.Spec.GatewayIPs {
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

	// Catch values that look like an IPv4 address but failed validation (e.g. 999.1.2.3).
	if ipv4LikeRegexp.MatchString(rtParam[0]) {
		return fmt.Errorf("RT format must have A.B.C.D:MN where A.B.C.D is a valid IPv4 address: %s", rt)
	}

	asn, err := strconv.ParseUint(rtParam[0], 10, 32)
	if err != nil {
		return fmt.Errorf("RT format must have ASN:MN: %s", rt)
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
