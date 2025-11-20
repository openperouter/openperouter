// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func ValidatePassthroughsForNodes(nodes []corev1.Node, underlays []v1alpha1.L3Passthrough) error {
	for _, node := range nodes {
		filteredPassThroughs, err := FilterL3PassthroughsForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		if err := ValidateNodePassthroughs(filteredPassThroughs); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func ValidateNodePassthroughs(l3Passthrough []v1alpha1.L3Passthrough) error {
	if len(l3Passthrough) > 1 {
		return fmt.Errorf("can't have more than one l3passthrough per node")
	}
	// host sessions are validated in ValidateHostSessions
	return nil
}
