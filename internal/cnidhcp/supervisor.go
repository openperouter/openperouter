// SPDX-License-Identifier:Apache-2.0

// Package cnidhcp supervises the upstream CNI "dhcp" IPAM daemon as a child
// process of the controller. The daemon shares the controller's mount namespace
// and privileges, so it reaches the router netns directly and its socket is the
// in-container default; the dhcp plugin (exec'd by libcni from the controller)
// connects to the same socket. The supervisor restarts the daemon on exit and,
// after each (re)start, signals a channel so a watcher can re-establish leases
// (the daemon keeps leases in memory and forgets them across restarts).
package cnidhcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	// DefaultSocketPath is the unix socket the dhcp daemon listens on and the
	// dhcp plugin connects to. It matches the plugin's compiled-in default, so
	// EnsureIPAM/ADD reach this daemon without extra config.
	DefaultSocketPath = "/run/cni/dhcp.sock"

	socketReadyTimeout = 10 * time.Second
	socketPollInterval = 100 * time.Millisecond
	restartBackoff     = 2 * time.Second
	shutdownGrace      = 5 * time.Second
)

// Supervisor runs and supervises the CNI dhcp IPAM daemon. It implements
// sigs.k8s.io/controller-runtime/pkg/manager.Runnable and opts out of leader
// election so the daemon runs on every node.
type Supervisor struct {
	// BinPath is the path to the dhcp plugin binary, run as "<bin> daemon".
	BinPath string
	// SocketPath is the daemon's listening socket; defaults to DefaultSocketPath.
	SocketPath string
	// Broadcast sets the daemon's -broadcast flag (request broadcast replies),
	// which clients without an address typically need.
	Broadcast bool
	// Logger receives the daemon's stdout/stderr and supervisor events.
	Logger *slog.Logger
	// RestartNotify, when set, receives an event after every (re)start of the
	// daemon once its socket is ready, so a watcher can re-establish leases.
	RestartNotify chan<- event.GenericEvent
}

// NeedLeaderElection returns false so the daemon runs on every node regardless
// of leader election.
func (s *Supervisor) NeedLeaderElection() bool { return false }

// Start runs the daemon and restarts it on exit until ctx is cancelled. It
// satisfies the controller-runtime manager.Runnable interface.
func (s *Supervisor) Start(ctx context.Context) error {
	logger := s.logger()
	socketPath := s.socketPath()
	logger.Info("starting dhcp daemon supervisor", "bin", s.BinPath, "socket", socketPath)
	for {
		err := s.runOnce(ctx, logger, socketPath)
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

func (s *Supervisor) runOnce(ctx context.Context, logger *slog.Logger, socketPath string) error {
	// A stale socket file from an unclean exit makes the daemon's net.Listen
	// fail with "address already in use".
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn("failed to remove stale dhcp socket", "socket", socketPath, "error", err)
	}

	args := []string{"daemon", "-socketpath", socketPath}
	if s.Broadcast {
		args = append(args, "-broadcast=true")
	}
	cmd := exec.CommandContext(ctx, s.BinPath, args...)
	// Graceful stop: SIGTERM on ctx cancel so the daemon removes its socket;
	// force-kill after the grace period if it does not exit.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = shutdownGrace
	cmd.Stdout = logWriter{logger: logger}
	cmd.Stderr = logWriter{logger: logger}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dhcp daemon: %w", err)
	}
	go s.notifyWhenReady(ctx, logger, socketPath)
	return cmd.Wait()
}

// notifyWhenReady polls the daemon socket and emits a restart notification once
// it accepts connections.
func (s *Supervisor) notifyWhenReady(ctx context.Context, logger *slog.Logger, socketPath string) {
	deadline := time.Now().Add(socketReadyTimeout)
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			logger.Info("dhcp daemon ready", "socket", socketPath)
			s.notifyRestart(logger)
			return
		}
		if time.Now().After(deadline) {
			logger.Warn("dhcp daemon socket not ready before timeout", "socket", socketPath)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(socketPollInterval):
		}
	}
}

func (s *Supervisor) notifyRestart(logger *slog.Logger) {
	if s.RestartNotify == nil {
		return
	}
	// Non-blocking: a single pending notification already triggers a reconcile
	// that re-establishes every lease, so a dropped duplicate is harmless.
	select {
	case s.RestartNotify <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{}}:
	default:
		logger.Debug("dhcp restart notification dropped (channel full)")
	}
}

func (s *Supervisor) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Supervisor) socketPath() string {
	if s.SocketPath != "" {
		return s.SocketPath
	}
	return DefaultSocketPath
}

// logWriter forwards a subprocess's output to slog, one line per log record.
type logWriter struct {
	logger *slog.Logger
}

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			w.logger.Info("dhcp daemon", "log", line)
		}
	}
	return len(p), nil
}
