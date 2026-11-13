// SPDX-License-Identifier:Apache-2.0

package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// RebootNode restarts a kind container and restores its lab links.
type RebootNode struct {
	Name          string
	Runtime       string
	RestartScript string
}

func (n RebootNode) Exec(command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return runCommand(ctx, n.Runtime, append([]string{"exec", n.Name, command}, args...)...)
}

// Reboot performs a fake reboot by restarting the kind container; the host kernel is not rebooted.
func (n RebootNode) Reboot() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, err := n.initPID(ctx)
	if err != nil {
		return err
	}
	output, err := runCommand(ctx, "bash", n.RestartScript, n.Runtime, n.Name)
	if err != nil {
		return fmt.Errorf("restarting kind node %s: %w: %s", n.Name, err, output)
	}
	after, err := n.initPID(ctx)
	if err != nil {
		return err
	}
	// Kind shares the host kernel, so a new init PID proves the node restarted.
	if after == before {
		return fmt.Errorf("kind node %s init PID has not changed: %d", n.Name, before)
	}
	if output, err := runCommand(ctx, n.Runtime, "exec", n.Name, "true"); err != nil {
		return fmt.Errorf("kind node %s is not ready after restart: %w: %s", n.Name, err, output)
	}
	return nil
}

func (n RebootNode) initPID(ctx context.Context) (int, error) {
	output, err := runCommand(ctx, n.Runtime, "inspect", "--format", "{{.State.Pid}}", n.Name)
	if err != nil {
		return 0, fmt.Errorf("inspecting kind node %s: %w: %s", n.Name, err, output)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, fmt.Errorf("parsing kind node %s init PID: %w", n.Name, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("kind node %s is not running (init PID %d)", n.Name, pid)
	}
	return pid, nil
}

func runCommand(ctx context.Context, command string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s failed: %w", command, err)
	}
	return string(output), nil
}
