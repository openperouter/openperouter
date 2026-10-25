// SPDX-License-Identifier:Apache-2.0

// Package groutdra is a kubelet DRA plugin for grout net_vhost ports.
//
// Contract matches kubevirt/vhostuser-network-binding-plugin + ovsdpdk DRA:
// KEP-5304 metadata attribute vhost-user-path (in-container socket), CDI
// bind-mount of the socket dir, grout DPDK client / QEMU libvirt server.
package groutdra

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"

	"github.com/openperouter/openperouter/internal/groutdra/poc"
)

type Driver struct {
	grout      portManager
	paths      pathLayout
	prepareDir func(string) error
	devices    DeviceCatalog

	opMu    sync.Mutex
	mu      sync.Mutex
	pending map[types.UID]*claimWorker
}

type claimWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// DeviceCatalog keeps inventory and per-device network setup together.
// A production catalog can discover devices and supply their network settings.
type DeviceCatalog interface {
	Resources() resourceslice.DriverResources
	GatewayCIDR(string) (string, error)
}

func (d *Driver) WatchHealthStatus(context.Context, chan<- kubeletplugin.DeviceHealthReport) error {
	return kubeletplugin.ErrHealthNotSupported
}

func New(groutSock string) *Driver {
	return NewWithCatalog(groutSock, poc.New(DriverName, PoolName))
}

// NewWithCatalog uses the supplied device inventory and gateway mapping.
// Production can supply a discovery-backed catalog; New uses the POC catalog.
func NewWithCatalog(groutSock string, catalog DeviceCatalog) *Driver {
	return &Driver{
		grout:      apiPorts{socketPath: groutSock},
		paths:      defaultPaths,
		prepareDir: prepareVhostDir,
		devices:    catalog,
		pending:    make(map[types.UID]*claimWorker),
	}
}

func (d *Driver) PoolResources() resourceslice.DriverResources {
	return d.devices.Resources()
}

// RecoverPreparedClaims resumes port attachment for claims that were prepared
// before this driver process started. Kubelet may not call Prepare again after
// a plugin restart.
func (d *Driver) RecoverPreparedClaims(ctx context.Context) error {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	entries, err := os.ReadDir(d.paths.hostRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading prepared claim directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		uid := types.UID(entry.Name())
		state, err := loadClaimState(d.paths, uid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := d.startClaimWorkers(ctx, state); err != nil {
			return err
		}
		slog.InfoContext(ctx, "resumed prepared grout claim", "uid", uid)
	}
	return nil
}

func (d *Driver) PrepareResourceClaims(ctx context.Context, claims []*resourceapi.ResourceClaim) (map[types.UID]kubeletplugin.PrepareResult, error) {
	out := make(map[types.UID]kubeletplugin.PrepareResult, len(claims))
	for _, claim := range claims {
		out[claim.UID] = d.prepare(ctx, claim)
	}
	return out, nil
}

func (d *Driver) prepare(ctx context.Context, claim *resourceapi.ResourceClaim) kubeletplugin.PrepareResult {
	d.opMu.Lock()
	defer d.opMu.Unlock()
	slog.InfoContext(ctx, "prepare grout vhost claim", "claim", claim.Name, "uid", claim.UID)
	if claim.Status.Allocation == nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s has no allocation", claim.Name)}
	}

	state := claimState{
		UID: claim.UID, Namespace: claim.Namespace, Name: claim.Name,
		PodClaimName: claim.Annotations["resource.kubernetes.io/pod-claim-name"],
	}
	if state.PodClaimName != "" && (filepath.Base(state.PodClaimName) != state.PodClaimName || !filepath.IsLocal(state.PodClaimName)) {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("invalid pod claim name %q", state.PodClaimName)}
	}
	seen := make(map[string]bool)
	deviceNames := make(map[string]bool)
	requestCounts := make(map[string]int)
	for _, result := range claim.Status.Allocation.Devices.Results {
		if result.Driver != DriverName {
			continue
		}
		if result.Request == "" || !filepath.IsLocal(result.Request) {
			return kubeletplugin.PrepareResult{Err: fmt.Errorf("invalid request %q", result.Request)}
		}
		if deviceNames[result.Device] {
			return kubeletplugin.PrepareResult{Err: fmt.Errorf("duplicate device %q", result.Device)}
		}
		deviceNames[result.Device] = true
		requestCounts[result.Request]++
		gatewayCIDR, err := d.devices.GatewayCIDR(result.Device)
		if err != nil {
			return kubeletplugin.PrepareResult{Err: err}
		}
		state.Allocations = append(state.Allocations, allocationState{
			Request: result.Request, Device: result.Device, Pool: result.Pool, GatewayCIDR: gatewayCIDR,
		})
	}
	if len(state.Allocations) == 0 {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s allocated no %s devices", claim.Name, DriverName)}
	}
	for i := range state.Allocations {
		allocation := &state.Allocations[i]
		allocation.Key = allocation.Request
		if requestCounts[allocation.Request] > 1 {
			allocation.Key += "/" + allocation.Device
		}
	}
	slices.SortFunc(state.Allocations, func(a, b allocationState) int {
		if order := cmp.Compare(a.Request, b.Request); order != 0 {
			return order
		}
		return cmp.Compare(a.Device, b.Device)
	})
	previous, err := loadClaimState(d.paths, claim.UID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return kubeletplugin.PrepareResult{Err: err}
	}
	newState := errors.Is(err, os.ErrNotExist)
	if !newState && !reflect.DeepEqual(previous, state) {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("claim %s allocation changed while resources are prepared", claim.Name)}
	}
	if newState {
		if err := saveClaimState(d.paths, state); err != nil {
			return kubeletplugin.PrepareResult{Err: err}
		}
	}
	devices := make([]kubeletplugin.Device, 0, len(state.Allocations))
	for _, allocation := range state.Allocations {
		dev, err := stageAllocation(d.paths, d.prepareDir, state, allocation)
		if err != nil {
			if newState {
				err = errors.Join(err, cleanupLocal(d.paths, state))
			}
			return kubeletplugin.PrepareResult{Err: err}
		}
		devices = append(devices, dev)
	}
	if state.PodClaimName != "" {
		for _, allocation := range state.Allocations {
			if seen[allocation.Request] {
				continue
			}
			seen[allocation.Request] = true
			if err := writeKubeVirtMetadata(d.paths, state, allocation.Request); err != nil {
				if newState {
					err = errors.Join(err, cleanupLocal(d.paths, state))
				}
				return kubeletplugin.PrepareResult{Err: err}
			}
		}
	}
	if err := d.startClaimWorkers(ctx, state); err != nil {
		return kubeletplugin.PrepareResult{Err: err}
	}
	return kubeletplugin.PrepareResult{Devices: devices}
}

