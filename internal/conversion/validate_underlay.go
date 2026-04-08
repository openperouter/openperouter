// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
)

func ValidateUnderlaysForNodes(nodes []corev1.Node, underlays []v1alpha1.Underlay) error {
	for _, node := range nodes {
		filteredUnderlays, err := filter.UnderlaysForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		if err := ValidateUnderlays(filteredUnderlays); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func ValidateUnderlays(underlays []v1alpha1.Underlay) error {
	if len(underlays) == 0 {
		return nil
	}
	if len(underlays) > 1 {
		return fmt.Errorf("can't have more than one underlay per node")
	}
	for _, underlay := range underlays {
		if err := validateUnderlay(underlay); err != nil {
			return err
		}
	}
	return nil
}

func validateUnderlay(underlay v1alpha1.Underlay) error {
	if underlay.Spec.ASN == 0 {
		return fmt.Errorf("underlay %s must have a valid ASN", underlay.Name)
	}

	// Validate at least one neighbor or one nic is specified
	if len(underlay.Spec.Neighbors) == 0 && len(underlay.Spec.Nics) == 0 {
		return fmt.Errorf("underlay %s must have at least one neighbor or one nic configured", underlay.Name)
	}

	// Validate neighbor uniqueness
	neighborAddresses := make(map[string]bool)
	for _, neighbor := range underlay.Spec.Neighbors {
		if underlay.Spec.ASN == neighbor.ASN {
			return fmt.Errorf("underlay %s local ASN %d must be different from remote ASN %d for neighbor %s", underlay.Name, underlay.Spec.ASN, neighbor.ASN, neighbor.Address)
		}
		if neighborAddresses[neighbor.Address] {
			return fmt.Errorf("underlay %s has duplicate neighbor address: %s", underlay.Name, neighbor.Address)
		}
		neighborAddresses[neighbor.Address] = true
	}

	// Validate nic uniqueness
	nicNames := make(map[string]bool)
	for _, nic := range underlay.Spec.Nics {
		if nicNames[nic] {
			return fmt.Errorf("underlay %s has duplicate nic name: %s", underlay.Name, nic)
		}
		nicNames[nic] = true
	}

	if underlay.Spec.EVPN != nil {
		hasVTEPCIDR := underlay.Spec.EVPN.VTEPCIDR != ""
		hasVTEPInterface := underlay.Spec.EVPN.VTEPInterface != ""

		if hasVTEPCIDR == hasVTEPInterface {
			return fmt.Errorf("underlay %s: either vtepcidr (%t) or vtepInterface (%t) (not both) must be specified", underlay.Name, hasVTEPCIDR, hasVTEPInterface)
		}

		if hasVTEPCIDR {
			if _, _, err := net.ParseCIDR(underlay.Spec.EVPN.VTEPCIDR); err != nil {
				return fmt.Errorf("invalid vtep CIDR format for underlay %s: %s - %w", underlay.Name, underlay.Spec.EVPN.VTEPCIDR, err)
			}
		}

		if hasVTEPInterface {
			if err := isValidInterfaceName(underlay.Spec.EVPN.VTEPInterface); err != nil {
				return fmt.Errorf("invalid vtep interface name %q for underlay %q: %w", underlay.Name, underlay.Spec.EVPN.VTEPInterface, err)
			}
		}
	}

	for _, n := range underlay.Spec.Nics {
		if err := isValidInterfaceName(n); err != nil {
			return fmt.Errorf("invalid nic name for underlay %s: %s - %w", underlay.Name, n, err)
		}
	}
	return nil
}
