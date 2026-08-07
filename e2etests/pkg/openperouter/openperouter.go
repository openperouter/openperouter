// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"context"
	"fmt"
	"io"
	"iter"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	Namespace                    = "openperouter-system"
	RouterDaemonSetLabelSelector = "app.kubernetes.io/component=router"
	routerLabelSelector          = "app=router"
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

// RestartDaemonSetPods restarts all pods that belong to the DaemonSet identified by namespace + label selector and
// waits until the DaemonSet reports ready.
func RestartDaemonSetPods(cs clientset.Interface, namespace, labelSelector string) {
	By(fmt.Sprintf("getting DaemonSet in namespace %s with label selector %s", namespace, labelSelector))
	var ds appsv1.DaemonSet
	var dsName string
	Eventually(func() error {
		dsList, err := cs.AppsV1().DaemonSets(namespace).List(
			context.Background(),
			metav1.ListOptions{
				LabelSelector: labelSelector,
			},
		)
		if err != nil {
			return err
		}
		if len(dsList.Items) != 1 {
			return fmt.Errorf("could not find DaemonSet with label selector %s, unexpected count %d",
				labelSelector, len(dsList.Items))
		}
		ds = dsList.Items[0]
		dsName = ds.Name
		return nil
	}).
		WithTimeout(1 * time.Minute).
		WithPolling(time.Second).
		ShouldNot(HaveOccurred())

	selector := metav1.FormatLabelSelector(ds.Spec.Selector)

	By(fmt.Sprintf("listing DaemonSet %s/%s pods", namespace, dsName))
	var oldPodNames map[string]struct{}
	Eventually(func() error {
		var err error
		oldPodNames, err = collectPodNames(cs, namespace, selector)
		return err
	}).
		WithTimeout(1 * time.Minute).
		WithPolling(time.Second).
		ShouldNot(HaveOccurred())

	By(fmt.Sprintf("deleting DaemonSet %s/%s pods", namespace, dsName))
	Expect(cs.CoreV1().Pods(namespace).DeleteCollection(
		context.Background(),
		metav1.DeleteOptions{},
		metav1.ListOptions{
			LabelSelector: selector,
		},
	)).To(Succeed())

	By(fmt.Sprintf("waiting for all DaemonSet %s/%s pods to change", namespace, dsName))
	Eventually(func() error {
		podNames, err := collectPodNames(cs, namespace, selector)
		if err != nil {
			return err
		}
		for oldName := range oldPodNames {
			if _, oldNameFound := podNames[oldName]; oldNameFound {
				return fmt.Errorf("pod %s hasn't restarted, yet. oldPodNames: %+v, podNames: %+v",
					oldName, oldPodNames, podNames)
			}
		}
		return nil
	}).
		WithTimeout(2 * time.Minute).
		WithPolling(time.Second).
		ShouldNot(HaveOccurred())

	By(fmt.Sprintf("waiting for DaemonSet %s/%s to become ready", namespace, dsName))
	Eventually(func(g Gomega) bool {
		ds, err := cs.AppsV1().DaemonSets(namespace).Get(context.Background(), dsName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		return ds.Status.DesiredNumberScheduled > 0 && ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
	}).
		WithTimeout(2 * time.Minute).
		WithPolling(time.Second).
		MustPassRepeatedly(10).
		Should(BeTrue())
}

func collectPodNames(cs clientset.Interface, namespace, selector string) (map[string]struct{}, error) {
	podList, err := cs.CoreV1().Pods(namespace).List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: selector,
		},
	)
	if err != nil {
		return nil, err
	}
	podNames := make(map[string]struct{}, len(podList.Items))
	for _, pod := range podList.Items {
		podNames[pod.Name] = struct{}{}
	}
	return podNames, nil
}