func stageAllocation(paths pathLayout, prepareDir func(string) error, state claimState, allocation allocationState) (kubeletplugin.Device, error) {
	uid := state.UID
	key := allocation.Key
	dir := paths.ClaimDir(uid, key)
	podSock := PodSocketPath(key)
	podMount := PodMountPath(key)

	if err := prepareDir(dir); err != nil {
		return kubeletplugin.Device{}, err
	}
	cdiID, err := writeCDISpec(paths, uid, key, dir, podMount)
	if err != nil {
		return kubeletplugin.Device{}, err
	}

	path := podSock
	// TRex initializes its DPDK ports with an MTU above 1500. Advertise a
	// jumbo-capable vhost MTU so the virtio frontend accepts that setup; the
	// benchmark traffic itself remains 1518 bytes or smaller.
	mtu := int64(VhostMTU)
	return kubeletplugin.Device{
		Requests:     []string{allocation.Request},
		PoolName:     allocation.Pool,
		DeviceName:   allocation.Device,
		CDIDeviceIDs: []string{cdiID},
		Metadata: &kubeletplugin.DeviceMetadata{
			Attributes: map[string]resourceapi.DeviceAttribute{
				VhostPathAttr: {StringValue: &path},
				"mtu":         {IntValue: &mtu},
			},
		},
	}, nil
}

func prepareVhostDir(dir string) error {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		return fmt.Errorf("chmod vhost directory: %w", err)
	}
	if err := os.Chown(dir, QEMUUID, QEMUGID); err != nil {
		return fmt.Errorf("chown vhost directory: %w", err)
	}
	if err := labelVhostDir(dir); err != nil {
		return err
	}
	return nil
}

// labelVhostDir makes the CDI-mounted socket directory writable by QEMU on
// SELinux enforcing nodes. Without this, host tmpfs directories inherit
// container_var_run_t and QEMU's container_t domain is denied bind(2).
// Do not probe /sys/fs/selinux/enforce: a privileged driver Pod can modify the
// label on its hostPath even when selinuxfs is not mounted in that Pod. Instead
// attempt the xattr directly and treat an unsupported SELinux xattr as the
// normal non-SELinux (vanilla Kubernetes) case.
func labelVhostDir(dir string) error {
	if err := unix.Setxattr(dir, "security.selinux", []byte("system_u:object_r:container_file_t:s0"), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			return nil
		}
		return fmt.Errorf("label vhost directory %s: %w", dir, err)
	}
	return nil
}

