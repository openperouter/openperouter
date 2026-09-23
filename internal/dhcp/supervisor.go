// SPDX-License-Identifier:Apache-2.0

package dhcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// DefaultSocketPath is the unix socket the dhcp daemon listens on and the
	// dhcp plugin connects to. It matches the plugin's compiled-in default, so
	// EnsureIPAM/ADD reach this daemon without extra config.
	DefaultSocketPath = "/run/cni/dhcp.sock"

	// DefaultBinPath is the well-known location of the dhcp daemon binary
	// inside the controller container image.
	DefaultBinPath = "/opt/openperouter/cni/bin/dhcp"

	// IPAMTypeDHCP is the IPAM type string that triggers DHCP lease management.
	IPAMTypeDHCP = "dhcp"

	socketReadyTimeout = 10 * time.Second
	socketPollInterval = 100 * time.Millisecond
	restartBackoff     = 500 * time.Millisecond
	shutdownGrace      = 5 * time.Second
)

// Supervisor runs and supervises the CNI dhcp IPAM daemon. It registers
// with the controller-runtime manager immediately but blocks in Start until
// EnsureUp is called — typically when the CNI invoker first processes an
// ADD/DEL/CHECK for a config using DHCP IPAM. Once started, the daemon runs
// for the lifetime of the manager; crashes are restarted automatically.
// The supervisor opts out of leader election so the daemon runs on every node.
type Supervisor struct {
	// Logger receives the daemon's stdout/stderr and supervisor events.
	Logger *slog.Logger
	// SocketPath is the daemon's listening socket; defaults to DefaultSocketPath.
	SocketPath string
	// Broadcast sets the daemon's -broadcast flag (request broadcast replies),
	// which clients without an address typically need.
	Broadcast bool
	// OnRestart is called when the daemon exits unexpectedly (not due to
	// context cancellation). Callers typically use it to trigger
	// reconciliation; the reconciler's EnsureUp call handles waiting for
	// the restarted daemon's socket.
	OnRestart func()

	binPath string
	startCh chan struct{}
	once    sync.Once
}

// NewLazySupervisor creates a Supervisor. Register it with the manager via
// mgr.Add(); it will block in Start until EnsureUp is called.
func NewLazySupervisor(logger *slog.Logger) *Supervisor {
	return &Supervisor{
		Logger:  logger,
		binPath: DefaultBinPath,
		startCh: make(chan struct{}),
	}
}

// NeedLeaderElection returns false so the daemon runs on every node regardless
// of leader election.
func (l *Supervisor) NeedLeaderElection() bool { return false }

// Start blocks until EnsureUp is called, then runs the daemon and restarts it
// on crash. Start only returns when ctx is cancelled (manager shutdown).
func (l *Supervisor) Start(ctx context.Context) error {
	logger := l.logger()
	socketPath := l.socketPath()

	select {
	case <-l.startCh:
	case <-ctx.Done():
		return nil
	}
	logger.Info("DHCP supervisor started", "bin", l.binPath, "socket", socketPath)

	return l.runDaemonWithRestart(ctx, logger, socketPath)
}

func (l *Supervisor) runDaemonWithRestart(ctx context.Context, logger *slog.Logger, socketPath string) error {
	wait.UntilWithContext(
		ctx,
		func(ctx context.Context) {
			err := l.runDaemon(ctx, logger, socketPath)
			if ctx.Err() != nil {
				return
			}

			logger.Error("dhcp daemon exited, restarting", "error", err, "backoff", restartBackoff)

			if l.OnRestart != nil {
				l.OnRestart()
			}
		},
		restartBackoff,
	)
	logger.Info("DHCP daemon supervisor stopped", "error", ctx.Err())
	return nil
}

// EnsureUp enables the supervisor and blocks until the DHCP daemon's socket is
// accepting connections (or ctx is cancelled). It is safe for concurrent use;
// when the daemon is already running the socket Dial succeeds immediately.
func (l *Supervisor) EnsureUp(ctx context.Context) error {
	// Signal Start() to proceed by closing startCh. sync.Once guarantees
	// this happens exactly once even when multiple goroutines call EnsureUp
	// concurrently — a second close would panic.
	l.once.Do(func() { close(l.startCh) })
	return l.waitForSocket(ctx)
}

func (l *Supervisor) waitForSocket(ctx context.Context) error {
	socketPath := l.socketPath()
	err := wait.PollUntilContextTimeout(ctx, socketPollInterval, socketReadyTimeout, true,
		func(ctx context.Context) (bool, error) {
			dialer := net.Dialer{Timeout: socketPollInterval / 2}
			conn, err := dialer.DialContext(ctx, "unix", socketPath)
			if err != nil {
				return false, nil
			}
			if err := conn.Close(); err != nil {
				l.logger().Warn("failed to close socket", "error", err)
			}
			return true, nil
		})
	if err != nil {
		return fmt.Errorf("dhcp daemon socket %s not ready after %s: %w", socketPath, socketReadyTimeout, err)
	}
	return nil
}

func (l *Supervisor) runDaemon(ctx context.Context, logger *slog.Logger, socketPath string) error {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn("failed to remove stale dhcp socket", "socket", socketPath, "error", err)
	}

	args := []string{"daemon", "-socketpath", socketPath}
	if l.Broadcast {
		args = append(args, "-broadcast=true")
	}
	cmd := exec.CommandContext(ctx, l.binPath, args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = shutdownGrace
	cmd.Stdout = logWriter{logger: logger, level: slog.LevelInfo}
	cmd.Stderr = logWriter{logger: logger, level: slog.LevelWarn}

	return cmd.Run()
}

func (l *Supervisor) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}

func (l *Supervisor) socketPath() string {
	if l.SocketPath != "" {
		return l.SocketPath
	}
	return DefaultSocketPath
}

// logWriter forwards a subprocess's output to slog, one line per log record.
type logWriter struct {
	logger *slog.Logger
	level  slog.Level
}

func (w logWriter) Write(p []byte) (int, error) {
	for line := range strings.SplitSeq(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			w.logger.Log(context.Background(), w.level, "DHCP daemon", "log", line)
		}
	}
	return len(p), nil
}
