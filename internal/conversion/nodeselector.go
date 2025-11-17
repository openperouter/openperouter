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

// filterForNode is a generic function that filters items based on node label selectors.
// It takes a selector function that extracts the NodeSelector from each item.
func filterForNode[T any](node *corev1.Node, items []T, getSelector func(T) *metav1.LabelSelector) ([]T, error) {
	var result []T
	for _, item := range items {
		matches, err := NodeMatchesSelector(node, getSelector(item))
		if err != nil {
			return nil, err
		}
		if matches {
			result = append(result, item)
		}
	}
	return result, nil
}

// FilterUnderlaysForNode returns underlays that match the given node's labels.
func FilterUnderlaysForNode(node *corev1.Node, underlays []v1alpha1.Underlay) ([]v1alpha1.Underlay, error) {
	return filterForNode(node, underlays, func(u v1alpha1.Underlay) *metav1.LabelSelector {
		return u.Spec.NodeSelector
	})
}

// FilterL3VNIsForNode returns L3VNIs that match the given node's labels.
func FilterL3VNIsForNode(node *corev1.Node, l3vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, error) {
	return filterForNode(node, l3vnis, func(v v1alpha1.L3VNI) *metav1.LabelSelector {
		return v.Spec.NodeSelector
	})
}

// FilterL2VNIsForNode returns L2VNIs that match the given node's labels.
func FilterL2VNIsForNode(node *corev1.Node, l2vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	return filterForNode(node, l2vnis, func(v v1alpha1.L2VNI) *metav1.LabelSelector {
		return v.Spec.NodeSelector
	})
}

// FilterL3PassthroughsForNode returns L3Passthroughs that match the given node's labels.
func FilterL3PassthroughsForNode(node *corev1.Node, l3passthroughs []v1alpha1.L3Passthrough) ([]v1alpha1.L3Passthrough, error) {
	return filterForNode(node, l3passthroughs, func(p v1alpha1.L3Passthrough) *metav1.LabelSelector {
		return p.Spec.NodeSelector
	})
}
