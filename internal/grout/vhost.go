// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// GRPortDevargsSize is grout's GR_PORT_DEVARGS_SIZE. Longer strings are rejected
// by the daemon; keep generated net_vhost args under this limit.
const GRPortDevargsSize = 256

// VhostGuestGatewayCIDR is the grout-side address for the L3 POC subnet.
const VhostGuestGatewayCIDR = "192.169.20.1/24"

const (
	vhostQSize         = 1024
	vhostInstanceMin   = 100
	vhostInstanceSpan  = 900 // 100..999, away from typical net_tap0
	vhostSocketPerm    = 0o777
	vhostSocketDirPerm = 0o777
)

// VhostPortParams is the only input CreateVhostPort needs. DRA (and humans)
// should call that helper rather than assembling grcli themselves.
type VhostPortParams struct {
	Name        string
	SocketPath  string
	Queues      uint
	MAC         string // optional; applied with interface set port … mac
	GatewayCIDR string // optional; empty skips address add
	// Client is DPDK net_vhost client=1: grout connects, QEMU binds (libvirt mode=server).
	Client bool
}

// VhostDevargs builds DPDK net_vhost device args.
// Instance index is stable per port name so create is idempotent on a node.
// client=true adds client=1 (grout reconnects to a QEMU-owned Unix socket).
func VhostDevargs(name, socketPath string, queues uint, client bool) (string, error) {
	if name == "" {
		return "", fmt.Errorf("vhost port name is required")
	}
	if socketPath == "" {
		return "", fmt.Errorf("vhost socket path is required")
	}
	if queues == 0 {
		queues = 1
	}
	devargs := fmt.Sprintf("net_vhost%d,iface=%s,queues=%d", vhostInstanceIndex(name), socketPath, queues)
	if client {
		devargs += ",client=1"
	}
	if len(devargs) > GRPortDevargsSize {
		return "", fmt.Errorf("vhost devargs length %d exceeds GR_PORT_DEVARGS_SIZE %d: %s", len(devargs), GRPortDevargsSize, devargs)
	}
	return devargs, nil
}

func vhostInstanceIndex(name string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return vhostInstanceMin + (h.Sum32() % vhostInstanceSpan)
}

// CreateVhostPort creates (or no-ops) a grout net_vhost port listening on socketPath.
func (c *Client) CreateVhostPort(ctx context.Context, p VhostPortParams) error {
	if p.Queues == 0 {
		p.Queues = 1
	}
	devargs, err := VhostDevargs(p.Name, p.SocketPath, p.Queues, p.Client)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p.SocketPath), vhostSocketDirPerm); err != nil {
		return fmt.Errorf("creating vhost socket dir for %s: %w", p.Name, err)
	}

	info, err := c.getInterfaceInfo(ctx, p.Name)
	if err != nil {
		return fmt.Errorf("checking vhost port %s: %w", p.Name, err)
	}
	// Client=1 connect is one-shot on grout 0.17. A port that exists but is not
	// running never completes the QEMU handshake unless we delete and add again.
	if p.Client && info != nil && !info.hasFlag("running") {
		slog.InfoContext(ctx, "grout vhost client port exists without running; recreating", "name", p.Name, "flags", info.Flags)
		if err := c.deletePort(ctx, p.Name); err != nil {
			return fmt.Errorf("deleting stuck vhost port %s: %w", p.Name, err)
		}
		info = nil
	}
	if info == nil {
		// Server-mode grout binds the socket; a leftover file blocks bind.
		// Client-mode grout connects to QEMU's socket — do not unlink it.
		if !p.Client {
			_ = os.Remove(p.SocketPath)
		}
		slog.InfoContext(ctx, "creating grout vhost port", "name", p.Name, "devargs", devargs, "client", p.Client)
		if err := c.run(ctx, "interface", "add", "port", p.Name, "devargs", devargs,
			"rxqs", fmt.Sprintf("%d", p.Queues), "qsize", fmt.Sprintf("%d", vhostQSize), "up"); err != nil {
			return fmt.Errorf("creating grout vhost port %s: %w", p.Name, err)
		}
	} else {
		slog.InfoContext(ctx, "grout vhost port already exists", "name", p.Name)
	}

	if p.MAC != "" {
		if err := c.run(ctx, "interface", "set", "port", p.Name, "mac", p.MAC); err != nil {
			// net_vhost reports EOPNOTSUPP; grout still assigns a PMD MAC. Guest MAC is QEMU's.
			if strings.Contains(err.Error(), "EOPNOTSUPP") || strings.Contains(err.Error(), "not supported") {
				slog.InfoContext(ctx, "grout cannot set MAC on vhost port (expected for net_vhost)", "name", p.Name, "mac", p.MAC)
			} else {
				return fmt.Errorf("setting mac on vhost port %s: %w", p.Name, err)
			}
		}
	}
	if p.GatewayCIDR != "" {
		if err := c.ensureAddress(ctx, p.Name, p.GatewayCIDR); err != nil {
			return fmt.Errorf("assigning %s on vhost port %s: %w", p.GatewayCIDR, p.Name, err)
		}
	}

	if !p.Client {
		if err := os.Chmod(p.SocketPath, vhostSocketPerm); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("chmod vhost socket %s: %w", p.SocketPath, err)
		}
	}
	return nil
}

// DeleteVhostPort removes the grout port and unlinks a leftover socket file.
func (c *Client) DeleteVhostPort(ctx context.Context, name, socketPath string) error {
	if err := c.deletePort(ctx, name); err != nil {
		return err
	}
	if socketPath != "" {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("unlinking vhost socket %s: %w", socketPath, err)
		}
	}
	return nil
}
