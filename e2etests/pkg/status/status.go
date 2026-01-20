// SPDX-License-Identifier:Apache-2.0

package status

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Default timeout values for status checks
const (
	DefaultTimeout  = 30 * time.Second
	DefaultInterval = 5 * time.Second
)

// DefaultPodLabels are the default labels used to identify controller pods
var DefaultPodLabels = map[string]string{"app": "controller"}

// Checker provides methods for verifying RouterNodeConfigurationStatus state.
// Use NewPodModeChecker or NewHostModeChecker to create instances.
type Checker struct {
	k8sClient client.Client
	hostMode  bool
	timeout   time.Duration
	interval  time.Duration
	podLabels map[string]string
}

// NewPodModeChecker creates a Checker for pod mode deployments
func NewPodModeChecker(k8sClient client.Client) *Checker {
	return &Checker{
		k8sClient: k8sClient,
		hostMode:  false,
		timeout:   DefaultTimeout,
		interval:  DefaultInterval,
		podLabels: DefaultPodLabels,
	}
}

// NewHostModeChecker creates a Checker for host/systemd mode deployments
func NewHostModeChecker(k8sClient client.Client) *Checker {
	return &Checker{
		k8sClient: k8sClient,
		hostMode:  true,
		timeout:   DefaultTimeout,
		interval:  DefaultInterval,
		podLabels: DefaultPodLabels,
	}
}

// WithTimeout returns a new Checker with custom timeout values
func (c *Checker) WithTimeout(timeout, interval time.Duration) *Checker {
	return &Checker{
		k8sClient: c.k8sClient,
		hostMode:  c.hostMode,
		timeout:   timeout,
		interval:  interval,
		podLabels: c.podLabels,
	}
}

// WithPodLabels returns a new Checker with custom pod labels for identifying controller pods
func (c *Checker) WithPodLabels(labels map[string]string) *Checker {
	return &Checker{
		k8sClient: c.k8sClient,
		hostMode:  c.hostMode,
		timeout:   c.timeout,
		interval:  c.interval,
		podLabels: labels,
	}
}

// getControllerNodes returns nodes that should have running controllers
func (c *Checker) getControllerNodes() []corev1.Node {
	nodeList := &corev1.NodeList{}
	err := c.k8sClient.List(context.Background(), nodeList)
	if err != nil {
		return []corev1.Node{}
	}

	if c.hostMode {
		return nodeList.Items
	}

	podList := &corev1.PodList{}
	err = c.k8sClient.List(context.Background(), podList, client.InNamespace(openperouter.Namespace),
		client.MatchingLabels(c.podLabels))
	if err != nil {
		return []corev1.Node{}
	}

	controllerNodeNames := make(map[string]struct{})
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			controllerNodeNames[pod.Spec.NodeName] = struct{}{}
		}
	}

	var controllerNodes []corev1.Node
	for _, node := range nodeList.Items {
		if _, ok := controllerNodeNames[node.Name]; ok {
			controllerNodes = append(controllerNodes, node)
		}
	}

	return controllerNodes
}

// getStatusList returns all RouterNodeConfigurationStatus resources
func (c *Checker) getStatusList() *v1alpha1.RouterNodeConfigurationStatusList {
	statusList := &v1alpha1.RouterNodeConfigurationStatusList{}
	err := c.k8sClient.List(context.Background(), statusList, client.InNamespace(openperouter.Namespace))
	if err != nil {
		return &v1alpha1.RouterNodeConfigurationStatusList{}
	}
	return statusList
}

