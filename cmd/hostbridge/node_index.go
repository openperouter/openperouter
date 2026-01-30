// SPDX-License-Identifier:Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/openperouter/openperouter/internal/staticconfiguration"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// OpenpeNodeIndex is the annotation key for the node index
	OpenpeNodeIndex = "openpe.io/nodeindex"
)

// annotateCurrentNode adds the node index annotation to the current node based on
// the content of the static configuration file.
func annotateCurrentNode(ctx context.Context, nodeConfigPath, nodeName string) error {
	nodeConfig, err := staticconfiguration.ReadNodeConfig(nodeConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read node configuration from %s: %w", nodeConfigPath, err)
	}

	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Create the patch for adding the annotation
	patchData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				OpenpeNodeIndex: strconv.Itoa(nodeConfig.NodeIndex),
			},
		},
	}

	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("failed to marshal patch data: %w", err)
	}

	return wait.ExponentialBackoff(wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2.0,
		Steps:    5,
	}, func() (bool, error) {
		_, err := clientset.CoreV1().Nodes().Patch(
			ctx, nodeName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	})
}
