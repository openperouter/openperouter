// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/filter"
)

// ValidateL2TrunksForNodes validates, for each node, the L2Trunks applying to
// it.
func ValidateL2TrunksForNodes(nodes []corev1.Node, l2trunks []v1alpha1.L2Trunk) error {
	var errs []error
	for _, node := range nodes {
		filtered, err := filter.L2TrunksForNode(&node, l2trunks)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to filter l2trunks for node %q: %w", node.Name, err))
			continue
		}
		if _, err := FilterValidL2Trunks(filtered); err != nil {
			errs = append(errs, fmt.Errorf("invalid l2trunks for node %q: %w", node.Name, err))
		}
	}
	return errors.Join(errs...)
}

// FilterValidL2Trunks validates the L2Trunks of a node and returns the valid
// ones alongside per-resource errors. The PF and VF of an SR-IOV trunk are
// node-local, so two trunks applying to the same node cannot select the same
// VF. The first trunk selecting a VF keeps it.
func FilterValidL2Trunks(l2trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	if len(l2trunks) == 0 {
		return l2trunks, nil
	}

	type vfKey struct {
		pfName  string
		vfIndex int32
	}

	vfOwners := map[vfKey]string{}
	valid := make([]v1alpha1.L2Trunk, 0, len(l2trunks))
	var errs []error
	for _, trunk := range l2trunks {
		if err := validateL2Trunk(trunk); err != nil {
			errs = append(errs, l2TrunkValidationError(trunk.Name, err.Error()))
			continue
		}
		if trunk.Spec.Type != v1alpha1.L2TrunkTypeSRIOV {
			valid = append(valid, trunk)
			continue
		}

		key := vfKey{pfName: trunk.Spec.SRIOV.PFName, vfIndex: *trunk.Spec.SRIOV.VFIndex}
		if owner, ok := vfOwners[key]; ok {
			errs = append(errs, l2TrunkValidationError(trunk.Name,
				fmt.Sprintf("VF %d of PF %s is already used by L2Trunk %s", key.vfIndex, key.pfName, owner)))
			continue
		}
		vfOwners[key] = trunk.Name
		valid = append(valid, trunk)
	}
	return valid, errors.Join(errs...)
}

// FilterUniqueTrunkVLANs removes the L2VNIs that reuse the VLAN tag of
// another L2VNI on the same trunk. Different trunks can carry the same tag.
func FilterUniqueTrunkVLANs(l2vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	if len(l2vnis) == 0 {
		return l2vnis, nil
	}

	type trunkVLAN struct {
		trunk string
		tag   int32
	}

	owners := map[trunkVLAN]string{}
	valid := make([]v1alpha1.L2VNI, 0, len(l2vnis))
	var errs []error
	for _, l2vni := range l2vnis {
		if l2vni.Spec.Trunk == nil {
			valid = append(valid, l2vni)
			continue
		}

		key := trunkVLAN{trunk: l2vni.Spec.Trunk.Name, tag: l2vni.Spec.Trunk.VLAN.Tag}
		if owner, ok := owners[key]; ok {
			errs = append(errs, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: openpeerrors.KindL2VNI, Name: l2vni.Name,
					Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: fmt.Sprintf("VLAN %d on L2Trunk %s is already used by L2VNI %s",
						key.tag, key.trunk, owner),
				},
			})
			continue
		}
		owners[key] = l2vni.Name
		valid = append(valid, l2vni)
	}
	return valid, errors.Join(errs...)
}

func validateL2Trunk(trunk v1alpha1.L2Trunk) error {
	switch trunk.Spec.Type {
	case v1alpha1.L2TrunkTypeHost:
		if trunk.Spec.SRIOV != nil {
			return errors.New("sriov can only be set when type is SRIOV")
		}
		return nil
	case v1alpha1.L2TrunkTypeSRIOV:
		return validateSRIOVTrunk(trunk.Spec)
	}
	return fmt.Errorf("unsupported L2Trunk type %q", trunk.Spec.Type)
}

func validateSRIOVTrunk(spec v1alpha1.L2TrunkSpec) error {
	if spec.HostMaster != nil {
		return errors.New("hostMaster can only be set when type is Host")
	}
	if spec.SRIOV == nil {
		return errors.New("sriov must be set when type is SRIOV")
	}
	if err := isValidInterfaceName(spec.SRIOV.PFName); err != nil {
		return fmt.Errorf("invalid pfName %q: %w", spec.SRIOV.PFName, err)
	}
	if spec.SRIOV.VFIndex == nil {
		return errors.New("sriov.vfIndex must be set")
	}
	if *spec.SRIOV.VFIndex < 0 {
		return fmt.Errorf("invalid vfIndex %d: must not be negative", *spec.SRIOV.VFIndex)
	}
	return nil
}

func l2TrunkValidationError(name, message string) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind: openpeerrors.KindL2Trunk, Name: name,
			Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: message,
		},
	}
}
