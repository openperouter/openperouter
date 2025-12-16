// SPDX-License-Identifier:Apache-2.0

package status

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getControllerNodes returns nodes that should have running controllers
// In pod mode: finds nodes with running controller pods
// In systemd mode: returns all nodes (controllers are expected on all nodes)
func getControllerNodes(k8sClient client.Client, hostMode bool) []corev1.Node {
	// Get all nodes
	nodeList := &corev1.NodeList{}
	err := k8sClient.List(context.Background(), nodeList)
	if err != nil {
		return []corev1.Node{}
	}

	if hostMode {
		// In systemd mode, expect controllers on all nodes
		return nodeList.Items
	}

	// Pod mode: Get controller pods to find which nodes have controllers
	podList := &corev1.PodList{}
	err = k8sClient.List(context.Background(), podList, client.InNamespace("openperouter-system"),
		client.MatchingLabels{"app": "controller"})
	if err != nil {
		return []corev1.Node{}
	}

	controllerNodeNames := make(map[string]bool)
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			controllerNodeNames[pod.Spec.NodeName] = true
		}
	}

	var controllerNodes []corev1.Node
	for _, node := range nodeList.Items {
		if controllerNodeNames[node.Name] {
			controllerNodes = append(controllerNodes, node)
		}
	}

	return controllerNodes
}

// getStatusList returns all RouterNodeConfigurationStatus resources
func getStatusList(k8sClient client.Client) *v1alpha1.RouterNodeConfigurationStatusList {
	statusList := &v1alpha1.RouterNodeConfigurationStatusList{}
	err := k8sClient.List(context.Background(), statusList, client.InNamespace("openperouter-system"))
	if err != nil {
		return &v1alpha1.RouterNodeConfigurationStatusList{}
	}
	return statusList
}

// getStableStatusList returns RouterNodeConfigurationStatus list with validation
// Returns the status list only when controller nodes and statuses are properly matched
func getStableStatusList(k8sClient client.Client, hostMode bool) (*v1alpha1.RouterNodeConfigurationStatusList, error) {
	controllerNodes := getControllerNodes(k8sClient, hostMode)
	statusList := getStatusList(k8sClient)

	if len(controllerNodes) == 0 {
		return nil, fmt.Errorf("expected at least one controller to be running")
	}

	if len(statusList.Items) != len(controllerNodes) {
		return nil, fmt.Errorf("expected %d RouterNodeConfigurationStatus resources (one per controller node), got %d",
			len(controllerNodes), len(statusList.Items))
	}

	return statusList, nil
}

// ExpectSuccessfulStatus verifies that all nodes have successful status (no failed resources)
func ExpectSuccessfulStatus(k8sClient client.Client, hostMode bool) {
	Eventually(func() error {
		statusList, err := getStableStatusList(k8sClient, hostMode)
		if err != nil {
			return err
		}
		for _, status := range statusList.Items {
			if len(status.Status.FailedResources) > 0 {
				return fmt.Errorf("node %s has failed resources: %v", status.Name, status.Status.FailedResources)
			}
		}
		return nil
	}, "30s", "5s").Should(Succeed())
}

// ExpectResourceFailure verifies that a specific resource failure is reported in status
func ExpectResourceFailure(k8sClient client.Client, resourceKind, resourceName string, hostMode bool) {
	Eventually(func() error {
		statusList, err := getStableStatusList(k8sClient, hostMode)
		if err != nil {
			return err
		}
		for _, status := range statusList.Items {
			for _, failed := range status.Status.FailedResources {
				if failed.Kind == resourceKind && failed.Name == resourceName {
					return nil // Found the expected failure
				}
			}
		}
		return fmt.Errorf("expected failure for %s %s not found in any node status", resourceKind, resourceName)
	}, "30s", "5s").Should(Succeed())
}
