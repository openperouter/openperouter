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

// filterL2TrunksSRIOVUniquePF drops the SR-IOV trunks selecting a PF already
// selected by an earlier trunk. The trunk VF carries the tagged frames of
// every workload VF on its PF, so a second trunk on the same PF would
// duplicate its traffic and loop it back through the NIC switch.
func filterL2TrunksSRIOVUniquePF(l2trunks []v1alpha1.L2Trunk) ([]v1alpha1.L2Trunk, error) {
	// Host trunks have no PF, so only the SRIOV ones take part in the check.
	sriovTrunks := make([]v1alpha1.L2Trunk, 0, len(l2trunks))
	for _, trunk := range l2trunks {
		if trunk.Spec.Type == v1alpha1.L2TrunkTypeSRIOV {
			sriovTrunks = append(sriovTrunks, trunk)
		}
	}
	// Sort by PF name to ensure deterministic behavior.
	slices.SortFunc(sriovTrunks, func(a, b v1alpha1.L2Trunk) int {
		return strings.Compare(a.Spec.SRIOV.PFName, b.Spec.SRIOV.PFName)
	})

	pfOwners := map[string]string{}
	rejected := map[string]struct{}{}
	var errs []error
	for _, trunk := range sriovTrunks {
		pfName := trunk.Spec.SRIOV.PFName
		if owner, ok := pfOwners[pfName]; ok {
			errs = append(errs, l2TrunkValidationError(trunk.Name,
				fmt.Sprintf("PF %s is already used by L2Trunk %s", pfName, owner)))
			rejected[trunk.Name] = struct{}{}
			continue
		}
		pfOwners[pfName] = trunk.Name
	}

	valid := make([]v1alpha1.L2Trunk, 0, len(l2trunks))
	for _, trunk := range l2trunks {
		if _, drop := rejected[trunk.Name]; drop {
			continue
		}
		valid = append(valid, trunk)
	}
	return valid, errors.Join(errs...)
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
