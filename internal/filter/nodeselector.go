// SPDX-License-Identifier:Apache-2.0

package filter

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
)

// UnderlaysForNode returns underlays that match the given node's labels.
func UnderlaysForNode(node *corev1.Node, underlays []v1alpha1.Underlay) ([]v1alpha1.Underlay, error) {
	return filterForNode(node, underlays, "Underlay", func(u v1alpha1.Underlay) (*metav1.LabelSelector, string) {
		return u.Spec.NodeSelector, u.Name
	})
}

// L3VNIsForNode returns L3VNIs that match the given node's labels.
func L3VNIsForNode(node *corev1.Node, l3vnis []v1alpha1.L3VNI) ([]v1alpha1.L3VNI, error) {
	return filterForNode(node, l3vnis, "L3VNI", func(v v1alpha1.L3VNI) (*metav1.LabelSelector, string) {
		return v.Spec.NodeSelector, v.Name
	})
}

// L2VNIsForNode returns L2VNIs that match the given node's labels.
func L2VNIsForNode(node *corev1.Node, l2vnis []v1alpha1.L2VNI) ([]v1alpha1.L2VNI, error) {
	return filterForNode(node, l2vnis, "L2VNI", func(v v1alpha1.L2VNI) (*metav1.LabelSelector, string) {
		return v.Spec.NodeSelector, v.Name
	})
}

// L3VPNsForNode returns L3VPNs that match the given node's labels.
func L3VPNsForNode(node *corev1.Node, l3vpns []v1alpha1.L3VPN) ([]v1alpha1.L3VPN, error) {
	return filterForNode(node, l3vpns, "L3VPN", func(v v1alpha1.L3VPN) (*metav1.LabelSelector, string) {
		return v.Spec.NodeSelector, v.Name
	})
}

// L3PassthroughsForNode returns L3Passthroughs that match the given node's labels.
func L3PassthroughsForNode(node *corev1.Node, l3passthroughs []v1alpha1.L3Passthrough) ([]v1alpha1.L3Passthrough, error) {
	return filterForNode(node, l3passthroughs, "L3Passthrough", func(p v1alpha1.L3Passthrough) (*metav1.LabelSelector, string) {
		return p.Spec.NodeSelector, p.Name
	})
}

// RawFRRConfigsForNode returns RawFRRConfigs that match the given node's labels.
func RawFRRConfigsForNode(node *corev1.Node, rawFRRConfigs []v1alpha1.RawFRRConfig) ([]v1alpha1.RawFRRConfig, error) {
	return filterForNode(node, rawFRRConfigs, "RawFRRConfig", func(r v1alpha1.RawFRRConfig) (*metav1.LabelSelector, string) {
		return r.Spec.NodeSelector, r.Name
	})
}

// NodeSelectorMatches reports whether the selector matches the node. A nil selector returns an error.
func NodeSelectorMatches(node *corev1.Node, nodeSelector *metav1.LabelSelector) (bool, error) {
	if nodeSelector == nil {
		return false, errors.New("node selector is nil")
	}
	labelSelector, err := metav1.LabelSelectorAsSelector(nodeSelector)
	if err != nil {
		return false, err
	}
	return labelSelector.Matches(labels.Set(node.Labels)), nil
}

// filterForNode is a generic function that filters items based on node label selectors.
// Invalid selectors are collected as ResourceErrors; valid matching items are still returned.
func filterForNode[T any](node *corev1.Node, items []T, kind v1alpha1.FailedResourceKind, getSelector func(T) (*metav1.LabelSelector, string)) ([]T, error) {
	var result []T
	var allErrors []error
	for _, item := range items {
		nodeSelector, name := getSelector(item)

		// nil NodeSelector matches all nodes
		if nodeSelector == nil {
			result = append(result, item)
			continue
		}

		labelSelector, err := metav1.LabelSelectorAsSelector(nodeSelector)
		if err != nil {
			allErrors = append(allErrors, &openpeerrors.ResourceError{Obj: v1alpha1.FailedResource{
				Kind: kind, Name: name, Reason: v1alpha1.FailedResourceReasonValidationFailed,
				Message: fmt.Sprintf("invalid node selector: %v", err),
			}})
			continue
		}

		if labelSelector.Matches(labels.Set(node.Labels)) {
			result = append(result, item)
		}
	}
	return result, errors.Join(allErrors...)
}
