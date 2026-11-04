// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"slices"
	"strings"

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
// ones alongside per-resource errors.
func FilterValidL2Trunks(l2trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	if len(l2trunks) == 0 {
		return l2trunks, nil
	}

	valid := make([]v1alpha1.L2Trunk, 0, len(l2trunks))
	var errs []error
	for _, trunk := range l2trunks {
		if err := validateL2Trunk(trunk); err != nil {
			errs = append(errs, l2TrunkValidationError(trunk.Name, err.Error()))
			continue
		}
		valid = append(valid, trunk)
	}

	valid, err := filterL2TrunksSRIOVUniquePF(valid)
	return valid, errors.Join(append(errs, err)...)
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

// filterL2TrunksSRIOVUniquePF drops the SR-IOV trunks selecting a PF already
// selected by an earlier trunk. The trunk VF carries the tagged frames of
// every workload VF on its PF, so a second trunk on the same PF would
// duplicate its traffic and loop it back through the NIC switch.
func filterL2TrunksSRIOVUniquePF(l2trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	// Sort the trunks by PF name to ensure deterministic behavior.
	slices.SortFunc(l2trunks, func(a, b v1alpha1.L2Trunk) int {
		return strings.Compare(a.Spec.SRIOV.PFName, b.Spec.SRIOV.PFName)
	})

	pfOwners := map[string]string{}
	valid := make([]v1alpha1.L2Trunk, 0, len(l2trunks))

	var errs []error
	for _, trunk := range l2trunks {
		pfName := trunk.Spec.SRIOV.PFName
		if owner, ok := pfOwners[pfName]; ok {
			errs = append(errs, l2TrunkValidationError(trunk.Name,
				fmt.Sprintf("PF %s is already used by L2Trunk %s", pfName, owner)))
			continue
		}
		pfOwners[pfName] = trunk.Name
		valid = append(valid, trunk)
	}
	return valid, errors.Join(errs...)
}

func validateL2Trunk(trunk v1alpha1.L2Trunk) error {
	if trunk.Spec.Type != v1alpha1.L2TrunkTypeSRIOV {
		return fmt.Errorf("unsupported L2Trunk type %q", trunk.Spec.Type)
	}
	return validateL2TrunkSRIOV(trunk.Spec)
}

func validateL2TrunkSRIOV(spec v1alpha1.L2TrunkSpec) error {
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
