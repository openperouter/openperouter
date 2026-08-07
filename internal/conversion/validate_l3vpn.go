// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/filter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// FilterValidL3VPNs validates L3VPNs per-field and returns the valid resources
// alongside per-resource errors.
func FilterValidL3VPNs(l3vpns []v1alpha1.L3VPN) ([]v1alpha1.L3VPN, error) {
	var valid []v1alpha1.L3VPN
	var allErrors []error
	for _, l3vpn := range l3vpns {
		if err := validateL3VPN(l3vpn); err != nil {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VPN", Name: l3vpn.Name,
					Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: err.Error(),
				},
			})
			continue
		}
		valid = append(valid, l3vpn)
	}
	return valid, errors.Join(allErrors...)
}

// FilterUniqueL3VPNs removes L3VPNs with duplicate RD assigned numbers. It returns
// the filtered L3VPNs as well as a map containing the unique RD assigned numbers
// and the name of the corresponding L3VPN.
// L3VPNs that collide with an existing VNI from FilterUniqueL3VNIs are
// discarded, too.
func FilterUniqueL3VPNs(l3Vpns []v1alpha1.L3VPN, existingVNIs map[int32]string,
) ([]v1alpha1.L3VPN, map[int32]string, error) {
	if existingVNIs == nil {
		existingVNIs = map[int32]string{}
	}
	existingRDAssignedNumber := map[int32]string{}
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	var validL3VPN []v1alpha1.L3VPN
	for _, l3 := range l3Vpns {
		if existing, duplicateFound := existingRDAssignedNumber[l3.Spec.RDAssignedNumber]; duplicateFound {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VPN", Name: l3.Name, Reason: reason,
					Message: fmt.Sprintf("duplicate rdAssignedNumber %d:%s", l3.Spec.RDAssignedNumber, existing),
				},
			})
			continue
		}
		if existing, duplicateFound := existingVNIs[l3.Spec.RDAssignedNumber]; duplicateFound {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VPN", Name: l3.Name, Reason: reason,
					Message: fmt.Sprintf("duplicate rdAssignedNumber %d cannot be the same as VNI in L3VNI: %s",
						l3.Spec.RDAssignedNumber, existing),
				},
			})
			continue
		}
		existingRDAssignedNumber[l3.Spec.RDAssignedNumber] = "L3VPN/" + l3.Name
		validL3VPN = append(validL3VPN, l3)
	}

	return validL3VPN, existingRDAssignedNumber, errors.Join(allErrors...)
}

// ValidateSRv6ForNodes returns an error if, for any node, the L3VPN resources are present without
// a valid SRV6 spec on the underlay resource for that node.
func ValidateSRv6ForNodes(nodes []corev1.Node, underlays []v1alpha1.Underlay, l3vpns []v1alpha1.L3VPN) error {
	for _, node := range nodes {
		filteredUnderlays, err := filter.UnderlaysForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}

		filteredL3VPNs, err := filter.L3VPNsForNode(&node, l3vpns)
		if err != nil {
			return fmt.Errorf("failed to filter l3vpns for node %q: %w", node.Name, err)
		}

		if HasMissingSRv6ForL3VPNs(filteredUnderlays, filteredL3VPNs) {
			return MissingSRv6ForL3VPNErrors(filteredL3VPNs, &node)
		}
	}
	return nil
}

// HasMissingSRv6ForL3VPNs returns true if any L3VPNs are configured with missing underlay SRv6 configuration.
func HasMissingSRv6ForL3VPNs(underlays []v1alpha1.Underlay, l3vpns []v1alpha1.L3VPN) bool {
	if len(l3vpns) == 0 {
		return false
	}
	if len(underlays) == 0 {
		return true
	}
	if underlays[0].Spec.SRV6 == nil {
		return true
	}
	return false
}

// MissingSRv6ForL3VPNErrors adds errors to all l3vpns about missing underlay SRv6 configuration.
func MissingSRv6ForL3VPNErrors(l3vpns []v1alpha1.L3VPN, node *corev1.Node) error {
	errs := make([]error, 0, len(l3vpns))

	nodeStr := ""
	if node != nil {
		nodeStr = fmt.Sprintf(" on node %q", node.Name)
	}

	for _, l3vpn := range l3vpns {
		errs = append(errs, &openpeerrors.ResourceError{
			Obj: v1alpha1.FailedResource{
				Kind:   v1alpha1.FailedResourceKind("L3VPN"),
				Name:   l3vpn.Name,
				Reason: v1alpha1.FailedResourceReasonValidationFailed,
				Message: "cannot specify L3VPN configuration without an underlay with SRV6 configuration" +
					nodeStr,
			},
		})
	}

	return errors.Join(errs...)
}

// validateL3VPN validates a single L3VPN's fields (VRF name, route targets).
func validateL3VPN(l3Vni v1alpha1.L3VPN) error {
	vni := vniFromL3VPN(l3Vni)
	if err := isValidInterfaceName(vni.vrfName); err != nil {
		return fmt.Errorf("invalid vrf name for vpn %q, vrf %q: %w", vni.name, vni.vrfName, err)
	}
	if len(vni.importRTs) == 0 {
		return fmt.Errorf("invalid import route targets for vpn %q: import route targets cannot be empty",
			vni.name)
	}
	if err := ValidateRouteTargets(vni); err != nil {
		return fmt.Errorf("invalid route targets for vpn %q: %w", vni.name, err)
	}
	return nil
}

// vniFromL3VPN converts an L3VPN to a vni.
// We set vni to the value of RDAssignedNumber - this is analogous to EVPN which uses the VNI value for interfaces
// and which builds RTs implicitly based on the VNI value.
// In the API to host conversion, for L3VPN we use the RDAssignedNumber as the numeric identifier for interfaces.
// In the API to FRR conversion, for L3VPN we use the RDAssignedNumber to create exportRTs.
func vniFromL3VPN(l3vpn v1alpha1.L3VPN) VNI {
	return VNI{
		name:      l3vpn.Name,
		vni:       uint32(l3vpn.Spec.RDAssignedNumber),
		vrfName:   l3vpn.Spec.VRF,
		exportRTs: convertRTsToSliceOfStrings(l3vpn.Spec.ExportRTs),
		importRTs: convertRTsToSliceOfStrings(l3vpn.Spec.ImportRTs),
	}
}

// v4SubnetForL3VPN extracts the valid IPv4 subnet from the l3vni, or returns nil.
func v4SubnetForL3VPN(l3vni v1alpha1.L3VPN) *net.IPNet {
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

// v6SubnetForL3VPN extracts the valid IPv6 subnet from the l3vni, or returns nil.
func v6SubnetForL3VPN(l3vni v1alpha1.L3VPN) *net.IPNet {
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
