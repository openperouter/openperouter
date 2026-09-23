// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// DaemonSetRolledOut returns nil when the daemonset rollout is complete,
// mirroring the checks performed by kubectl rollout status.
func DaemonSetRolledOut(cs clientset.Interface, namespace, name string) error {
	ds, err := cs.AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if ds.Spec.UpdateStrategy.Type != appsv1.RollingUpdateDaemonSetStrategyType {
		return fmt.Errorf("daemonset %s/%s: rollout status not watchable for strategy %s", namespace, name, ds.Spec.UpdateStrategy.Type)
	}
	if ds.Generation > ds.Status.ObservedGeneration {
		return fmt.Errorf("daemonset %s/%s: observed generation %d behind generation %d", namespace, name, ds.Status.ObservedGeneration, ds.Generation)
	}
	if ds.Status.UpdatedNumberScheduled < ds.Status.DesiredNumberScheduled {
		return fmt.Errorf("daemonset %s/%s: %d of %d pods updated", namespace, name, ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled)
	}
	if ds.Status.NumberAvailable < ds.Status.DesiredNumberScheduled {
		return fmt.Errorf("daemonset %s/%s: %d of %d pods available", namespace, name, ds.Status.NumberAvailable, ds.Status.DesiredNumberScheduled)
	}
	return nil
}

// DaemonSetPods returns the ready pods of the daemonset, erroring if their
// number does not match the desired number of scheduled pods.
func DaemonSetPods(cs clientset.Interface, namespace, name string) ([]*corev1.Pod, error) {
	ds, err := cs.AppsV1().DaemonSets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	podList, err := cs.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(ds.Spec.Selector),
	})
	if err != nil {
		return nil, err
	}
	var readyPods []*corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if PodIsReady(pod) {
			readyPods = append(readyPods, pod)
		}
	}
	if len(readyPods) != int(ds.Status.DesiredNumberScheduled) {
		return nil, fmt.Errorf("daemonset %s/%s: expected %d ready pods, got %d", namespace, name, ds.Status.DesiredNumberScheduled, len(readyPods))
	}
	return readyPods, nil
}
