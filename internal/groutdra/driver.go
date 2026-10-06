// SPDX-License-Identifier:Apache-2.0

// Package groutdra is a kubelet DRA plugin for grout net_vhost ports.
//
// Contract matches kubevirt/vhostuser-network-binding-plugin + ovsdpdk DRA:
// KEP-5304 metadata attribute vhost-user-path (in-container socket), CDI
// bind-mount of the socket dir, grout DPDK client / QEMU libvirt server.
package groutdra

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"

	"github.com/openperouter/openperouter/internal/grout"
)

type Driver struct {
	grout *grout.Client

	mu      sync.Mutex
	pending map[types.UID]context.CancelFunc
}

func New(groutSock string) *Driver {
	return &Driver{
		grout:   grout.NewClient(groutSock),
		pending: make(map[types.UID]context.CancelFunc),
	}
}

func PoolResources() resourceslice.DriverResources {
	devices := make([]resourceapi.Device, 0, MaxDevices)
	for i := 0; i < MaxDevices; i++ {
		devices = append(devices, resourceapi.Device{
			Name: fmt.Sprintf("vhu-%d", i),
		})
	}
	return resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			PoolName: {
				Slices: []resourceslice.Slice{{Devices: devices}},
			},
		},
	}
}

func (d *Driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	out := make(map[types.UID]kubeletplugin.PrepareResult, len(claims))
	for _, claim := range claims {
		out[claim.UID] = d.prepare(ctx, claim)
	}
	return out, nil
}

func (d *Driver) prepare(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	slog.InfoContext(ctx, "prepare grout vhost claim", "claim", claim.Name, "uid", claim.UID)
	if claim.Status.Allocation == nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s has no allocation", claim.Name)}
	}

	var devices []kubeletplugin.Device
	for _, result := range claim.Status.Allocation.Devices.Results {
		if result.Driver != DriverName {
			continue
		}
		dev, err := d.prepareOne(ctx, claim, result)
		if err != nil {
			return kubeletplugin.PrepareResult{Err: err}
		}
		devices = append(devices, dev)
	}
	if len(devices) == 0 {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s allocated no %s devices", claim.Name, DriverName)}
	}
	return kubeletplugin.PrepareResult{Devices: devices}
}

func (d *Driver) prepareOne(ctx context.Context, claim *resourceapi.ResourceClaim, result resourceapi.DeviceRequestAllocationResult) (kubeletplugin.Device, error) {
	uid := claim.UID
	request := result.Request
	dir := ClaimDir(uid, request)
	sock := SocketPath(uid, request)
	name := PortName(uid)
	podSock := PodSocketPath(request)
	podMount := PodMountPath(request)

	if err := os.MkdirAll(dir, 0o777); err != nil {
		return kubeletplugin.Device{}, err
	}
	_ = os.Chmod(dir, 0o777)
	_ = os.Chown(dir, QEMUUID, QEMUGID)
	podClaimName := claim.Annotations["resource.kubernetes.io/pod-claim-name"]
	if podClaimName != "" {
		if err := writeKubeVirtMetadata(claim, result, podClaimName, PodSocketPath(request)); err != nil {
			return kubeletplugin.Device{}, err
		}
	}

	// QEMU binds the socket (server). Creating grout client=1 before that
	// is a one-shot ENOENT on grout 0.17. Do not CreateVhostPort until the socket exists.
	d.startClaimWorkers(uid, name, sock, claim.Namespace, claim.Name, podClaimName, request)

	cdiID, err := writeCDISpec(uid, dir, podMount)
	if err != nil {
		d.cancelPending(uid)
		return kubeletplugin.Device{}, err
	}

	path := podSock
	return kubeletplugin.Device{
		Requests:     []string{request},
		PoolName:     result.Pool,
		DeviceName:   result.Device,
		CDIDeviceIDs: []string{cdiID},
		Metadata: &kubeletplugin.DeviceMetadata{
			Attributes: map[string]resourceapi.DeviceAttribute{
				VhostPathAttr: {StringValue: &path},
			},
		},
	}, nil
}

