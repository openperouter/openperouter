// SPDX-License-Identifier:Apache-2.0

// Package groutdra is a Kind POC kubelet DRA plugin. Layout and CDI env match
// kubevirt#18444 (hostpath DRA test driver) so kubevirt#19044 / 
// quay.io/anbanerj/test-vhostuser-nbp:client can consume the mount later.
package groutdra

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/dynamic-resource-allocation/resourceslice"

	"github.com/openperouter/openperouter/internal/grout"
)

type Driver struct {
	grout *grout.Client
}

func New(groutSock string) *Driver {
	return &Driver{grout: grout.NewClient(groutSock)}
}

func PoolResources() resourceslice.DriverResources {
	devices := make([]resourceapi.Device, 0, MaxDevices)
	for i := 0; i < MaxDevices; i++ {
		qemuMode := "client"
		mac := GuestMAC
		queues := int64(1)
		devices = append(devices, resourceapi.Device{
			Name: fmt.Sprintf("vhu-%d", i),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"grout.openperouter.io/qemuMode": {StringValue: &qemuMode},
				"grout.openperouter.io/mac":      {StringValue: &mac},
				"grout.openperouter.io/queues":   {IntValue: &queues},
			},
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
	uid := claim.UID
	dir := ClaimDir(uid)
	sock := SocketPath(uid)
	name := PortName(uid)

	if err := os.MkdirAll(dir, 0o777); err != nil {
		return kubeletplugin.PrepareResult{Err: err}
	}
	// grout (not qemu) binds the Unix socket. 0755 + uid 107 blocks grout.
	_ = os.Chmod(dir, 0o777)

	if err := d.grout.CreateVhostPort(ctx, grout.VhostPortParams{
		Name:        name,
		SocketPath:  sock,
		Queues:      1,
		MAC:         GuestMAC,
		GatewayCIDR: grout.VhostGuestGatewayCIDR,
	}); err != nil {
		return kubeletplugin.PrepareResult{Err: fmt.Errorf("CreateVhostPort %s: %w", name, err)}
	}
	_ = os.Chown(sock, QEMUUID, QEMUGID)
	_ = os.Chmod(sock, 0o777)

	cdiID, err := writeCDISpec(uid, dir)
	if err != nil {
		return kubeletplugin.PrepareResult{Err: err}
	}

	var devices []kubeletplugin.Device
	if claim.Status.Allocation != nil {
		for _, result := range claim.Status.Allocation.Devices.Results {
			if result.Driver != DriverName {
				continue
			}
			devices = append(devices, kubeletplugin.Device{
				Requests:     []string{result.Request},
				PoolName:     result.Pool,
				DeviceName:   result.Device,
				CDIDeviceIDs: []string{cdiID},
			})
		}
	}
	if len(devices) == 0 {
		devices = []kubeletplugin.Device{{
			PoolName:     PoolName,
			DeviceName:   "vhu-0",
			CDIDeviceIDs: []string{cdiID},
		}}
	}
	return kubeletplugin.PrepareResult{Devices: devices}
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
	name := PortName(uid)
	sock := SocketPath(uid)
	if err := d.grout.DeleteVhostPort(ctx, name, sock); err != nil {
		slog.WarnContext(ctx, "DeleteVhostPort", "err", err)
	}
	removeCDISpec(uid)
	_ = os.RemoveAll(ClaimDir(uid))
	return nil
}

func (d *Driver) HandleError(ctx context.Context, err error, msg string) {
	slog.ErrorContext(ctx, msg, "err", err)
}
