// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"

	"github.com/openperouter/openperouter/internal/controller/nodeindex"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netns"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RouterNamedNSProvider struct {
	Node            string
	FRRConfigPath   string
	FRRReloadSocket string
	// CNIRuntime is the node-level environment used to tear down CNI-backed
	// underlay interfaces when the netns is rebuilt.
	CNIRuntime hostnetwork.CNIRuntime
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
	slog.Info("stopping bridge refreshers before netns cleanup")
	bridgerefresh.StopAllVNIs()

	// Delete the CNI-provisioned underlay interfaces (releasing their IPAM
	// allocations) and clear their cached attachments before the netns goes
	// away, so the rebuilt namespace starts from scratch and the next
	// reconcile provisions them again with a fresh CNI ADD.
	if err := hostnetwork.RemoveCNIUnderlay(ctx, r.manager.CNIRuntime); err != nil {
		slog.Warn("failed to delete cni underlay interfaces during non-recoverable cleanup", "error", err)
	}

	// Delete the named netns so the next pod starts with a clean namespace
	// rebuilt from scratch by the controller. Without this, the persistent
	// netns retains stale state (e.g. old underlay NIC) causing the new pod
	// to hit the same non-recoverable error in a loop.
	if err := netnamespace.DeleteNamespace(); err != nil {
		slog.Warn("failed to delete named netns during non-recoverable cleanup", "error", err)
	}

	if path := r.manager.FRRConfigPath; path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove persistent frr config", "path", path, "error", err)
		}
		slog.Info("removed persistent frr config", "path", path)
	}

	if r.pod == nil {
		slog.Info("no router pod reference, skipping pod deletion")
		return nil
	}
	slog.Info("deleting router pod", "pod", r.pod.Name, "namespace", r.pod.Namespace)
	err := r.manager.Delete(ctx, r.pod)
	if err != nil && !apierrors.IsNotFound(err) {
		slog.Error("failed to delete router pod", "error", err)
		return err
	}
	return nil
}

const nodeNameIndex = "spec.NodeName"

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
