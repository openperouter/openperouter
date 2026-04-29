// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
	corev1 "k8s.io/api/core/v1"
)

// ValidateL3VPNsForNodes runs L3VPN specific validation, per Node.
func ValidateL3VPNsForNodes(nodes []corev1.Node, l3vpns []v1alpha1.L3VPN, underlays []v1alpha1.Underlay,
	l2vnis []v1alpha1.L2VNI) error {
	for _, node := range nodes {
		filteredL3VPNs, err := filter.L3VPNsForNode(&node, l3vpns)
		if err != nil {
			return fmt.Errorf("failed to filter L3VPNs for node %q: %w", node.Name, err)
		}
		filteredUnderlays, err := filter.UnderlaysForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		filteredL2VNIs, err := filter.L2VNIsForNode(&node, l2vnis)
		if err != nil {
			return fmt.Errorf("failed to filter L2VNIs for node %q: %w", node.Name, err)
		}
		if err := ValidateL3VPNs(filteredL3VPNs, filteredUnderlays, filteredL2VNIs); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}

	return nil
}

// ValidateL3VPNs runs L3VPN specific validation.
func ValidateL3VPNs(l3vpns []v1alpha1.L3VPN, underlays []v1alpha1.Underlay, l2vnis []v1alpha1.L2VNI) error {
	if len(l3vpns) == 0 {
		return nil
	}
	if len(underlays) == 0 {
		return errors.New("cannot create L3VPNs without a valid SRV6 configuration (have no underlays)")
	}
	if len(underlays) > 1 {
		return errors.New("cannot have more than one underlay per node")
	}
	if underlays[0].Spec.SRV6 == nil {
		return errors.New("cannot create L3VPNs without a valid underlay.Spec.SRV6 definition")
	}

	vnis := append(vnisFromL3VPNs(l3vpns), vnisFromL2VNIs(l2vnis)...)
	if err := validateVNIs(vnis); err != nil {
		return err
	}

	return nil
}

// vnisFromL3VPNSs converts L3VPNs to vni slice.
// We set vni to the value of RDAssignedNumber: VNIs build RTs implicitly based on the VNI value, and so do we with
// exportRTs and RDAssignedNumber (in to FRR conversion). We also use the RDAssignedNumber as the numeric identifier for
// interfaces, the same as the VNI (in to host conversion).
func vnisFromL3VPNs(l3vpns []v1alpha1.L3VPN) []VNI {
	result := make([]VNI, len(l3vpns))
	for i, l3vni := range l3vpns {
		exportRTs := make([]string, 0, len(l3vni.Spec.ExportRTs))
		importRTs := make([]string, 0, len(l3vni.Spec.ImportRTs))
		for _, exportRT := range l3vni.Spec.ExportRTs {
			exportRTs = append(exportRTs, string(exportRT))
		}
		for _, importRT := range l3vni.Spec.ImportRTs {
			importRTs = append(importRTs, string(importRT))
		}
		result[i] = VNI{
			name:      l3vni.Name,
			vni:       uint32(l3vni.Spec.RDAssignedNumber),
			vrfName:   l3vni.Spec.VRF,
			exportRTs: exportRTs,
			importRTs: importRTs,
		}
	}
	return result
}
