// SPDX-License-Identifier:Apache-2.0

// Package ovssandbox provides an OVS sandbox environment for testing using containers.
// This allows running OVS unit tests without requiring a system-wide OVS installation.
// It uses testcontainers-go to manage the container lifecycle.
package ovssandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/vishvananda/netns"
)

const (
	// DefaultImage is the container image with OVS pre-installed.
	// This is the same image used for kind nodes in the project.
	DefaultImage = "quay.io/openperouter/kind-node-openperouter:v1.32.2"
)

// Sandbox represents a running OVS sandbox environment in a container.
type Sandbox struct {
	// Dir is the sandbox directory on the host containing OVS runtime files
	Dir string
	// SocketPath is the full path to the OVSDB unix socket (without unix: prefix)
	SocketPath string
	// OVSDBSocketURI is the URI to use for connecting to OVSDB (with unix: prefix)
	OVSDBSocketURI string
	// NetNS is the network namespace handle for the container
	NetNS netns.NsHandle
	// container is the testcontainers container instance
	container testcontainers.Container
}

// Config contains configuration options for creating a sandbox.
type Config struct {
	// Image is the container image to use. If empty, uses DefaultImage.
	Image string
}

// ovsStartupScript is the script that initializes OVS inside the container.
const ovsStartupScript = `#!/bin/bash
set -e

# Bring up loopback interface
ip link set lo up

# Create OVS database
ovsdb-tool create /var/run/openvswitch/conf.db /usr/share/openvswitch/vswitch.ovsschema

# Start ovsdb-server
ovsdb-server \
    --detach \
    --no-chdir \
    --pidfile=/var/run/openvswitch/ovsdb-server.pid \
    --log-file=/var/run/openvswitch/ovsdb-server.log \
    --remote=punix:/var/run/openvswitch/db.sock \
    --remote=db:Open_vSwitch,Open_vSwitch,manager_options \
    /var/run/openvswitch/conf.db

# Wait for socket to be ready
i=0
while ! [ -S /var/run/openvswitch/db.sock ]; do
    i=$((i+1))
    if [ "$i" -ge 50 ]; then
        echo "Error: OVSDB socket /var/run/openvswitch/db.sock not found after 5 seconds." >&2
        exit 1
    fi
    sleep 0.1
done

# Initialize database
ovs-vsctl --no-wait init

# Start ovs-vswitchd
ovs-vswitchd \
    --detach \
    --no-chdir \
    --pidfile=/var/run/openvswitch/ovs-vswitchd.pid \
    --log-file=/var/run/openvswitch/ovs-vswitchd.log

# Signal that OVS is ready
echo "OVS_SANDBOX_READY"

# Keep container running
exec sleep infinity
`

