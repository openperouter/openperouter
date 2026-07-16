// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/openperouter/openperouter/internal/controller/nodeindex"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netns"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RouterNamedNSProvider struct {
	Node            string
	FRRConfigPath   string
	FRRReloadSocket string
	client.Client
}

var _ RouterProvider = (*RouterNamedNSProvider)(nil)

type RouterNamedNS struct {
	manager *RouterNamedNSProvider
}

var _ Router = (*RouterNamedNS)(nil)

func (r *RouterNamedNSProvider) New(ctx context.Context) (Router, error) {
	if err := netnamespace.EnsureNamespace(); err != nil {
		return nil, fmt.Errorf("failed to ensure named netns: %w", err)
	}

	return &RouterNamedNS{
		manager: r,
	}, nil
}

func (r *RouterNamedNSProvider) NodeIndex(ctx context.Context) (int, error) {
	var node v1.Node
	if err := r.Get(ctx, client.ObjectKey{Name: r.Node}, &node); err != nil {
		return 0, fmt.Errorf("failed to get node %s: %w", r.Node, err)
	}
	if node.Annotations == nil {
		return 0, fmt.Errorf("node %s has no annotations", node.Name)
	}
	index, ok := node.Annotations[nodeindex.OpenpeNodeIndex]
	if !ok {
		return 0, fmt.Errorf("node %s has no index annotation", node.Name)
	}
	i, err := strconv.Atoi(index)
	if err != nil {
		return 0, fmt.Errorf("failed to parse index %s: %w", index, err)
	}
	return i, nil
}

func (r *RouterNamedNS) TargetNS(_ context.Context) (string, error) {
	return netnamespace.NamedNSPath, nil
}

func (r *RouterNamedNS) CanReconcile() (bool, error) {
	ns, err := netns.GetFromPath(netnamespace.NamedNSPath)
	if err != nil {
		slog.Info("named netns not available", "path", netnamespace.NamedNSPath, "error", err)
		return false, nil
	}
	if err := ns.Close(); err != nil {
		slog.Error("failed to close namespace handle", "error", err)
	}

	if socketPath := r.manager.FRRReloadSocket; socketPath != "" {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			slog.Info("reloader socket not yet available", "socket", socketPath)
			return false, nil
		}
		if err := conn.Close(); err != nil {
			slog.Warn("reloader socket close error", "socket", socketPath, "error", err)
		}
	}

	return true, nil
}

const nodeNameIndex = "spec.NodeName"

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
