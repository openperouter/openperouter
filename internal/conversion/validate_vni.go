// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"bytes"
	"errors"
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
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/hostnetwork"
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
	if err := isValidInterfaceName(l3Vni.Spec.VRF); err != nil {
		return fmt.Errorf("invalid vrf name for vni %q, vrf %q: %w", l3Vni.Name, l3Vni.Spec.VRF, err)
	}
	if err := ValidateRouteTargets(
		convertRTsToSliceOfStrings(l3Vni.Spec.ExportRTs),
		convertRTsToSliceOfStrings(l3Vni.Spec.ImportRTs),
	); err != nil {
		return fmt.Errorf("invalid route targets for vni %q: %w", l3Vni.Name, err)
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
// the filtered L3VNIs as well as a map containing the unique VNI numbers
// and the name of the corresponding L3VNI for L2VNI conflict checks.
func FilterUniqueL3VNIs(l3Vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, map[int32]string, error) {
	existingVNIs := map[int32]string{}
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	var validL3VNI []v1alpha1.L3VNI
	for _, l3 := range l3Vnis {
		if existing, duplicateFound := existingVNIs[l3.Spec.VNI]; duplicateFound {
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
// L2VNIs that collide with an existing L3VNI VNI number are discarded.
func FilterUniqueL2VNIs(
	l2Vnis []v1alpha1.L2VNI,
	allocatedVNIs map[int32]string,
) ([]v1alpha1.L2VNI, error) {
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	var validL2 []v1alpha1.L2VNI
	for _, l2 := range l2Vnis {
		if existing, duplicateFound := allocatedVNIs[l2.Spec.VNI]; duplicateFound {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L2VNI", Name: l2.Name, Reason: reason,
					Message: fmt.Sprintf("duplicate vni %d:%s", l2.Spec.VNI, existing),
				},
			})
			continue
		}
		allocatedVNIs[l2.Spec.VNI] = "L2VNI/" + l2.Name
		validL2 = append(validL2, l2)
	}

	return validL2, errors.Join(allErrors...)
}

// FilterUniqueL3VNIRDNumbers removes L3VNIs with duplicate configured RD assigned numbers.
func FilterUniqueL3VNIRDNumbers(vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, error) {
	owners := map[int32]string{}
	valid := []v1alpha1.L3VNI{}
	var errs []error
	for _, resource := range vnis {
		if resource.Spec.RDAssignedNumber == nil {
			valid = append(valid, resource)
			continue
		}
		number := *resource.Spec.RDAssignedNumber
		identity := "L3VNI/" + resource.Name
		if existing, found := owners[number]; found {
			errs = append(errs, &openpeerrors.ResourceError{Obj: v1alpha1.FailedResource{
				Kind: "L3VNI", Name: resource.Name, Reason: v1alpha1.FailedResourceReasonValidationFailed,
				Message: fmt.Sprintf("duplicate configured rdAssignedNumber %d: %s conflicts with %s", number, identity, existing),
			}})
			continue
		}
		owners[number] = identity
		valid = append(valid, resource)
	}
	return valid, errors.Join(errs...)
}

// FilterUniqueL2VNIRDNumbers removes L2VNIs with duplicate configured RD assigned numbers.
func FilterUniqueL2VNIRDNumbers(vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	owners := map[int32]string{}
	valid := []v1alpha1.L2VNI{}
	var errs []error
	for _, resource := range vnis {
		if resource.Spec.RDAssignedNumber == nil {
			valid = append(valid, resource)
			continue
		}
		number := *resource.Spec.RDAssignedNumber
		identity := "L2VNI/" + resource.Name
		if existing, found := owners[number]; found {
			errs = append(errs, &openpeerrors.ResourceError{Obj: v1alpha1.FailedResource{
				Kind: "L2VNI", Name: resource.Name, Reason: v1alpha1.FailedResourceReasonValidationFailed,
				Message: fmt.Sprintf("duplicate configured rdAssignedNumber %d: %s conflicts with %s", number, identity, existing),
			}})
			continue
		}
		owners[number] = identity
		valid = append(valid, resource)
	}
	return valid, errors.Join(errs...)
}

// validateL2VNI validates a single L2VNI's fields (HostMaster, GatewayIPs, route targets).
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
	if err := ValidateRouteTargets(
		convertRTsToSliceOfStrings(l2Vni.Spec.ExportRTs),
		convertRTsToSliceOfStrings(l2Vni.Spec.ImportRTs),
	); err != nil {
		return fmt.Errorf("invalid route targets for vni %q: %w", l2Vni.Name, err)
	}
	return nil
}

