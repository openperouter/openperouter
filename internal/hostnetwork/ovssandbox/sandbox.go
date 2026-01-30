// SPDX-License-Identifier:Apache-2.0

// Package ovssandbox provides an OVS sandbox environment for testing using containers.
// This allows running OVS unit tests without requiring a system-wide OVS installation.
// It uses testcontainers-go to manage the container lifecycle.
package ovssandbox

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/vishvananda/netns"
	apimachinerywait "k8s.io/apimachinery/pkg/util/wait"
)

const (
	defaultImage = "quay.io/openperouter/kind-node-openperouter:v1.32.2"
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
	// Image is the container image to use. If empty, uses defaultImage.
	Image string
}

func (c Config) image() string {
	if c.Image != "" {
		return c.Image
	}

	image := os.Getenv("KIND_NODE_IMG")
	if image != "" {
		return image
	}

	return defaultImage
}

// ovsStartupScript is the script that initializes OVS inside the container.
//
//go:embed testdata/ovs-start.sh
var ovsStartupScript string

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

	// Create container request
	// Container runs in its own network namespace (no --network=host)
	// so OVS bridges are isolated from the host
	req := testcontainers.ContainerRequest{
		Image: cfg.image(),
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
	if err := waitForSocket(ctx, sandbox.SocketPath, 30*time.Second); err != nil {
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
	return netnamespace.In(s.NetNS, fn)
}

func waitForSocket(ctx context.Context, path string, timeout time.Duration) error {
	return apimachinerywait.PollUntilContextTimeout(ctx, 100*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		info, err := os.Stat(path)
		if err != nil {
			return false, nil
		}
		// Check it's a socket
		return info.Mode()&os.ModeSocket != 0, nil
	})
}
