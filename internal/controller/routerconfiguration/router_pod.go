// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/openperouter/openperouter/internal/pods"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const nodeNameIndex = "spec.NodeName"

type RouterPodProvider struct {
	PodRuntime    *pods.Runtime
	Node          string
	FRRConfigPath string
	client.Client
}

var _ RouterProvider = (*RouterPodProvider)(nil)

type RouterPod struct {
	manager *RouterPodProvider
	pod     *v1.Pod
}

var _ Router = (*RouterPod)(nil)

func (r *RouterPodProvider) New(ctx context.Context) (Router, error) {
	routerPod, err := routerPodForNode(ctx, r, r.Node)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch router pod for node %s: %w", r.Node, err)
	}

	return &RouterPod{
		manager: r,
		pod:     routerPod,
	}, nil
}

func (r *RouterPodProvider) NodeIndex(ctx context.Context) (int, error) {
	return nodeIndexFor(ctx, r.Client, r.Node)
}

func (r *RouterPod) TargetNS(ctx context.Context) (string, error) {
	targetNS, err := r.manager.PodRuntime.NetworkNamespace(ctx, string(r.pod.UID))
	if err != nil {
		return "", fmt.Errorf("failed to retrieve namespace for pod %s: %w", r.pod.UID, err)
	}
	res := filepath.Join("/run/netns", targetNS)
	return res, nil
}

func (r *RouterPod) HandleNonRecoverableError(ctx context.Context) error {
	slog.Info("deleting router pod", "pod", r.pod.Name, "namespace", r.pod.Namespace)
	err := r.manager.Delete(ctx, r.pod)
	if err != nil {
		slog.Error("failed to delete router pod", "error", err)
		return err
	}
	return nil
}

func (r *RouterPod) CanReconcile(ctx context.Context) (bool, error) {
	routerPodIsReady := PodIsReady(r.pod)
	if !routerPodIsReady {
		slog.Info("router pod", "Pod", r.pod.Name, "event", "is not ready, waiting for it to be ready before configuring")
		return false, nil
	}
	return true, nil
}

// routerPodForNode returns the non-terminating router pod for the given node.
// Pods with DeletionTimestamp set are filtered out so that the brief overlap
// between a dying pod and its DaemonSet replacement is not treated as an error.
func routerPodForNode(ctx context.Context, cli client.Client, node string) (*v1.Pod, error) {
	var pods v1.PodList
	if err := cli.List(ctx, &pods, client.MatchingLabels{"app": "router"},
		client.MatchingFields{
			nodeNameIndex: node,
		}); err != nil {
		return nil, fmt.Errorf("failed to get router pod for node %s: %v", node, err)
	}
	active := make([]v1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp == nil {
			active = append(active, pods.Items[i])
		}
	}
	if len(active) > 1 {
		return nil, fmt.Errorf("more than one router pod found for node %s", node)
	}
	if len(active) == 0 {
		return nil, fmt.Errorf("no router pods found for node %s", node)
	}
	return &active[0], nil
}

// PodIsReady returns true only when the pod is not terminating and both its
// PodReady and ContainersReady conditions are True. A pod in the termination
// grace period still reports Ready=True, so DeletionTimestamp must be checked.
func PodIsReady(p *v1.Pod) bool {
	if p == nil || p.DeletionTimestamp != nil {
		return false
	}
	return podConditionStatus(p, v1.PodReady) == v1.ConditionTrue && podConditionStatus(p, v1.ContainersReady) == v1.ConditionTrue
}

// podConditionStatus returns the status of the condition for a given pod.
func podConditionStatus(p *v1.Pod, condition v1.PodConditionType) v1.ConditionStatus {
	if p == nil {
		return v1.ConditionUnknown
	}

	for _, c := range p.Status.Conditions {
		if c.Type == condition {
			return c.Status
		}
	}

	return v1.ConditionUnknown
}
