// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applyconfigurationscorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	nodeLabelerFieldManagerName = "openpe-e2e-node-labeler"
)

func GetNodes(cs clientset.Interface) ([]corev1.Node, error) {
	nodes, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	return nodes.Items, nil
}

func LabelNodes(cs clientset.Interface, labels map[string]string, nodes ...corev1.Node) error {
	for _, node := range nodes {
		_, err := cs.CoreV1().Nodes().Apply(
			context.Background(),
			applyconfigurationscorev1.
				Node(node.Name).
				WithLabels(labels),
			metav1.ApplyOptions{
				FieldManager: nodeLabelerFieldManagerName,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to apply labels to node %q: %w", node.Name, err)
		}
	}
	return nil
}

func UnlabelNodes(cs clientset.Interface, nodes ...corev1.Node) error {
	// This will remove the labels applied with the specified field manager
	return LabelNodes(cs, map[string]string{}, nodes...)
}
