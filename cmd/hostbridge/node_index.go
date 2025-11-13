// SPDX-License-Identifier:Apache-2.0

package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/openperouter/openperouter/internal/staticconfiguration"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// OpenpeNodeIndex is the annotation key for the node index
	OpenpeNodeIndex = "openpe.io/nodeindex"
)

// AnnotateCurrentNode adds the node index annotation to the current node based on
// the content of the static configuration file.
func AnnotateCurrentNode(ctx context.Context, configPath, nodeName string) error {
	config, err := staticconfiguration.ReadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read static configuration from %s: %w", configPath, err)
	}

	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	annotations := node.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[OpenpeNodeIndex] = strconv.Itoa(config.NodeIndex)
	node.Annotations = annotations

	if _, err := clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update node %s with annotation: %w", nodeName, err)
	}

	return nil
}
