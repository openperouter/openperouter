// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodesWithUnderlayLabel(t *testing.T) {
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "master", Labels: map[string]string{}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "worker", Labels: map[string]string{
			UnderlayE2ELabelKey: UnderlayE2ELabelValue,
		}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "wrong-value", Labels: map[string]string{
			UnderlayE2ELabelKey: "yes",
		}}},
	}
	got := NodesWithUnderlayLabel(nodes)
	if len(got) != 1 || got[0].Name != "worker" {
		t.Fatalf("NodesWithUnderlayLabel = %v, want [worker]", nodeNames(got))
	}
}

func nodeNames(nodes []corev1.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}
