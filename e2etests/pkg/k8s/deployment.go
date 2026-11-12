// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
)

const restartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// RestartDeployment triggers a rollout of the deployment, as kubectl rollout
// restart does.
func RestartDeployment(cs clientset.Interface, namespace, name string) error {
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`,
		restartedAtAnnotation, time.Now().Format(time.RFC3339Nano))
	_, err := cs.AppsV1().Deployments(namespace).Patch(context.Background(), name,
		types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("restarting deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

// DeploymentRolledOut returns nil when the deployment rollout is complete,
// mirroring the checks performed by kubectl rollout status.
func DeploymentRolledOut(cs clientset.Interface, namespace, name string) error {
	deployment, err := cs.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	status := deployment.Status
	if deployment.Generation > status.ObservedGeneration {
		return fmt.Errorf("deployment %s/%s: observed generation %d behind generation %d",
			namespace, name, status.ObservedGeneration, deployment.Generation)
	}
	if status.UpdatedReplicas < desired {
		return fmt.Errorf("deployment %s/%s: %d of %d replicas updated", namespace, name, status.UpdatedReplicas, desired)
	}
	if status.Replicas > status.UpdatedReplicas {
		return fmt.Errorf("deployment %s/%s: %d old replicas pending termination",
			namespace, name, status.Replicas-status.UpdatedReplicas)
	}
	if status.AvailableReplicas < status.UpdatedReplicas {
		return fmt.Errorf("deployment %s/%s: %d of %d updated replicas available",
			namespace, name, status.AvailableReplicas, status.UpdatedReplicas)
	}
	return nil
}
