// SPDX-License-Identifier:Apache-2.0

// Package grout provides a client for managing grout DPDK dataplane interfaces.
// It communicates with the grout daemon via grcli commands over the UNIX socket.
package grout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// Client communicates with the grout daemon via grcli.
type Client struct {
	socketPath string
}

type groutAddress struct {
	Iface   string `json:"iface"`
	Family  string `json:"family"`
	Address string `json:"address"`
}

type groutInterface struct {
	Name string `json:"name"`
}

// NewClient creates a new grout client pointing at the given UNIX socket.
func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

func (c *Client) deleteAddress(ctx context.Context, iface, addr string) error {
	slog.InfoContext(ctx, "deleting grout address", "addr", addr, "iface", iface)
	if err := c.run(ctx, "address", "del", addr, "iface", iface); err != nil {
		return fmt.Errorf("deleting grout address %s from iface %s: %w", addr, iface, err)
	}
	return nil
}

func (c *Client) ensurePort(ctx context.Context, name, devargs string) error {
	exists, err := c.portExists(ctx, name)
	if err != nil {
		return fmt.Errorf("checking if port %s exists: %w", name, err)
	}
	if exists {
		slog.InfoContext(ctx, "grout port already exists", "name", name)
		return nil
	}

	slog.InfoContext(ctx, "creating grout port", "name", name, "devargs", devargs)
	if err := c.run(ctx, "interface", "add", "port", name, "devargs", devargs); err != nil {
		return fmt.Errorf("creating grout port %s: %w", name, err)
	}
	return nil
}

func (c *Client) ensurePortInVRF(ctx context.Context, name, devargs, vrf string) error {
	exists, err := c.portExists(ctx, name)
	if err != nil {
		return fmt.Errorf("checking if port %s exists: %w", name, err)
	}
	if exists {
		slog.InfoContext(ctx, "grout port already exists", "name", name)
		return nil
	}

	slog.InfoContext(ctx, "creating grout port in VRF", "name", name, "devargs", devargs, "vrf", vrf)
	if err := c.run(ctx, "interface", "add", "port", name, "devargs", devargs, "vrf", vrf, "up"); err != nil {
		return fmt.Errorf("creating grout port %s in VRF %s: %w", name, vrf, err)
	}
	return nil
}

func (c *Client) deletePort(ctx context.Context, name string) error {
	exists, err := c.portExists(ctx, name)
	if err != nil {
		return fmt.Errorf("checking if port %s exists: %w", name, err)
	}
	if !exists {
		return nil
	}

	slog.InfoContext(ctx, "deleting grout port", "name", name)
	if err := c.run(ctx, "interface", "del", name); err != nil {
		return fmt.Errorf("deleting grout port %s: %w", name, err)
	}
	return nil
}

// ensureAddress assigns an IP address (in CIDR notation) to a grout port.
// If the address is already assigned, it is a no-op.
func (c *Client) ensureAddress(ctx context.Context, ifaceName, cidr string) error {
	slog.InfoContext(ctx, "assigning IP to grout port", "iface", ifaceName, "cidr", cidr)
	err := c.run(ctx, "address", "add", cidr, "iface", ifaceName)
	if err != nil && strings.Contains(err.Error(), "already") {
		slog.DebugContext(ctx, "address already assigned", "iface", ifaceName, "cidr", cidr)
		return nil
	}
	return err
}

func (c *Client) getAddresses(ctx context.Context, ifaceName string) ([]string, error) {
	out, err := c.runOutput(ctx, "address", "show", "iface", ifaceName)
	if err != nil {
		return nil, err
	}

	var entries []groutAddress
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("parsing address JSON for %s: %w", ifaceName, err)
	}

	addrs := make([]string, 0, len(entries))
	for _, e := range entries {
		addrs = append(addrs, e.Address)
	}
	return addrs, nil
}

func (c *Client) listInterfaces(ctx context.Context) ([]groutInterface, error) {
	out, err := c.runOutput(ctx, "interface", "show")
	if err != nil {
		return nil, fmt.Errorf("listing interfaces: %w", err)
	}
	if out == "" || out == "[]" {
		return nil, nil
	}
	var ifaces []groutInterface
	if err := json.Unmarshal([]byte(out), &ifaces); err != nil {
		return nil, fmt.Errorf("parsing interface list JSON: %w", err)
	}
	return ifaces, nil
}

// portExists checks whether a port with the given name exists in grout.
func (c *Client) portExists(ctx context.Context, name string) (bool, error) {
	out, err := c.runOutput(ctx, "interface", "show", "name", name)
	if err != nil {
		// grcli returns an error when the interface doesn't exist
		if strings.Contains(err.Error(), "No such") || strings.Contains(out, "No such") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// run executes a grcli command and returns any error.
func (c *Client) run(ctx context.Context, args ...string) error {
	_, err := c.runOutput(ctx, args...)
	return err
}

var execCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

// runOutput executes a grcli command and returns stdout and any error.
func (c *Client) runOutput(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{"--err-exit", "--json", "--socket", c.socketPath}, args...)

	slog.DebugContext(ctx, "running grcli", "args", strings.Join(cmdArgs, " "))
	out, err := execCmd(ctx, "grcli", cmdArgs...)
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("grcli %s failed: %w, output: %s", strings.Join(args, " "), err, output)
	}
	return output, nil
}
