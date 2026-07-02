// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const controllerLabelSelector = "app=controller"

func ControllerPods(cs clientset.Interface) ([]*corev1.Pod, error) {
	pods, err := k8s.PodsForLabel(cs, Namespace, controllerLabelSelector)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve controller pods: %w", err)
	}
	return pods, nil
}
