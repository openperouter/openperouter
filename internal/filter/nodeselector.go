// SPDX-License-Identifier:Apache-2.0

package filter

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

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

// nodeMatchesSelector returns true if the given node matches the label selector.
// If the selector is nil or empty, it matches all nodes (backward compatible).
func nodeMatchesSelector(node *corev1.Node, labelSelector labels.Selector) bool {
	// Empty selector matches everything
	if labelSelector == labels.Nothing() || labelSelector.Empty() {
		return true
	}

	return labelSelector.Matches(labels.Set(node.Labels))
}

// filterForNode is a generic function that filters items based on node label selectors.
// It takes a selector function that extracts the NodeSelector from each item.
func filterForNode[T any](node *corev1.Node, items []T, getSelector func(T) *metav1.LabelSelector) ([]T, error) {
	var result []T
	for _, item := range items {
		labelSelector, err := metav1.LabelSelectorAsSelector(getSelector(item))
		if err != nil {
			return nil, err
		}

		if nodeMatchesSelector(node, labelSelector) {
			result = append(result, item)
		}
	}
	return result, nil
}