// FilterValidL2Trunks validates L2Trunks per-field and returns the valid
// resources alongside per-resource errors.
func FilterValidL2Trunks(trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	var valid []v1alpha1.L2Trunk
	var allErrors []error
	for _, trunk := range trunks {
		if err := validateL2Trunk(trunk); err != nil {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL2Trunk, Name: trunk.Name,
					Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: err.Error(),
				},
			})
			continue
		}
		valid = append(valid, trunk)
	}
	valid, err := filterL2TrunksSRIOVUniquePF(valid)
	return valid, errors.Join(append(allErrors, err)...)
}

func validateL2Trunk(trunk v1alpha1.L2Trunk) error {
	switch trunk.Spec.Type {
	case v1alpha1.L2TrunkTypeSRIOV:
		return validateL2TrunkSRIOV(trunk.Spec)
	case v1alpha1.L2TrunkTypeHost:
		if trunk.Spec.HostMaster == nil {
			return nil
		}
		if err := validateHostMaster(trunk.Name, trunk.Spec.HostMaster); err != nil {
			return err
		}
		return validateExternalHostMaster(trunk.Name, trunk.Spec.HostMaster)
	default:
		return fmt.Errorf("unsupported type %q for L2Trunk %q", trunk.Spec.Type, trunk.Name)
	}
}

func validateExternalHostMaster(owner string, hostMaster *v1alpha1.HostMaster) error {
	var lifecycle v1alpha1.BridgeLifecycle
	switch hostMaster.Type {
	case v1alpha1.LinuxBridge:
		if hostMaster.LinuxBridge != nil {
			lifecycle = hostMaster.LinuxBridge.Lifecycle
		}
	case v1alpha1.OVSBridge:
		if hostMaster.OVSBridge != nil {
			lifecycle = hostMaster.OVSBridge.Lifecycle
		}
	default:
		return fmt.Errorf("invalid host master type %q for L2Trunk %q", hostMaster.Type, owner)
	}
	if lifecycle != v1alpha1.BridgeLifecycleExternal {
		return fmt.Errorf("L2Trunk %q requires an External host master, got lifecycle %q", owner, lifecycle)
	}
	return nil
}

// FilterUniqueTrunkIDs removes trunks whose derived device identifier collides
// with another trunk's. The identifier is a hash of namespace/name, so a
// collision would silently merge two tenants onto one bridge.
func FilterUniqueTrunkIDs(trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	owners := map[string]string{}
	var valid []v1alpha1.L2Trunk
	var allErrors []error
	for _, trunk := range trunks {
		id := hostnetwork.TrunkID(trunk.Namespace, trunk.Name)
		identity := types.NamespacedName{Namespace: trunk.Namespace, Name: trunk.Name}.String()
		if existing, found := owners[id]; found {
			allErrors = append(allErrors, &openpeerrors.ResourceError{Obj: v1alpha1.FailedResource{
				Kind: openpeerrors.KindL2Trunk, Name: trunk.Name,
				Reason:  v1alpha1.FailedResourceReasonValidationFailed,
				Message: fmt.Sprintf("trunk id %q of %s collides with %s", id, identity, existing),
			}})
			continue
		}
		owners[id] = identity
		valid = append(valid, trunk)
	}
	return valid, errors.Join(allErrors...)
}

// FilterUniqueL2VNITrunkTags removes every L2VNI that reuses a VLAN tag on the
// same trunk, reporting all the resources involved.
func FilterUniqueL2VNITrunkTags(l2vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	type trunkTag struct {
		key trunkKey
		tag int32
	}
	return rejectConflictingGroups(l2vnis,
		func(l2vni v1alpha1.L2VNI) (trunkTag, bool) {
			key, trunked := trunkKeyForL2VNI(l2vni)
			if !trunked {
				return trunkTag{}, false
			}
			return trunkTag{key: key, tag: l2vni.Spec.Trunk.VLAN.Tag}, true
		},
		func(group trunkTag, members []v1alpha1.L2VNI) string {
			names := make([]string, 0, len(members))
			for _, member := range members {
				names = append(names, member.Name)
			}
			return fmt.Sprintf("duplicate vlan tag %d on L2Trunk %q: %s",
				group.tag, group.key.name, strings.Join(names, ", "))
		})
}

