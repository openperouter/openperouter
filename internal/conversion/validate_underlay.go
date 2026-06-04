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
	return validateUnderlay(underlays[0])
}

func validateUnderlay(underlay v1alpha1.Underlay) error {
	if underlay.Spec.ASN == 0 {
		return fmt.Errorf("underlay %s must have a valid ASN", underlay.Name)
	}

	// Validate at least one neighbor is specified
	if len(underlay.Spec.Neighbors) == 0 {
		return fmt.Errorf("underlay %s must have at least one neighbor configured", underlay.Name)
	}

	if err := validateNoDuplicates(neighborAddressesOf(underlay.Spec.Neighbors)); err != nil {
		return fmt.Errorf("underlay %s has duplicate neighbor address: %w", underlay.Name, err)
	}

	if err := validateNoDuplicates(underlay.Spec.Nics); err != nil {
		return fmt.Errorf("underlay %s has duplicate nic name: %w", underlay.Name, err)
	}

	if underlay.Spec.EVPN != nil {
		if err := validateUnderlayEVPN(&underlay); err != nil {
			return err
		}
	}

	for _, n := range underlay.Spec.Nics {
		if err := isValidInterfaceName(n); err != nil {
			return fmt.Errorf("invalid nic name for underlay %s: %s - %w", underlay.Name, n, err)
		}
	}

	return nil
}

func validateUnderlayEVPN(underlay *v1alpha1.Underlay) error {
	if underlay.Spec.EVPN.VTEPCIDR == nil || *underlay.Spec.EVPN.VTEPCIDR == "" {
		return fmt.Errorf("underlay %s: vtepCIDR must be specified", underlay.Name)
	}

	if _, _, err := net.ParseCIDR(*underlay.Spec.EVPN.VTEPCIDR); err != nil {
		return fmt.Errorf("invalid vtep CIDR format for underlay %s: %s - %w", underlay.Name, *underlay.Spec.EVPN.VTEPCIDR, err)
	}

	return nil
}

func neighborAddressesOf(neighbors []v1alpha1.Neighbor) []string {
	res := make([]string, len(neighbors))
	for i, n := range neighbors {
		if n.Address == nil {
			continue
		}
		res[i] = *n.Address
	}
	return res
}

func validateNoDuplicates(items []string) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			return fmt.Errorf("duplicate entry %s", item)
		}
		seen[item] = struct{}{}
	}
	return nil
}