// getStableStatusList returns RouterNodeConfigurationStatus list with validation
func (c *Checker) getStableStatusList() (*v1alpha1.RouterNodeConfigurationStatusList, error) {
	controllerNodes := c.getControllerNodes()
	statusList := c.getStatusList()

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
func (c *Checker) ExpectSuccessfulStatus() {
	Eventually(func() error {
		statusList, err := c.getStableStatusList()
		if err != nil {
			return err
		}
		for _, status := range statusList.Items {
			if len(status.Status.FailedResources) > 0 {
				return fmt.Errorf("node %s has failed resources: %v", status.Name, status.Status.FailedResources)
			}
		}
		return nil
	}, c.timeout, c.interval).Should(Succeed())
}

// ExpectResourceFailure verifies that a specific resource failure is reported in status
func (c *Checker) ExpectResourceFailure(resourceKind, resourceName string) {
	Eventually(func() error {
		statusList, err := c.getStableStatusList()
		if err != nil {
			return err
		}
		for _, status := range statusList.Items {
			for _, failed := range status.Status.FailedResources {
				if failed.Kind == resourceKind && failed.Name == resourceName {
					return nil
				}
			}
		}
		return fmt.Errorf("expected failure for %s %s not found in any node status", resourceKind, resourceName)
	}, c.timeout, c.interval).Should(Succeed())
}

// ExpectOnlyResourceFailure verifies that a specific resource is the ONLY one failing
func (c *Checker) ExpectOnlyResourceFailure(resourceKind, resourceName string) {
	Eventually(func() error {
		statusList, err := c.getStableStatusList()
		if err != nil {
			return err
		}

		foundExpected := false
		var unexpectedFailures []string

		for _, status := range statusList.Items {
			for _, failed := range status.Status.FailedResources {
				if failed.Kind == resourceKind && failed.Name == resourceName {
					foundExpected = true
				} else {
					unexpectedFailures = append(unexpectedFailures,
						fmt.Sprintf("%s/%s on node %s", failed.Kind, failed.Name, status.Name))
				}
			}
		}

		if !foundExpected {
			return fmt.Errorf("expected failure for %s %s not found", resourceKind, resourceName)
		}
		if len(unexpectedFailures) > 0 {
			return fmt.Errorf("unexpected failures found: %v", unexpectedFailures)
		}
		return nil
	}, c.timeout, c.interval).Should(Succeed())
}

// ExpectNoResourceFailure verifies that a specific resource is NOT reported as failed
func (c *Checker) ExpectNoResourceFailure(resourceKind, resourceName string) {
	Eventually(func() error {
		statusList, err := c.getStableStatusList()
		if err != nil {
			return err
		}
		for _, status := range statusList.Items {
			for _, failed := range status.Status.FailedResources {
				if failed.Kind == resourceKind && failed.Name == resourceName {
					return fmt.Errorf("resource %s %s is still reported as failed on node %s: %s",
						resourceKind, resourceName, status.Name, failed.Message)
				}
			}
		}
		return nil
	}, c.timeout, c.interval).Should(Succeed())
}

// Legacy function wrappers for backward compatibility

// getControllerNodes returns nodes that should have running controllers (legacy wrapper)
func getControllerNodes(k8sClient client.Client, hostMode bool) []corev1.Node {
	c := &Checker{k8sClient: k8sClient, hostMode: hostMode, podLabels: DefaultPodLabels}
	return c.getControllerNodes()
}

// getStatusList returns all RouterNodeConfigurationStatus resources (legacy wrapper)
func getStatusList(k8sClient client.Client) *v1alpha1.RouterNodeConfigurationStatusList {
	c := &Checker{k8sClient: k8sClient}
	return c.getStatusList()
}

// getStableStatusList returns RouterNodeConfigurationStatus list with validation (legacy wrapper)
func getStableStatusList(k8sClient client.Client, hostMode bool) (*v1alpha1.RouterNodeConfigurationStatusList, error) {
	c := &Checker{k8sClient: k8sClient, hostMode: hostMode, podLabels: DefaultPodLabels}
	return c.getStableStatusList()
}

// ExpectSuccessfulStatus verifies that all nodes have successful status (legacy wrapper)
func ExpectSuccessfulStatus(k8sClient client.Client, hostMode bool) {
	c := &Checker{k8sClient: k8sClient, hostMode: hostMode, timeout: DefaultTimeout, interval: DefaultInterval, podLabels: DefaultPodLabels}
	c.ExpectSuccessfulStatus()
}

// ExpectResourceFailure verifies that a specific resource failure is reported (legacy wrapper)
func ExpectResourceFailure(k8sClient client.Client, resourceKind, resourceName string, hostMode bool) {
	c := &Checker{k8sClient: k8sClient, hostMode: hostMode, timeout: DefaultTimeout, interval: DefaultInterval, podLabels: DefaultPodLabels}
	c.ExpectResourceFailure(resourceKind, resourceName)
}

// ExpectOnlyResourceFailure verifies that a specific resource is the ONLY one failing (legacy wrapper)
func ExpectOnlyResourceFailure(k8sClient client.Client, resourceKind, resourceName string, hostMode bool) {
	c := &Checker{k8sClient: k8sClient, hostMode: hostMode, timeout: DefaultTimeout, interval: DefaultInterval, podLabels: DefaultPodLabels}
	c.ExpectOnlyResourceFailure(resourceKind, resourceName)
}

// ExpectNoResourceFailure verifies that a specific resource is NOT reported as failed (legacy wrapper)
func ExpectNoResourceFailure(k8sClient client.Client, resourceKind, resourceName string, hostMode bool) {
	c := &Checker{k8sClient: k8sClient, hostMode: hostMode, timeout: DefaultTimeout, interval: DefaultInterval, podLabels: DefaultPodLabels}
	c.ExpectNoResourceFailure(resourceKind, resourceName)
}