// writeKubeVirtMetadata publishes the same KEP-5304 payload synchronously for
// virt-handler. This avoids a race with kubeletplugin's asynchronous metadata
// persistence while retaining that standard path for ordinary Kubernetes Pods.
func writeKubeVirtMetadata(claim *resourceapi.ResourceClaim, result resourceapi.DeviceRequestAllocationResult, podClaimName, socketPath string) error {
	target := KubeVirtMetadataProjectionPath(podClaimName, result.Request)
	payload := map[string]any{
		"kind":         "DeviceMetadata",
		"apiVersion":   "metadata.resource.k8s.io/v1alpha1",
		"metadata":     map[string]any{"name": claim.Name, "namespace": claim.Namespace, "uid": claim.UID, "generation": 1},
		"podClaimName": podClaimName,
		"requests": []any{map[string]any{"name": result.Request, "devices": []any{map[string]any{
			"driver": result.Driver, "pool": result.Pool, "name": result.Device,
			"attributes": map[string]any{VhostPathAttr: map[string]string{"string": socketPath}},
		}}}},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func (d *Driver) startClaimWorkers(uid types.UID, portName, sock, namespace, claimName, podClaimName, request string) {
	d.cancelPending(uid)
	ctx, cancel := context.WithCancel(context.Background())
	d.mu.Lock()
	d.pending[uid] = cancel
	d.mu.Unlock()

	go func() {
		if err := d.attachClientUntilRunning(ctx, uid, portName, sock); err != nil {
			slog.Info("stop attaching grout vhost client", "uid", uid, "err", err)
		}
	}()
	if podClaimName != "" {
		go func() {
			if err := projectKubeVirtMetadata(ctx, namespace, claimName, podClaimName, request); err != nil {
				slog.Info("stop projecting KubeVirt DRA metadata", "claim", claimName, "err", err)
			}
		}()
	}
}

// projectKubeVirtMetadata bridges the kubeletplugin metadata stream to the
// host path consumed by virt-handler. It is intentionally additive: vanilla
// Kubernetes continues to use the standard CDI metadata projection, while
// OpenShift Virtualization can read the same stream before it launches QEMU.
func projectKubeVirtMetadata(ctx context.Context, namespace, claimName, podClaimName, request string) error {
	source := MetadataSourcePath(namespace, claimName, request)
	target := KubeVirtMetadataProjectionPath(podClaimName, request)
	for {
		data, err := os.ReadFile(source)
		if err == nil {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			tmp := target + ".tmp"
			if err := os.WriteFile(tmp, data, 0o644); err != nil {
				return err
			}
			return os.Rename(tmp, target)
		}
		if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(socketPollInterval):
		}
	}
}

func (d *Driver) attachClientUntilRunning(ctx context.Context, uid types.UID, portName, sock string) error {
	slog.Info("waiting for qemu vhost socket", "uid", uid, "sock", sock)
	if err := waitForUnixSocket(ctx, sock); err != nil {
		return err
	}
	params := grout.VhostPortParams{
		Name:        portName,
		SocketPath:  sock,
		Queues:      1,
		GatewayCIDR: grout.VhostGuestGatewayCIDR,
		Client:      true,
	}
	_ = os.Chmod(sock, 0o777)
	slog.Info("creating grout client port", "uid", uid, "name", portName)
	if err := d.grout.CreateVhostPort(ctx, params); err != nil {
		return fmt.Errorf("CreateVhostPort after socket ready: %w", err)
	}
	ticker := time.NewTicker(socketPollInterval)
	defer ticker.Stop()
	for {
		running, err := d.grout.PortIsRunning(ctx, portName)
		if err != nil {
			slog.Error("PortIsRunning", "name", portName, "err", err)
		} else if running {
			slog.Info("grout vhost client running", "uid", uid, "name", portName)
			return nil
		} else {
			slog.Info("grout vhost port not running yet; waiting for grout", "uid", uid, "name", portName)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *Driver) cancelPending(uid types.UID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.pending[uid]; ok {
		c()
		delete(d.pending, uid)
	}
}

func (d *Driver) UnprepareResourceClaims(ctx context.Context, claims []kubeletplugin.NamespacedObject) (map[types.UID]error, error) {
	out := make(map[types.UID]error, len(claims))
	for _, claim := range claims {
		out[claim.UID] = d.unprepare(ctx, claim.UID)
	}
	return out, nil
}

func (d *Driver) unprepare(ctx context.Context, uid types.UID) error {
	slog.InfoContext(ctx, "unprepare grout vhost claim", "uid", uid)
	d.cancelPending(uid)
	name := PortName(uid)
	if err := d.grout.DeleteVhostPort(ctx, name, ""); err != nil {
		slog.WarnContext(ctx, "DeleteVhostPort", "err", err)
	}
	removeCDISpec(uid)
	_ = os.RemoveAll(filepath.Join(HostVhostRoot, string(uid)))
	return nil
}

func (d *Driver) HandleError(ctx context.Context, err error, msg string) {
	slog.ErrorContext(ctx, msg, "err", err)
}