// writeKubeVirtMetadata publishes the same KEP-5304 payload synchronously for
// virt-handler. This avoids a race with kubeletplugin's asynchronous metadata
// persistence while retaining that standard path for ordinary Kubernetes Pods.
func writeKubeVirtMetadata(paths pathLayout, state claimState, request string) error {
	target := paths.KubeVirtMetadataProjectionPath(state.PodClaimName, request)
	devices := make([]any, 0, len(state.Allocations))
	for _, allocation := range state.Allocations {
		if allocation.Request != request {
			continue
		}
		devices = append(devices, map[string]any{
			"driver": DriverName, "pool": allocation.Pool, "name": allocation.Device,
			"attributes": map[string]any{
				VhostPathAttr: map[string]string{"string": PodSocketPath(allocation.Key)},
				"mtu":         map[string]int64{"int": VhostMTU},
			},
		})
	}
	payload := map[string]any{
		"kind":         "DeviceMetadata",
		"apiVersion":   "metadata.resource.k8s.io/v1alpha1",
		"metadata":     map[string]any{"name": state.Name, "namespace": state.Namespace, "uid": state.UID, "generation": 1},
		"podClaimName": state.PodClaimName,
		"requests":     []any{map[string]any{"name": request, "devices": devices}},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return writeMetadataFile(target, data)
}

func (d *Driver) startClaimWorkers(ctx context.Context, state claimState) error {
	if err := d.stopClaimWorkers(ctx, state.UID); err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	w := &claimWorker{cancel: cancel, done: make(chan struct{})}
	d.mu.Lock()
	d.pending[state.UID] = w
	d.mu.Unlock()
	go func() {
		defer close(w.done)
		var wg sync.WaitGroup
		projected := make(map[string]bool)
		for _, allocation := range state.Allocations {
			wg.Add(1)
			go func() {
				defer wg.Done()
				d.attachClientUntilRunning(workerCtx, state.UID, PortName(state.UID, allocation.Key),
					d.paths.SocketPath(state.UID, allocation.Key), allocation.GatewayCIDR)
			}()
			if state.PodClaimName != "" && !projected[allocation.Request] {
				projected[allocation.Request] = true
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := projectKubeVirtMetadata(workerCtx, d.paths, state.Namespace, state.Name,
						state.PodClaimName, allocation.Request); err != nil && !errors.Is(err, context.Canceled) {
						slog.Error("projecting KubeVirt metadata", "claim", state.Name, "err", err)
					}
				}()
			}
		}
		wg.Wait()
		d.mu.Lock()
		if d.pending[state.UID] == w {
			delete(d.pending, state.UID)
		}
		d.mu.Unlock()
	}()
	return nil
}

// projectKubeVirtMetadata bridges the kubeletplugin metadata stream to the
// host path consumed by virt-handler. It is intentionally additive: vanilla
// Kubernetes continues to use the standard CDI metadata projection, while
// OpenShift Virtualization can read the same stream before it launches QEMU.
func projectKubeVirtMetadata(ctx context.Context, paths pathLayout, namespace, claimName, podClaimName, request string) error {
	source := paths.MetadataSourcePath(namespace, claimName, request)
	target := paths.KubeVirtMetadataProjectionPath(podClaimName, request)
	for {
		data, err := os.ReadFile(source)
		if err == nil {
			return writeMetadataFile(target, data)
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

func writeMetadataFile(target string, data []byte) error {
	return writeAtomic(target, data, 0o644)
}

func (d *Driver) attachClientUntilRunning(ctx context.Context, uid types.UID, portName, sock, gatewayCIDR string) {
	slog.Info("waiting for qemu vhost socket", "uid", uid, "sock", sock)
	if err := waitForUnixSocket(ctx, sock); err != nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := os.Chmod(sock, 0o777); err != nil {
			slog.Warn("chmod qemu socket", "path", sock, "err", err)
		} else if err := d.grout.Create(ctx, portName, sock, gatewayCIDR); err != nil {
			slog.Warn("creating grout vhost client", "name", portName, "err", err)
		} else if running, err := d.grout.Running(ctx, portName); err != nil {
			slog.Warn("checking grout port state", "name", portName, "err", err)
		} else if running {
			slog.Info("grout vhost client running", "uid", uid, "name", portName)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Driver) stopClaimWorkers(ctx context.Context, uid types.UID) error {
	d.mu.Lock()
	w := d.pending[uid]
	d.mu.Unlock()
	if w == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for claim worker %s: %w", uid, ctx.Err())
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
	d.opMu.Lock()
	defer d.opMu.Unlock()
	slog.InfoContext(ctx, "unprepare grout vhost claim", "uid", uid)
	if err := d.stopClaimWorkers(ctx, uid); err != nil {
		return err
	}
	state, err := loadClaimState(d.paths, uid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, allocation := range state.Allocations {
		if err := d.grout.Delete(ctx, PortName(uid, allocation.Key)); err != nil {
			return fmt.Errorf("deleting grout port for %s: %w", allocation.Request, err)
		}
	}
	return cleanupLocal(d.paths, state)
}

func cleanupLocal(paths pathLayout, state claimState) error {
	var errs []error
	removedMetadata := make(map[string]bool)
	for _, allocation := range state.Allocations {
		if err := removeCDISpec(paths, state.UID, allocation.Key); err != nil {
			errs = append(errs, fmt.Errorf("removing CDI spec for %s: %w", allocation.Request, err))
		}
		if state.PodClaimName != "" && !removedMetadata[allocation.Request] {
			removedMetadata[allocation.Request] = true
			path := paths.KubeVirtMetadataProjectionPath(state.PodClaimName, allocation.Request)
			if err := removeMetadataIfOwned(state.UID, path); err != nil {
				errs = append(errs, fmt.Errorf("removing metadata for %s: %w", allocation.Request, err))
			}
		}
	}
	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	return os.RemoveAll(filepath.Join(paths.hostRoot, string(state.UID)))
}

func (d *Driver) HandleError(ctx context.Context, err error, msg string) {
	slog.ErrorContext(ctx, msg, "err", err)
}
