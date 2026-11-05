// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"io"
	"iter"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"

	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const routerLabelSelector = "app=router"

var (
	Namespace              = "openperouter-system"
	InstrumentedNodesLabel string
)

type Routers interface {
	Dump(writer io.Writer)
	GetExecutors() iter.Seq[RouterExecutor]
	ExecutorForNode(nodeName string) (RouterExecutor, error)
}

type RouterExecutor interface {
	executor.Executor
	Name() string
	NodeName() string
}

// Get returns routers on nodes matching InstrumentedNodesLabel.
// An empty selector includes all routers.
func Get(cs clientset.Interface, hostMode bool) (Routers, error) {
	if !hostMode {
		pods, err := RouterPods(cs)
		if err != nil {
			return nil, err
		}
		if InstrumentedNodesLabel == "" {
			return routerPods{pods: pods}, nil
		}

		nodes, err := instrumentedNodes(cs)
		if err != nil {
			return nil, err
		}
		pods = k8s.PodsOnNodes(pods, nodes)
		if len(pods) != len(nodes) {
			return nil, fmt.Errorf("expected %d router pods on selected nodes, got %d", len(nodes), len(pods))
		}
		return routerPods{pods: pods}, nil
	}

	nodes, err := instrumentedNodes(cs)
	if err != nil {
		return nil, err
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

// AreReady returns nil when every router in the set is non-terminating and
// ready. In host mode (routerPodmans) it always returns nil because podman
// has no pod-readiness concept.
func AreReady(routers Routers) error {
	rp, ok := routers.(routerPods)
	if !ok {
		return nil
	}
	if !allPodsReady(rp.pods) {
		return fmt.Errorf("not all router pods are non-terminating and ready")
	}
	return nil
}

func RouterPodsForNodes(cs clientset.Interface, nodes []corev1.Node) ([]*corev1.Pod, error) {
	allRouterPods, err := k8s.PodsForLabel(cs, Namespace, routerLabelSelector)
	if err != nil {
		return nil, err
	}
	return k8s.PodsOnNodes(allRouterPods, nodes), nil
}

// ExecutorForNode returns the RouterExecutor running on the given node.
func ExecutorForNode(routers Routers, nodeName string) (RouterExecutor, error) {
	return routers.ExecutorForNode(nodeName)
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

func instrumentedNodes(cs clientset.Interface) ([]corev1.Node, error) {
	nodes, err := k8s.GetNodes(cs)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve nodes: %w", err)
	}
	nodes, err = k8s.NodesWithLabel(nodes, InstrumentedNodesLabel)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes match instrumented node label selector %q", InstrumentedNodesLabel)
	}
	return nodes, nil
}
