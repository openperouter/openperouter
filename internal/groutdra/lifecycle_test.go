// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type fakePorts struct {
	mu          sync.Mutex
	deleted     []string
	deleteError error
	createCount int
	createError error
}

func (f *fakePorts) Create(context.Context, string, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCount++
	if f.createCount == 1 {
		return f.createError
	}
	return nil
}

func (*fakePorts) Running(context.Context, string) (bool, error) { return true, nil }

func (f *fakePorts) Delete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteError != nil {
		return f.deleteError
	}
	f.deleted = append(f.deleted, name)
	return nil
}

func testDriver(t *testing.T, ports *fakePorts) *Driver {
	t.Helper()
	root := t.TempDir()
	d := New("")
	d.grout = ports
	d.paths = pathLayout{
		hostRoot: filepath.Join(root, "vhost"), cdiDir: filepath.Join(root, "cdi"),
		metadataRoot: filepath.Join(root, "metadata"), pluginRoot: filepath.Join(root, "plugin"),
	}
	d.prepareDir = func(path string) error { return os.MkdirAll(path, 0o700) }
	return d
}

func testClaim(results ...resourceapi.DeviceRequestAllocationResult) *resourceapi.ResourceClaim {
	return &resourceapi.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{
			UID: "claim-123", Namespace: "default", Name: "claim", Annotations: map[string]string{
				"resource.kubernetes.io/pod-claim-name": "vm-network",
			},
		},
		Status: resourceapi.ResourceClaimStatus{Allocation: &resourceapi.AllocationResult{
			Devices: resourceapi.DeviceAllocationResult{Results: results},
		}},
	}
}

func testAllocation(request, device string) resourceapi.DeviceRequestAllocationResult {
	return resourceapi.DeviceRequestAllocationResult{
		Request: request, Driver: DriverName, Pool: PoolName, Device: device,
	}
}

func TestPrepareMultipleDevicesAndRestartCleanup(t *testing.T) {
	ports := &fakePorts{}
	d := testDriver(t, ports)
	claim := testClaim(testAllocation("net", "vhu-0"), testAllocation("net", "vhu-1"),
		testAllocation("other", "vhu-2"))
	result := d.prepare(context.Background(), claim)
	require.NoError(t, result.Err)
	require.Len(t, result.Devices, 3)
	require.NotEqual(t, result.Devices[0].CDIDeviceIDs, result.Devices[1].CDIDeviceIDs)
	state, err := loadClaimState(d.paths, claim.UID)
	require.NoError(t, err)
	require.Len(t, state.Allocations, 3)
	for _, allocation := range state.Allocations {
		_, err := os.Stat(d.paths.CDISpecPath(claim.UID, allocation.Key))
		require.NoError(t, err)
	}
	metadata, err := os.ReadFile(d.paths.KubeVirtMetadataProjectionPath("vm-network", "net"))
	require.NoError(t, err)
	require.Contains(t, string(metadata), "vhu-0")
	require.Contains(t, string(metadata), "vhu-1")
	require.NoError(t, d.stopClaimWorkers(context.Background(), claim.UID))

	restarted := New("")
	restarted.grout = ports
	restarted.paths = d.paths
	require.NoError(t, restarted.unprepare(context.Background(), claim.UID))
	ports.mu.Lock()
	require.Len(t, ports.deleted, 3)
	ports.mu.Unlock()
	_, err = os.Stat(d.paths.ClaimStatePath(claim.UID))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(d.paths.KubeVirtMetadataProjectionPath("vm-network", "net"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUnprepareRetriesPortDeletion(t *testing.T) {
	ports := &fakePorts{deleteError: errors.New("grout unavailable")}
	d := testDriver(t, ports)
	claim := testClaim(testAllocation("net", "vhu-0"))
	require.NoError(t, d.prepare(context.Background(), claim).Err)
	require.ErrorContains(t, d.unprepare(context.Background(), claim.UID), "grout unavailable")
	_, err := os.Stat(d.paths.ClaimStatePath(claim.UID))
	require.NoError(t, err)
	ports.deleteError = nil
	require.NoError(t, d.unprepare(context.Background(), claim.UID))
	_, err = os.Stat(d.paths.ClaimStatePath(claim.UID))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestPrepareRollsBackStagedFiles(t *testing.T) {
	d := testDriver(t, &fakePorts{})
	d.prepareDir = func(path string) error {
		if filepath.Base(path) == "second" {
			return errors.New("cannot create second directory")
		}
		return os.MkdirAll(path, 0o700)
	}
	claim := testClaim(testAllocation("first", "vhu-0"), testAllocation("second", "vhu-1"))
	require.ErrorContains(t, d.prepare(context.Background(), claim).Err, "cannot create second directory")
	_, err := os.Stat(d.paths.ClaimStatePath(claim.UID))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(d.paths.CDISpecPath(claim.UID, "first"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestPortWorkerRetriesTransientCreateError(t *testing.T) {
	ports := &fakePorts{createError: errors.New("temporary")}
	d := testDriver(t, ports)
	claim := testClaim(testAllocation("net", "vhu-0"))
	require.NoError(t, d.prepare(context.Background(), claim).Err)
	socket := d.paths.SocketPath(claim.UID, "net")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	require.Eventually(t, func() bool {
		ports.mu.Lock()
		defer ports.mu.Unlock()
		return ports.createCount >= 2
	}, 4*time.Second, 20*time.Millisecond)
	require.NoError(t, d.unprepare(context.Background(), types.UID("claim-123")))
}

func TestRecoverPreparedClaimsRestartsPortWorker(t *testing.T) {
	ports := &fakePorts{}
	d := testDriver(t, ports)
	claim := testClaim(testAllocation("net", "vhu-0"))
	require.NoError(t, d.prepare(context.Background(), claim).Err)
	require.NoError(t, d.stopClaimWorkers(context.Background(), claim.UID))

	restarted := New("")
	restarted.grout = ports
	restarted.paths = d.paths
	require.NoError(t, restarted.RecoverPreparedClaims(context.Background()))
	listener, err := net.Listen("unix", restarted.paths.SocketPath(claim.UID, "net"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	require.Eventually(t, func() bool {
		ports.mu.Lock()
		defer ports.mu.Unlock()
		return ports.createCount > 0
	}, 4*time.Second, 20*time.Millisecond)
	require.NoError(t, restarted.unprepare(context.Background(), claim.UID))
}
