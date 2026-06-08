// SPDX-License-Identifier:Apache-2.0

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/api/static"
	"github.com/openperouter/openperouter/internal/controller/nodeindex"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func annotateCurrentNode(ctx context.Context, nodeConfig *static.NodeConfig, nodeName string) error {
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	slog.Info("Annotating node with node index", "node", nodeName, "nodeIndex", nodeConfig.NodeIndex)

	return nodeindex.AnnotateNodeIndex(ctx, clientset, nodeName, nodeConfig.NodeIndex)
}
