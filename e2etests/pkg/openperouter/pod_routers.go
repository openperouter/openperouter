// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"io"
	"iter"
	"slices"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func RouterPods(cs clientset.Interface) ([]*corev1.Pod, error) {
	pods, err := k8s.PodsForLabel(cs, Namespace, routerLabelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve pods %w", err)
	}
	return pods, nil
}

type routerPods struct {
	pods []*corev1.Pod
}

type routerPod struct {
	*corev1.Pod
}

func (r routerPod) Exec(cmd string, args ...string) (string, error) {
	return executor.ForPod(r.Namespace, r.Name(), "frr").Exec(cmd, args...)
}

func (r routerPod) Name() string {
	return r.Pod.Name
}

func (r routerPods) GetExecutors() iter.Seq[RouterExecutor] {
	return func(yield func(RouterExecutor) bool) {
		for _, pod := range r.pods {
			routerPod := routerPod{pod}
			if !yield(routerPod) {
				return
			}
		}
	}
}

func (r routerPods) Dump(writer io.Writer) {
	fmt.Fprintf(writer, "router pods are:")
	for _, pod := range r.pods {
		fmt.Fprintf(writer, "Pod %s/%s: %s", pod.Namespace, pod.Name, pod.Status.Phase)
		fmt.Fprintf(writer, "  Node: %s", pod.Spec.NodeName)
		fmt.Fprintf(writer, "  IPs: %v", pod.Status.PodIPs)
		fmt.Fprintf(writer, "  Containers:")
		for _, c := range pod.Spec.Containers {
			fmt.Fprintf(writer, "    - %s: %s", c.Name, c.Image)
		}
		fmt.Fprint(writer, "\n")
	}
}

func daemonsetPodRolled(oldRouters, newRouters routerPods) error {
	oldPodsNames := []string{}
	for _, p := range oldRouters.pods {
		oldPodsNames = append(oldPodsNames, p.Name)
	}

	if len(newRouters.pods) != len(oldPodsNames) {
		return fmt.Errorf("new pods len %d different from old pods len: %d", len(newRouters.pods), len(oldPodsNames))
	}

	for _, p := range newRouters.pods {
		if slices.Contains(oldPodsNames, p.Name) {
			return fmt.Errorf("old pod %s not deleted yet", p.Name)
		}
		if !k8s.PodIsReady(p) {
			return fmt.Errorf("pod %s is not ready", p.Name)
		}
	}
	return nil
}
