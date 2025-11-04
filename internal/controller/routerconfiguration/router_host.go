// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/openperouter/openperouter/internal/systemdctl"
)

type RouterHostProvider struct {
	FRRConfigPath     string
	RouterPidFilePath string
	CurrentNodeIndex  int
	SystemdSocketPath string
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
	res, err := client.IsActive("pod-routerpod.service")
	if err != nil {
		return false, fmt.Errorf("failed to check if router pod service is active")
	}

	return res, nil
}
