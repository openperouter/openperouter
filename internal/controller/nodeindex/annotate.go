// SPDX-License-Identifier:Apache-2.0

package nodeindex

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

func AnnotateNodeIndex(ctx context.Context, clientset kubernetes.Interface, nodeName string, nodeIndex int) error {
	patchData := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]string{
				OpenpeNodeIndex: strconv.Itoa(nodeIndex),
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
