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
	restartBackoff     = 2 * time.Second
	shutdownGrace      = 5 * time.Second
)

// LazySupervisor runs and supervises the CNI dhcp IPAM daemon. It registers
// with the controller-runtime manager immediately but blocks in Start until
// Enable is called — typically when the reconciler first detects an underlay
// using DHCP IPAM. It opts out of leader election so the daemon runs on every
// node.
type LazySupervisor struct {
	// Logger receives the daemon's stdout/stderr and supervisor events.
	Logger *slog.Logger
	// SocketPath is the daemon's listening socket; defaults to DefaultSocketPath.
	SocketPath string
	// Broadcast sets the daemon's -broadcast flag (request broadcast replies),
	// which clients without an address typically need.
	Broadcast bool

	binPath  string
	enableCh chan struct{}
	once     sync.Once
}

// NewLazySupervisor creates a LazySupervisor. Register it with the manager via
// mgr.Add(); it will block in Start until Enable is called.
func NewLazySupervisor(logger *slog.Logger) *LazySupervisor {
	return &LazySupervisor{
		Logger:   logger,
		binPath:  DefaultBinPath,
		enableCh: make(chan struct{}),
	}
}

// NeedLeaderElection returns false so the daemon runs on every node regardless
// of leader election.
func (l *LazySupervisor) NeedLeaderElection() bool { return false }

// Start blocks until Enable is called, then runs the daemon and restarts it on
// exit until ctx is cancelled.
func (l *LazySupervisor) Start(ctx context.Context) error {
	select {
	case <-l.enableCh:
	case <-ctx.Done():
		return nil
	}

	logger := l.logger()
	socketPath := l.socketPath()
	logger.Info("DHCP supervisor enabled by underlay configuration", "bin", l.binPath, "socket", socketPath)

	for {
		err := l.startDaemon(ctx, logger, socketPath)
		if ctx.Err() != nil {
			logger.Info("dhcp daemon supervisor stopped")
			return nil
		}
		logger.Error("dhcp daemon exited, restarting", "error", err, "backoff", restartBackoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(restartBackoff):
		}
	}
}

// Enable unblocks the supervisor so it starts the DHCP daemon. It is safe to
// call from multiple goroutines; only the first call has any effect.
func (l *LazySupervisor) Enable() {
	l.once.Do(func() {
		close(l.enableCh)
	})
}

// EnsureUp enables the supervisor and blocks until the DHCP daemon's socket is
// accepting connections (or ctx is cancelled). It is safe for concurrent use;
// when the daemon is already running the socket Dial succeeds immediately.
func (l *LazySupervisor) EnsureUp(ctx context.Context) error {
	l.Enable()
	return l.waitForSocket(ctx)
}

func (l *LazySupervisor) waitForSocket(ctx context.Context) error {
	socketPath := l.socketPath()
	deadline := time.Now().Add(socketReadyTimeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dhcp daemon socket %s not ready after %s", socketPath, socketReadyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(socketPollInterval):
		}
	}
}

func (l *LazySupervisor) startDaemon(ctx context.Context, logger *slog.Logger, socketPath string) error {
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
	cmd.Stdout = logWriter{logger: logger}
	cmd.Stderr = logWriter{logger: logger}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dhcp daemon: %w", err)
	}
	go func() {
		if err := l.waitForSocket(ctx); err != nil {
			logger.Warn("dhcp daemon socket not ready before timeout", "socket", socketPath, "error", err)
			return
		}
		logger.Info("dhcp daemon ready", "socket", socketPath)
	}()
	return cmd.Wait()
}

func (l *LazySupervisor) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}

func (l *LazySupervisor) socketPath() string {
	if l.SocketPath != "" {
		return l.SocketPath
	}
	return DefaultSocketPath
}

// logWriter forwards a subprocess's output to slog, one line per log record.
type logWriter struct {
	logger *slog.Logger
}

func (w logWriter) Write(p []byte) (int, error) {
	for line := range strings.SplitSeq(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			w.logger.Info("dhcp daemon", "log", line)
		}
	}
	return len(p), nil
}
