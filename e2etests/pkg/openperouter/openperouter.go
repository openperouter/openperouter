// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"slices"

	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	corev1 "k8s.io/api/core/v1"

	clientset "k8s.io/client-go/kubernetes"
)

const (
	Namespace           = "openperouter-system"
	routerLabelSelector = "app=router"
)

func RouterPods(cs clientset.Interface) ([]*corev1.Pod, error) {
	return k8s.PodsForLabel(cs, Namespace, routerLabelSelector)
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

func DaemonsetRolled(cs clientset.Interface, oldRouterPods []*corev1.Pod) error {
	oldPodsNames := []string{}
	nodes := map[string]bool{}
	for _, p := range oldRouterPods {
		nodes[p.Spec.NodeName] = true
		oldPodsNames = append(oldPodsNames, p.Name)
	}
	routerPods, err := RouterPodsForNodes(cs, nodes)
	if err != nil {
		return err
	}

	if len(routerPods) != len(oldPodsNames) {
		return fmt.Errorf("new pods len %d different from old pods len: %d", len(routerPods), len(oldPodsNames))
	}

	for _, p := range routerPods {
		if slices.Contains(oldPodsNames, p.Name) {
			return fmt.Errorf("old pod %s not deleted yet", p.Name)
		}
		if !k8s.PodIsReady(p) {
			return fmt.Errorf("pod %s is not ready", p.Name)
		}
	}
	return nil
}