// FilterUniqueTrunkTagsAcrossHostMasters rejects the same VLAN tag being used
// by trunks that share a host master. Trunks with no host master attach to no
// shared bridge, so they are exempt.
func FilterUniqueTrunkTagsAcrossHostMasters(l2vnis []v1alpha1.L2VNI, trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2VNI, error) {
	trunkByKey := map[trunkKey]v1alpha1.L2Trunk{}
	for _, trunk := range trunks {
		trunkByKey[trunkKey{namespace: trunk.Namespace, name: trunk.Name}] = trunk
	}

	type hostMasterTag struct {
		hostMaster string
		tag        int32
	}
	return rejectConflictingGroups(l2vnis,
		func(l2vni v1alpha1.L2VNI) (hostMasterTag, bool) {
			key, trunked := trunkKeyForL2VNI(l2vni)
			if !trunked {
				return hostMasterTag{}, false
			}
			trunk, found := trunkByKey[key]
			if !found {
				return hostMasterTag{}, false
			}
			hostMaster := hostMasterIdentity(trunk.Spec.HostMaster)
			if hostMaster == "" {
				return hostMasterTag{}, false
			}
			return hostMasterTag{hostMaster: hostMaster, tag: l2vni.Spec.Trunk.VLAN.Tag}, true
		},
		func(group hostMasterTag, members []v1alpha1.L2VNI) string {
			identities := make([]string, 0, len(members))
			for _, member := range members {
				identities = append(identities, fmt.Sprintf("%s on L2Trunk %q", member.Name, member.Spec.Trunk.Name))
			}
			return fmt.Sprintf("duplicate vlan tag %d across L2Trunks sharing host master %q: %s",
				group.tag, group.hostMaster, strings.Join(identities, ", "))
		})
}

// FilterTrunkSettingsAgreement rejects L2VNIs on one trunk that disagree on
// the effective vxlanPort or underlayAddressFamily: they share one VXLAN
// device, so there is only one possible setting.
func FilterTrunkSettingsAgreement(l2vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	return rejectConflictingGroups(l2vnis, trunkKeyForL2VNI,
		func(key trunkKey, members []v1alpha1.L2VNI) string {
			expected := effectiveVXLanSettings(members[0])
			for i := 1; i < len(members); i++ {
				actual := effectiveVXLanSettings(members[i])
				if actual == expected {
					continue
				}
				return fmt.Sprintf("L2VNIs on L2Trunk %q disagree on vxlanPort or underlayAddressFamily: "+
					"%s uses (port %d, family %q), %s uses (port %d, family %q)",
					key.name, members[0].Name, expected.port, expected.family,
					members[i].Name, actual.port, actual.family)
			}
			return ""
		})
}

// ValidateOverlayResourcesForNodes validates that the VNI / VPN information as a whole is correct, per Node.
func ValidateOverlayResourcesForNodes(nodes []corev1.Node, l2vnis []v1alpha1.L2VNI, l2trunks []v1alpha1.L2Trunk,
	l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN, passthroughs []v1alpha1.L3Passthrough) error {
	var errs []error
	for _, node := range nodes {
		if err := validateOverlayResourcesForNode(node, l2vnis, l2trunks, l3vnis, l3vpns, passthroughs); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func validateOverlayResourcesForNode(node corev1.Node, l2vnis []v1alpha1.L2VNI, l2trunks []v1alpha1.L2Trunk,
	l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN, passthroughs []v1alpha1.L3Passthrough) error {
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

	filteredL2Trunks, err := filter.L2TrunksForNode(&node, l2trunks)
	if err != nil {
		return fmt.Errorf("failed to filter l2trunks for node %q: %w", node.Name, err)
	}

	filteredPassthroughs, err := filter.L3PassthroughsForNode(&node, passthroughs)
	if err != nil {
		return fmt.Errorf("failed to filter passthroughs for node %q: %w", node.Name, err)
	}

	_, err = FilterOverlayResources(OverlayResources{
		L3VNIs:        filteredL3VNIs,
		L3VPNs:        filteredL3VPNs,
		L2VNIs:        filteredL2VNIs,
		L2Trunks:      filteredL2Trunks,
		L3Passthrough: filteredPassthroughs,
	})
	if err != nil {
		return fmt.Errorf("invalid overlay resources for node %q: %w", node.Name, err)
	}
	return nil
}

// FilterUniqueVRFsForL3VNIs checks VRF uniqueness among L3VNIs and returns the valid
// L3VNIs alongside per-resource errors for duplicates. Collects and returns
// VRFs used by L3VNIs for duplicate detection by FilterUniqueVRFsForL3VPNs.
func FilterUniqueVRFsForL3VNIs(l3vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, map[string]types.NamespacedName, error) {
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	vrfToVNI := map[string]types.NamespacedName{}
	var validL3VNIs []v1alpha1.L3VNI
	for _, l3vni := range l3vnis {
		namespaceName := types.NamespacedName{Namespace: l3vni.Namespace, Name: l3vni.Name}
		if existing, duplicateFound := vrfToVNI[l3vni.Spec.VRF]; duplicateFound {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VNI", Name: l3vni.Name, Reason: reason,
					Message: fmt.Sprintf("more than one L3VNI detected in VRF %q: %q already exists", l3vni.Spec.VRF, existing),
				},
			})
			continue
		}
		vrfToVNI[l3vni.Spec.VRF] = namespaceName
		validL3VNIs = append(validL3VNIs, l3vni)
	}

	return validL3VNIs, vrfToVNI, errors.Join(allErrors...)
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
	ipv4 := ipfamily.CIDRForFamily(l3vni.Spec.HostSession.LocalCIDRs, ipfamily.IPv4)
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
	ipv6 := ipfamily.CIDRForFamily(l3vni.Spec.HostSession.LocalCIDRs, ipfamily.IPv6)
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

