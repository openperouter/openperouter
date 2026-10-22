// SPDX-License-Identifier:Apache-2.0

package infra

import corev1 "k8s.io/api/core/v1"

const (
	KindControlPlane = "pe-kind-control-plane"
	KindWorker       = "pe-kind-worker"

	// UnderlayE2ELabelKey marks nodes used for underlay e2e assertions.
	// Do not use it as an Underlay nodeSelector.
	UnderlayE2ELabelKey   = "e2e.openperouter.io/underlay"
	UnderlayE2ELabelValue = "true"
)

// NodesWithUnderlayLabel returns nodes with UnderlayE2ELabelKey set.
func NodesWithUnderlayLabel(nodes []corev1.Node) []corev1.Node {
	out := make([]corev1.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Labels[UnderlayE2ELabelKey] == UnderlayE2ELabelValue {
			out = append(out, n)
		}
	}
	return out
}

// UnderlayNodeNames returns NodeNames(NodesWithUnderlayLabel(nodes)).
func UnderlayNodeNames(nodes []corev1.Node) map[string]bool {
	return NodeNames(NodesWithUnderlayLabel(nodes))
}
