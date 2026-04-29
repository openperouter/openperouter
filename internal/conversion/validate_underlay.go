// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/ipfamily"
)

func ValidateUnderlaysForNodes(nodes []corev1.Node, underlays []v1alpha1.Underlay, l3vpns []v1alpha1.L3VPN) error {
	for _, node := range nodes {
		filteredUnderlays, err := filter.UnderlaysForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		filteredL3VPNs, err := filter.L3VPNsForNode(&node, l3vpns)
		if err != nil {
			return fmt.Errorf("failed to filter L3VPNs for node %q: %w", node.Name, err)
		}
		if err := ValidateUnderlays(filteredUnderlays, filteredL3VPNs); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func ValidateUnderlays(underlays []v1alpha1.Underlay, l3vpns []v1alpha1.L3VPN) error {
	if len(underlays) == 0 {
		return nil
	}
	if len(underlays) > 1 {
		return fmt.Errorf("can't have more than one underlay per node")
	}
	return validateUnderlay(underlays[0], l3vpns)
}

func validateUnderlay(underlay v1alpha1.Underlay, l3vpns []v1alpha1.L3VPN) error {
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

	if underlay.Spec.TunnelEndpoint != nil {
		if err := validateUnderlayTunnelEndpoint(&underlay); err != nil {
			return err
		}
	}

	for _, n := range underlay.Spec.Nics {
		if err := isValidInterfaceName(n); err != nil {
			return fmt.Errorf("invalid nic name for underlay %s: %s - %w", underlay.Name, n, err)
		}
	}

	if err := validateUnderlaySRv6(underlay.Spec.SRV6, l3vpns); err != nil {
		return fmt.Errorf("invalid SRv6 configuration in underlay %q, err: %w", underlay.Name, err)
	}

	return nil
}

func validateUnderlayTunnelEndpoint(underlay *v1alpha1.Underlay) error {
	if underlay.Spec.TunnelEndpoint == nil {
		return fmt.Errorf("underlay %s: tunnel endpoint must be specified", underlay.Name)
	}

	cidrs := underlay.Spec.TunnelEndpoint.CIDRs
	if len(cidrs) == 0 {
		return fmt.Errorf("underlay %s: tunnel endpoint CIDRs must be specified", underlay.Name)
	}

	af, err := ipfamily.ForCIDRStrings(cidrs...)
	if err != nil {
		return fmt.Errorf("invalid tunnel endpoint CIDRs for underlay %s: %v - %w",
			underlay.Name, cidrs, err)
	}

	if underlay.Spec.SRV6 != nil && af == ipfamily.IPv4 {
		return fmt.Errorf("invalid tunnel endpoint CIDRs for underlay %s with SRv6, no IPv6 CIDR found: %v",
			underlay.Name, cidrs)
	}
	if underlay.Spec.SRV6 != nil {
		return nil
	}

	if af == ipfamily.IPv6 {
		return fmt.Errorf("invalid tunnel endpoint CIDRs for underlay %s, no IPv4 CIDR found: %v",
			underlay.Name, cidrs)
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

func validateUnderlaySRv6(srv6Config *v1alpha1.SRV6Config, l3vpns []v1alpha1.L3VPN) error {
	if len(l3vpns) > 0 && srv6Config == nil {
		return fmt.Errorf("invalid SRv6 configuration. Cannot be nil when there are %d matching L3VPNs", len(l3vpns))
	}

	if srv6Config == nil {
		return nil
	}

	if _, isValid := locatorFormats[srv6Config.Locator.Format]; !isValid {
		return fmt.Errorf("invalid locator format %q", srv6Config.Locator.Format)
	}

	return nil
}