// New creates and starts a new OVS sandbox environment in a container.
func New(ctx context.Context, cfg Config) (*Sandbox, error) {
	// Create sandbox directory on host for the socket
	sandboxDir, err := os.MkdirTemp("", "ovs-sandbox-")
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox directory: %w", err)
	}

	// Ensure directory has correct permissions for container access
	if err := os.Chmod(sandboxDir, 0755); err != nil {
		_ = os.RemoveAll(sandboxDir)
		return nil, fmt.Errorf("failed to set sandbox directory permissions: %w", err)
	}

	sandbox := &Sandbox{
		Dir:        sandboxDir,
		SocketPath: filepath.Join(sandboxDir, "db.sock"),
	}
	sandbox.OVSDBSocketURI = "unix:" + sandbox.SocketPath

	// Determine image
	image := cfg.Image
	if image == "" {
		image = DefaultImage
	}

	// Create container request
	// Container runs in its own network namespace (no --network=host)
	// so OVS bridges are isolated from the host
	req := testcontainers.ContainerRequest{
		Image: image,
		// Override entrypoint to bypass the kind node entrypoint
		Entrypoint: []string{"/bin/bash", "-c"},
		// Run OVS startup script
		Cmd: []string{ovsStartupScript},
		// Wait for OVS to be ready
		WaitingFor: wait.ForLog("OVS_SANDBOX_READY").WithStartupTimeout(60 * time.Second),
		// Add capabilities and mount sandbox directory for the socket
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.CapAdd = []string{"NET_ADMIN"}
			hc.Binds = append(hc.Binds, sandboxDir+":/var/run/openvswitch")
		},
	}

	// Start container
	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		_ = os.RemoveAll(sandboxDir)
		return nil, fmt.Errorf("failed to start OVS container: %w", err)
	}

	sandbox.container = ctr

	// Wait for socket to appear on host
	if err := waitForSocket(sandbox.SocketPath, 30*time.Second); err != nil {
		_ = sandbox.Cleanup(ctx)
		return nil, err
	}

	// Get the container's PID to access its network namespace
	inspect, err := ctr.Inspect(ctx)
	if err != nil {
		_ = sandbox.Cleanup(ctx)
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	containerPID := inspect.State.Pid
	nsHandle, err := netns.GetFromPid(containerPID)
	if err != nil {
		_ = sandbox.Cleanup(ctx)
		return nil, fmt.Errorf("failed to get container network namespace: %w", err)
	}
	sandbox.NetNS = nsHandle

	return sandbox, nil
}

// waitForSocket waits for a unix socket to appear on the filesystem.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil {
			// Check it's a socket
			if info.Mode()&os.ModeSocket != 0 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for OVS socket: %s", path)
}

// Cleanup stops the container and removes the sandbox directory.
func (s *Sandbox) Cleanup(ctx context.Context) error {
	if s == nil {
		return nil
	}

	var errs []error

	// Close network namespace handle
	if s.NetNS != 0 {
		if err := s.NetNS.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close netns: %w", err))
		}
	}

	// Terminate container (OVS bridges are automatically cleaned up
	// since they exist only in the container's network namespace)
	if s.container != nil {
		if err := s.container.Terminate(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to terminate container: %w", err))
		}
	}

	// Remove sandbox directory
	if s.Dir != "" {
		if err := os.RemoveAll(s.Dir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove sandbox dir: %w", err))
		}
	}

	return errors.Join(errs...)
}

// Logs returns the container logs for debugging.
func (s *Sandbox) Logs(ctx context.Context) (string, error) {
	if s.container == nil {
		return "", nil
	}

	reader, err := s.container.Logs(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

// Exec runs a command inside the OVS container.
func (s *Sandbox) Exec(ctx context.Context, cmd []string) (int, string, error) {
	if s.container == nil {
		return -1, "", fmt.Errorf("container not running")
	}

	exitCode, reader, err := s.container.Exec(ctx, cmd)
	if err != nil {
		return exitCode, "", err
	}
	if closer, ok := reader.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		return exitCode, "", err
	}

	return exitCode, string(output), nil
}

// RunOVSVsctl runs an ovs-vsctl command against this sandbox.
func (s *Sandbox) RunOVSVsctl(ctx context.Context, args ...string) (string, error) {
	cmd := append([]string{"ovs-vsctl"}, args...)
	exitCode, output, err := s.Exec(ctx, cmd)
	if err != nil {
		return output, err
	}
	if exitCode != 0 {
		return output, fmt.Errorf("ovs-vsctl exited with code %d: %s", exitCode, output)
	}
	return output, nil
}

// InNetNS executes a function in the container's network namespace.
// This properly locks the OS thread to ensure namespace isolation.
func (s *Sandbox) InNetNS(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save current namespace
	origNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get current namespace: %w", err)
	}
	defer func() { _ = origNS.Close() }()

	// Switch to container's namespace
	if err := netns.Set(s.NetNS); err != nil {
		return fmt.Errorf("failed to switch to container namespace: %w", err)
	}

	// Ensure we switch back
	defer func() { _ = netns.Set(origNS) }()

	// Execute the function
	return fn()
}
