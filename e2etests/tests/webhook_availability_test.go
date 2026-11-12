// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"slices"
	"testing"

	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWebhookReplacementReadiness(t *testing.T) {
	survivor := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "survivor",
			Namespace: openperouter.Namespace,
			Labels:    map[string]string{"app": "webhook"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			},
		},
	}
	replacement := survivor.DeepCopy()
	replacement.Name = "replacement"
	replacement.Status = corev1.PodStatus{Phase: corev1.PodPending}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "webhook", Namespace: openperouter.Namespace, Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: new(int32(2))},
		// Pod deletion does not change the Deployment generation, and its
		// status can still count the deleted replica as ready.
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           2,
			UpdatedReplicas:    2,
			ReadyReplicas:      2,
			AvailableReplicas:  2,
		},
	}
	cs := fake.NewClientset(deployment, survivor, replacement)
	if err := k8s.DeploymentRolledOut(cs, openperouter.Namespace, deployment.Name); err != nil {
		t.Fatalf("expected stale Deployment status to pass the rollout check: %v", err)
	}
	if _, err := readyWebhookPodNames(cs); err == nil || err.Error() != `webhook pod "replacement" is not ready` {
		t.Fatalf("expected the pending replacement to prevent recovery, got %v", err)
	}

	replacement.Status = survivor.Status
	if _, err := cs.CoreV1().Pods(openperouter.Namespace).UpdateStatus(
		context.Background(), replacement, metav1.UpdateOptions{},
	); err != nil {
		t.Fatal(err)
	}
	names, err := readyWebhookPodNames(cs)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	want := []string{"replacement", "survivor"}
	if !slices.Equal(names, want) {
		t.Fatalf("ready webhook pods = %v, want %v", names, want)
	}
}
