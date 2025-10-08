// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"

	"github.com/openperouter/openperouter/internal/controller/nodeindex"
	"github.com/openperouter/openperouter/internal/k8s"
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
	var node v1.Node
	if err := r.Get(ctx, client.ObjectKey{Name: r.Node}, &node); err != nil {
		return 0, fmt.Errorf("failed to get node %s: %w", r.Node, err)
	}
	if node.Annotations == nil {
		return 0, fmt.Errorf("node %s has no annotations", r.Node)
	}
	index, ok := node.Annotations[nodeindex.OpenpeNodeIndex]
	if !ok {
		return 0, fmt.Errorf("node %s has no index annotation", r.Node)
	}
	i, err := strconv.Atoi(index)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index %s: %w", index, err)
	}
	return i, nil
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
	routerPodIsReady := k8s.PodIsReady(r.pod)
	if !routerPodIsReady {
		slog.Info("router pod", "Pod", r.pod.Name, "event", "is not ready, waiting for it to be ready before configuring")
		return false, nil
	}
	return true, nil
}

// routerPodForNode returns the router pod for the given node
func routerPodForNode(ctx context.Context, cli client.Client, node string) (*v1.Pod, error) {
	var pods v1.PodList
	if err := cli.List(ctx, &pods, client.MatchingLabels{"app": "router"},
		client.MatchingFields{
			nodeNameIndex: node,
		}); err != nil {
		return nil, fmt.Errorf("failed to get router pod for node %s: %v", node, err)
	}
	if len(pods.Items) > 1 {
		return nil, fmt.Errorf("more than one router pod found for node %s", node)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no router pods found for node %s", node)
	}
	return &pods.Items[0], nil
}
