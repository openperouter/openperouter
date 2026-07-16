// SPDX-License-Identifier:Apache-2.0

package systemdctl

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	HostDBusSocket = "unix:path=/host/dbus/system_bus_socket"

	DefaultTimeout = 30 * time.Second

	StateActive     = "active"
	StateActivating = "activating"
	StateInactive   = "inactive"
	StateFailed     = "failed"
)

// Client is a systemd client that uses systemctl commands via nsenter
// to avoid D-Bus authentication issues when running in containers
type Client struct {
	timeout time.Duration
	hostPID string // PID 1 on the host (usually "1")
}

// NewClient creates a new systemd client that uses systemctl commands
func NewClient() (*Client, error) {
	return NewClientWithTimeout(DefaultTimeout)
}

// NewClientWithTimeout creates a new systemd client with a custom timeout
func NewClientWithTimeout(timeout time.Duration) (*Client, error) {
	return &Client{
		timeout: timeout,
		hostPID: "1",
	}, nil
}

// Restart restarts a systemd unit
func (c *Client) Restart(ctx context.Context, unitName string) error {
	return c.runSystemctl(ctx, "restart", unitName)
}

// IsActive checks if a systemd unit is active
func (c *Client) IsActive(unitName string) (bool, error) {
	state, err := c.ActiveState(unitName)
	if err != nil {
		return false, err
	}
	return state == StateActive, nil
}

// IsActivating checks if a systemd unit is in the "activating" state
// (i.e. still starting up).
func (c *Client) IsActivating(unitName string) (bool, error) {
	state, err := c.ActiveState(unitName)
	if err != nil {
		return false, err
	}
	return state == StateActivating, nil
}

// ActiveState returns the ActiveState of a systemd unit
// (e.g. "active", "activating", "inactive", "failed").
func (c *Client) ActiveState(unitName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	cmdArgs := []string{
		"-t", c.hostPID,
		"-m", "-u", "-i", "-n",
		"systemctl",
		"is-active",
		unitName,
	}

	cmd := exec.CommandContext(ctx, "nsenter", cmdArgs...)
	output, err := cmd.CombinedOutput()
	state := strings.TrimSpace(string(output))

	// systemctl is-active returns non-zero for anything other than "active"
	if err != nil {
		if state == StateInactive || state == StateFailed || state == StateActivating {
			return state, nil
		}
		return "", fmt.Errorf("systemctl is-active %s failed: %w: %s", unitName, err, string(output))
	}

	return state, nil
}

// runSystemctl executes a systemctl command in the host's namespaces
func (c *Client) runSystemctl(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Use nsenter to run systemctl in the host's mount, UTS, IPC, and net namespaces
	// Since we're already running with --pid=host, we can use PID 1
	//nolint:prealloc
	cmdArgs := []string{
		"-t", c.hostPID,
		"-m", "-u", "-i", "-n",
		"systemctl",
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "nsenter", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s failed: %w: %s", strings.Join(args, " "), err, string(output))
	}
	return nil
}