func ValidateRouteTargets(exportRTs, importRTs []string) error {
	for _, rt := range exportRTs {
		if err := validateRouteTarget(rt); err != nil {
			return err
		}
	}
	for _, rt := range importRTs {
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

type trunkKey struct {
	namespace string
	name      string
}

func trunkKeyForL2VNI(l2vni v1alpha1.L2VNI) (trunkKey, bool) {
	if l2vni.Spec.Trunk == nil {
		return trunkKey{}, false
	}
	return trunkKey{namespace: l2vni.Namespace, name: l2vni.Spec.Trunk.Name}, true
}

// hostMasterIdentity identifies the host bridge a trunk attaches to, so trunks
// that share one are compared against each other.
func hostMasterIdentity(hostMaster *v1alpha1.HostMaster) string {
	if hostMaster == nil {
		return ""
	}
	switch hostMaster.Type {
	case v1alpha1.LinuxBridge:
		if hostMaster.LinuxBridge != nil {
			return hostMaster.Type + "/" + ptr.Deref(hostMaster.LinuxBridge.Name, "")
		}
	case v1alpha1.OVSBridge:
		if hostMaster.OVSBridge != nil {
			return hostMaster.Type + "/" + ptr.Deref(hostMaster.OVSBridge.Name, "")
		}
	}
	return ""
}

type vxlanSettings struct {
	port   int32
	family string
}

func effectiveVXLanSettings(l2vni v1alpha1.L2VNI) vxlanSettings {
	return vxlanSettings{
		port:   *vxlanPort(l2vni.Spec.VXLanPort),
		family: ptr.Deref(l2vni.Spec.UnderlayAddressFamily, ""),
	}
}

// rejectConflictingGroups groups the L2VNIs by key and rejects every member of
// a group for which conflictMessage reports a conflict. A group with fewer than
// two members cannot conflict. conflictMessage returns an empty string when the
// group is consistent.
func rejectConflictingGroups[K comparable](
	l2vnis []v1alpha1.L2VNI,
	groupKey func(v1alpha1.L2VNI) (K, bool),
	conflictMessage func(key K, members []v1alpha1.L2VNI) string,
) ([]v1alpha1.L2VNI, error) {
	groups := map[K][]v1alpha1.L2VNI{}
	var order []K
	for _, l2vni := range l2vnis {
		key, grouped := groupKey(l2vni)
		if !grouped {
			continue
		}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], l2vni)
	}

	invalid := map[types.NamespacedName]struct{}{}
	var allErrors []error
	for _, key := range order {
		members := groups[key]
		if len(members) < 2 {
			continue
		}
		message := conflictMessage(key, members)
		if message == "" {
			continue
		}
		for _, member := range members {
			invalid[types.NamespacedName{Namespace: member.Namespace, Name: member.Name}] = struct{}{}
			allErrors = append(allErrors, &openpeerrors.ResourceError{Obj: v1alpha1.FailedResource{
				Kind:    openpeerrors.KindL2VNI,
				Name:    member.Name,
				Reason:  v1alpha1.FailedResourceReasonValidationFailed,
				Message: message,
			}})
		}
	}
	return rejectL2VNIs(l2vnis, invalid), errors.Join(allErrors...)
}

func rejectL2VNIs(l2vnis []v1alpha1.L2VNI, invalid map[types.NamespacedName]struct{}) []v1alpha1.L2VNI {
	if len(invalid) == 0 {
		return l2vnis
	}
	valid := make([]v1alpha1.L2VNI, 0, len(l2vnis)-len(invalid))
	for _, l2vni := range l2vnis {
		if _, rejected := invalid[types.NamespacedName{Namespace: l2vni.Namespace, Name: l2vni.Name}]; rejected {
			continue
		}
		valid = append(valid, l2vni)
	}
	return valid
}
