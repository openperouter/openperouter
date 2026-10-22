// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"io"
	"iter"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"

	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const routerLabelSelector = "app=router"

var Namespace = "openperouter-system"

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

func Get(cs clientset.Interface, hostMode bool) (Routers, error) {
	if !hostMode {
		pods, err := k8s.PodsForLabel(cs, Namespace, routerLabelSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve pods %w", err)
		}
		return routerPods{pods: pods}, nil
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

// AreReadyOnNodes is like AreReady for router pods on the given node names.
func AreReadyOnNodes(cs clientset.Interface, nodeNames map[string]bool) error {
	pods, err := RouterPodsForNodes(cs, nodeNames)
	if err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, p := range pods {
		covered[p.Spec.NodeName] = true
	}
	missing := []string{}
	for node, selected := range nodeNames {
		if !selected || covered[node] {
			continue
		}
		missing = append(missing, node)
	}
	if len(missing) > 0 {
		return fmt.Errorf("no router pod on selected nodes: %s", strings.Join(missing, ", "))
	}
	notReady := []string{}
	for _, p := range pods {
		if p.DeletionTimestamp != nil || !k8s.PodIsReady(p) {
			notReady = append(notReady, fmt.Sprintf("%s/%s", p.Spec.NodeName, p.Name))
		}
	}
	if len(notReady) > 0 {
		return fmt.Errorf("router pods not ready on selected nodes: %s", strings.Join(notReady, ", "))
	}
	return nil
}

func RouterPodsForNodes(cs clientset.Interface, nodes map[string]bool) ([]*corev1.Pod, error) {
	routerPods, err := k8s.PodsForLabel(cs, Namespace, routerLabelSelector)
	if err != nil {
		return nil, err
	}
	filteredRouterPods := []*corev1.Pod{}
	for _, p := range routerPods {
		if nodes[p.Spec.NodeName] {
			filteredRouterPods = append(filteredRouterPods, p)
		}
	}
	return filteredRouterPods, nil
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
