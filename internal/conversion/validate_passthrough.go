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

func ValidatePassthroughsForNodes(nodes []corev1.Node, underlays []v1alpha1.L3Passthrough) error {
	for _, node := range nodes {
		filteredPassThroughs, err := filter.L3PassthroughsForNode(&node, underlays)
		if err != nil {
			return fmt.Errorf("failed to filter underlays for node %q: %w", node.Name, err)
		}
		if _, err := FilterValidPassthroughs(filteredPassThroughs); err != nil {
			return fmt.Errorf("failed to validate underlays for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func FilterValidPassthroughs(l3Passthrough []v1alpha1.L3Passthrough) ([]v1alpha1.L3Passthrough, error) {
	if len(l3Passthrough) > 1 {
		allErrors := make([]error, 0, len(l3Passthrough))
		for _, pt := range l3Passthrough {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind:    v1alpha1.FailedResourceKind("L3Passthrough"),
					Name:    pt.Name,
					Reason:  v1alpha1.FailedResourceReasonValidationFailed,
					Message: "can't have more than one l3passthrough per node",
				},
			})
		}
		return nil, errors.Join(allErrors...)
	}
	return l3Passthrough, nil
}
