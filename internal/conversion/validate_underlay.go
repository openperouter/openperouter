// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	corev1 "k8s.io/api/core/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/status"
)

func ValidateUnderlaysForNodes(nodes []corev1.Node, underlays []v1alpha1.Underlay) error {
	for _, node := range nodes {
		filteredUnderlays, err := filter.UnderlaysForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		if err := ValidateUnderlays(filteredUnderlays, &NoOpStatusReporter{}); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func ValidateUnderlays(underlays []v1alpha1.Underlay, statusReporter status.StatusReporter) error {
	if len(underlays) == 0 {
		return nil
	}
	if len(underlays) > 1 {
		return fmt.Errorf("can't have more than one underlay per node")
	}
	return validateUnderlay(&underlays[0], statusReporter)
}

func validateUnderlay(underlay *v1alpha1.Underlay, statusReporter status.StatusReporter) error {
	if underlay.Spec.ASN == 0 {
		err := fmt.Errorf("underlay %s must have a valid ASN", underlay.Name)
		statusReporter.ReportResourceFailure(status.UnderlayKind, underlay.Name, err)
		return err
	}

	for _, neighbor := range underlay.Spec.Neighbors {
		if underlay.Spec.ASN == neighbor.ASN {
			err := fmt.Errorf("underlay %s local ASN %d must be different from remote ASN %d", underlay.Name, underlay.Spec.ASN, neighbor.ASN)
			statusReporter.ReportResourceFailure(status.UnderlayKind, underlay.Name, err)
			return err
		}
	}

	if underlay.Spec.EVPN != nil {
		if _, _, err := net.ParseCIDR(underlay.Spec.EVPN.VTEPCIDR); err != nil {
			validationErr := fmt.Errorf("invalid vtep CIDR format for underlay %s: %s - %w", underlay.Name, underlay.Spec.EVPN.VTEPCIDR, err)
			statusReporter.ReportResourceFailure(status.UnderlayKind, underlay.Name, validationErr)
			return validationErr
		}
	}

	if len(underlay.Spec.Nics) > 1 {
		err := fmt.Errorf("underlay %s can only have one nic, found %d", underlay.Name, len(underlay.Spec.Nics))
		statusReporter.ReportResourceFailure(status.UnderlayKind, underlay.Name, err)
		return err
	}

	for _, n := range underlay.Spec.Nics {
		if err := isValidInterfaceName(n); err != nil {
			validationErr := fmt.Errorf("invalid nic name for underlay %s: %s - %w", underlay.Name, n, err)
			statusReporter.ReportResourceFailure(status.UnderlayKind, underlay.Name, validationErr)
			return validationErr
		}
	}
	return nil
}
