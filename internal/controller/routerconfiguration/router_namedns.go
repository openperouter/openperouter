// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"net"

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
	pod     *v1.Pod
}

var _ Router = (*RouterNamedNS)(nil)

func (r *RouterNamedNSProvider) New(ctx context.Context) (Router, error) {
	if err := netnamespace.EnsureNamespace(); err != nil {
		return nil, fmt.Errorf("failed to ensure named netns: %w", err)
	}

	// Pod reference is optional — used only for HandleNonRecoverableError.
	// New() succeeds even if the pod doesn't exist yet (decoupled lifecycle).
	pod, err := routerPodForNode(ctx, r, r.Node)
	if err != nil {
		slog.Info("router pod not found, proceeding without it", "node", r.Node, "error", err)
	}

	return &RouterNamedNS{
		manager: r,
		pod:     pod,
	}, nil
}

func (r *RouterNamedNSProvider) NodeIndex(ctx context.Context) (int, error) {
	return nodeIndexFor(ctx, r.Client, r.Node)
}

func (r *RouterNamedNS) TargetNS(_ context.Context) (string, error) {
	return netnamespace.NamedNSPath, nil
}

func (r *RouterNamedNS) CanReconcile(_ context.Context) (bool, error) {
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

func (r *RouterNamedNS) HandleNonRecoverableError(ctx context.Context) error {
	// Delete the named netns so the next pod starts with a clean namespace
	// rebuilt from scratch by the controller. Without this, the persistent
	// netns retains stale state (e.g. old underlay NIC) causing the new pod
	// to hit the same non-recoverable error in a loop.
	if err := netnamespace.DeleteNamespace(); err != nil {
		slog.Warn("failed to delete named netns during non-recoverable cleanup", "error", err)
	}

	if r.pod == nil {
		slog.Info("no router pod reference, skipping pod deletion")
		return nil
	}
	slog.Info("deleting router pod", "pod", r.pod.Name, "namespace", r.pod.Namespace)
	err := r.manager.Delete(ctx, r.pod)
	if err != nil {
		slog.Error("failed to delete router pod", "error", err)
		return err
	}
	return nil
}
