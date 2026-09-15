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
}

// VhostDevargs builds DPDK net_vhost server (client=0) device args.
// Instance index is stable per port name so create is idempotent on a node.
func VhostDevargs(name, socketPath string, queues uint) (string, error) {
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
	devargs, err := VhostDevargs(p.Name, p.SocketPath, p.Queues)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p.SocketPath), vhostSocketDirPerm); err != nil {
		return fmt.Errorf("creating vhost socket dir for %s: %w", p.Name, err)
	}

	exists, err := c.portExists(ctx, p.Name)
	if err != nil {
		return fmt.Errorf("checking vhost port %s: %w", p.Name, err)
	}
	if !exists {
		// Stale Unix socket blocks grout bind after a previous crash.
		_ = os.Remove(p.SocketPath)
		slog.InfoContext(ctx, "creating grout vhost port", "name", p.Name, "devargs", devargs)
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

	if err := os.Chmod(p.SocketPath, vhostSocketPerm); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("chmod vhost socket %s: %w", p.SocketPath, err)
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
