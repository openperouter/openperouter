// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/systemdctl"
	"github.com/vishvananda/netns"
)

type RouterHostProvider struct {
	FRRConfigPath         string
	RouterPidFilePath     string
	CurrentNodeIndex      int
	SystemdSocketPath     string
	RouterHealthCheckPort int
}

var _ RouterProvider = (*RouterHostProvider)(nil)

type RouterHostContainer struct {
	manager *RouterHostProvider
}

var _ Router = (*RouterHostContainer)(nil)

func (r *RouterHostProvider) New(ctx context.Context) (Router, error) {
	return &RouterHostContainer{
		manager: r,
	}, nil
}

func (r *RouterHostProvider) NodeIndex(ctx context.Context) (int, error) {
	return r.CurrentNodeIndex, nil
}

func (r *RouterHostContainer) TargetNS(ctx context.Context) (string, error) {
	pidData, err := os.ReadFile(r.manager.RouterPidFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read PID file %s: %w", r.manager.RouterPidFilePath, err)
	}

	pidStr := strings.TrimSpace(string(pidData))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse PID from file %s: %w", r.manager.RouterPidFilePath, err)
	}

	res := fmt.Sprintf("/hostproc/%d/ns/net", pid)
	return res, nil
}

func (r *RouterHostContainer) HandleNonRecoverableError(ctx context.Context) error {
	client, err := systemdctl.NewClient()
	if err != nil {
		return fmt.Errorf("failed to create systemd client %w", err)
	}
	slog.Info("restarting router systemd unit", "unit", "pod-routerpod.service")
	if err := client.Restart(ctx, "pod-routerpod.service"); err != nil {
		return fmt.Errorf("failed to restart routerpod service")
	}
	slog.Info("router systemd unit restarted", "unit", "pod-routerpod.service")

	return nil
}

func (r *RouterHostContainer) CanReconcile(ctx context.Context) (bool, error) {
	client, err := systemdctl.NewClient()
	if err != nil {
		return false, fmt.Errorf("failed to create systemd client %w", err)
	}
	isActive, err := client.IsActive("pod-routerpod.service")
	if err != nil {
		return false, fmt.Errorf("failed to check if router pod service is active")
	}
	if !isActive {
		slog.Info("router pod service is not active")
		return false, nil
	}

	targetNS, err := r.TargetNS(ctx)
	if err != nil {
		slog.Info("failed to get router target namespace", "error", err)
		return false, nil
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		slog.Info("failed to open router network namespace", "namespace", targetNS, "error", err)
		return false, nil
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	var healthCheckErr error
	err = netnamespace.In(ns, func() error {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", r.manager.RouterHealthCheckPort), 2*time.Second)
		if err != nil {
			healthCheckErr = fmt.Errorf("failed to connect to reloader: %w", err)
			return nil
		}
		defer func() {
			if err := conn.Close(); err != nil {
				slog.Error("failed to close connection", "error", err)
			}
		}()

		_, err = fmt.Fprintf(conn, "GET /healthz HTTP/1.1\r\nHost: 127.0.0.1:%d\r\n\r\n", r.manager.RouterHealthCheckPort)
		if err != nil {
			healthCheckErr = fmt.Errorf("failed to send health check request: %w", err)
			return nil
		}

		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			healthCheckErr = fmt.Errorf("failed to set read deadline: %w", err)
			return nil
		}

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			healthCheckErr = fmt.Errorf("failed to read health check response: %w", err)
			return nil
		}

		response := string(buf[:n])
		if !strings.Contains(response, "200 OK") {
			healthCheckErr = fmt.Errorf("health check did not return 200 OK: %s", response)
		}
		return nil
	})
	if err != nil {
		slog.Info("failed to execute health check in router namespace", "error", err)
		return false, nil
	}
	if healthCheckErr != nil {
		slog.Info("router health check failed", "error", healthCheckErr)
		return false, nil
	}

	return true, nil
}
