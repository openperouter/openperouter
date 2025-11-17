// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

// NodeMatchesSelector returns true if the given node matches the label selector.
// If the selector is nil or empty, it matches all nodes (backward compatible).
func NodeMatchesSelector(node *corev1.Node, selector *metav1.LabelSelector) (bool, error) {
	if selector == nil {
		return true, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}

	// Empty selector matches everything
	if labelSelector.Empty() {
		return true, nil
	}

	return labelSelector.Matches(labels.Set(node.Labels)), nil
}

// FilterUnderlaysForNode returns underlays that match the given node's labels.
func FilterUnderlaysForNode(node *corev1.Node, underlays []v1alpha1.Underlay) ([]v1alpha1.Underlay, error) {
	var result []v1alpha1.Underlay
	for _, underlay := range underlays {
		matches, err := NodeMatchesSelector(node, underlay.Spec.NodeSelector)
		if err != nil {
			return nil, err
		}
		if matches {
			result = append(result, underlay)
		}
	}
	return result, nil
}

// FilterL3VNIsForNode returns L3VNIs that match the given node's labels.
func FilterL3VNIsForNode(node *corev1.Node, l3vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, error) {
	var result []v1alpha1.L3VNI
	for _, l3vni := range l3vnis {
		matches, err := NodeMatchesSelector(node, l3vni.Spec.NodeSelector)
		if err != nil {
			return nil, err
		}
		if matches {
			result = append(result, l3vni)
		}
	}
	return result, nil
}

// FilterL2VNIsForNode returns L2VNIs that match the given node's labels.
func FilterL2VNIsForNode(node *corev1.Node, l2vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	var result []v1alpha1.L2VNI
	for _, l2vni := range l2vnis {
		matches, err := NodeMatchesSelector(node, l2vni.Spec.NodeSelector)
		if err != nil {
			return nil, err
		}
		if matches {
			result = append(result, l2vni)
		}
	}
	return result, nil
}

// FilterL3PassthroughsForNode returns L3Passthroughs that match the given node's labels.
func FilterL3PassthroughsForNode(node *corev1.Node, l3passthroughs []v1alpha1.L3Passthrough) ([]v1alpha1.L3Passthrough, error) {
	var result []v1alpha1.L3Passthrough
	for _, l3passthrough := range l3passthroughs {
		matches, err := NodeMatchesSelector(node, l3passthrough.Spec.NodeSelector)
		if err != nil {
			return nil, err
		}
		if matches {
			result = append(result, l3passthrough)
		}
	}
	return result, nil
}
