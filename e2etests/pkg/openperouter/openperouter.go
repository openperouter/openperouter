// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"io"
	"iter"
	"time"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"

	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var NamedNSMode bool

const (
	Namespace           = "openperouter-system"
	routerLabelSelector = "app=router"
)

type Routers interface {
	Dump(writer io.Writer)
	GetExecutors() iter.Seq[RouterExecutor]
	ExecutorForNode(nodeName string) (RouterExecutor, error)
}

type RouterExecutor interface {
	executor.Executor
	Name() string
}

func Get(cs clientset.Interface, hostMode bool) (Routers, error) {
	if !hostMode {
		pods, err := k8s.PodsForLabel(cs, Namespace, routerLabelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve pods %w", err)
		}
		running := filterRunningPods(pods)
		if len(running) == 0 {
			return nil, fmt.Errorf("no running (non-terminating) router pods found")
		}
		return routerPods{pods: running}, nil
	}

	nodes, err := k8s.GetNodes(cs)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve nodes %w", err)
	}

	routers := []routerPodman{}
	for _, node := range nodes {
		pid, err := getPodmanRouterPID(node.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get router pod PID for node %s: %w", node.Name, err)
		}
		routers = append(routers, routerPodman{
			nodeName: node.Name,
			pid:      pid,
		})
	}

	return routerPodmans{routers: routers}, nil
}

func RouterPodsForNodes(cs clientset.Interface, nodes map[string]bool) ([]*corev1.Pod, error) {
	routerPods, err := k8s.PodsForLabel(cs, Namespace, routerLabelSelector)
	if err != nil {
		return nil, err
	}
	filteredRouterPods := []*corev1.Pod{}
	for _, p := range filterRunningPods(routerPods) {
		if nodes[p.Spec.NodeName] {
			filteredRouterPods = append(filteredRouterPods, p)
		}
	}
	return filteredRouterPods, nil
}

// filterRunningPods returns only pods that are not being terminated.
func filterRunningPods(pods []*corev1.Pod) []*corev1.Pod {
	running := make([]*corev1.Pod, 0, len(pods))
	for _, p := range pods {
		if p.DeletionTimestamp == nil {
			running = append(running, p)
		}
	}
	return running
}

// ExecutorForNode returns the RouterExecutor running on the given node.
func ExecutorForNode(routers Routers, nodeName string) (RouterExecutor, error) {
	return routers.ExecutorForNode(nodeName)
}

// WaitForRolledRouters waits for the DaemonSet rollout to complete after an
// operation that triggers pod recreation (e.g. CleanAll), then returns the
// new set of ready routers. oldRouters is the snapshot captured before the
// operation.
func WaitForRolledRouters(cs clientset.Interface, hostMode bool, oldRouters Routers, timeout time.Duration) (Routers, error) {
	var newRouters Routers
	deadline := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return nil, fmt.Errorf("timed out waiting for daemonset rollout after %s", timeout)
		case <-ticker.C:
			r, err := Get(cs, hostMode)
			if err != nil {
				continue
			}
			if err := DaemonsetRolled(oldRouters, r); err != nil {
				continue
			}
			newRouters = r
			return newRouters, nil
		}
	}
}

// DaemonsetRolled checks if routers have been rolled/restarted by comparing old and new state
// For routerPods: checks if pods were deleted and recreated (names changed)
// For routerPodmans: checks if pods were restarted (PIDs changed)
func DaemonsetRolled(oldRouters Routers, newRouters Routers) error {
	// Type assert to determine which type of routers we're dealing with
	switch old := oldRouters.(type) {
	case routerPods:
		new, ok := newRouters.(routerPods)
		if !ok {
			return fmt.Errorf("old routers are routerPods but new routers are %T", newRouters)
		}
		return daemonsetPodRolled(old, new)
	case routerPodmans:
		new, ok := newRouters.(routerPodmans)
		if !ok {
			return fmt.Errorf("old routers are routerPodmans but new routers are %T", newRouters)
		}
		return podmanRolled(old, new)
	default:
		return fmt.Errorf("unknown router type: %T", oldRouters)
	}
}
