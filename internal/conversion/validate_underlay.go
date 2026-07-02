// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/ipfamily"
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
		return &openpeerrors.ResourceError{
			Obj: v1alpha1.FailedResource{
				Kind:    v1alpha1.FailedResourceKind("Underlay"),
				Name:    underlays[0].Name,
				Reason:  v1alpha1.FailedResourceReasonValidationFailed,
				Message: "can't have more than one underlay per node",
			},
		}
	}
	if err := validateUnderlay(underlays[0]); err != nil {
		return &openpeerrors.ResourceError{
			Obj: v1alpha1.FailedResource{
				Kind:    v1alpha1.FailedResourceKind("Underlay"),
				Name:    underlays[0].Name,
				Reason:  v1alpha1.FailedResourceReasonValidationFailed,
				Message: err.Error(),
			},
		}
	}
	return nil
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

	// do a no-op conversion to catch validation errors
	if _, err := underlayInterfacesToHost(underlay.Spec.Interfaces); err != nil {
		return fmt.Errorf("underlay %s has invalid interfaces: %w", underlay.Name, err)
	}

	if underlay.Spec.TunnelEndpoint != nil {
		if err := validateUnderlayTunnelEndpoint(&underlay); err != nil {
			return err
		}
	}

	srv6Config := underlay.Spec.SRV6
	if srv6Config == nil {
		return nil
	}
	if _, isValid := locatorFormats[srv6Config.Locator.Format]; !isValid {
		return &openpeerrors.ResourceError{
			Obj: v1alpha1.FailedResource{
				Kind:    v1alpha1.FailedResourceKind("Underlay"),
				Name:    underlay.Name,
				Reason:  v1alpha1.FailedResourceReasonValidationFailed,
				Message: fmt.Sprintf("invalid locator format %q", srv6Config.Locator.Format),
			},
		}
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

	if underlay.Spec.SRV6 == nil {
		return nil
	}

	if af == ipfamily.IPv4 {
		return fmt.Errorf("invalid tunnel endpoint CIDRs for underlay %s with SRv6, no IPv6 CIDR found: %v",
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
