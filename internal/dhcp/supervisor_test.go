// SPDX-License-Identifier:Apache-2.0

package dhcp

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newStartedSupervisor(binPath, socketPath string, logger *slog.Logger) *Supervisor {
	l := &Supervisor{
		binPath:    binPath,
		SocketPath: socketPath,
		Logger:     logger,
		startCh:    make(chan struct{}),
	}
	l.once.Do(func() { close(l.startCh) })
	return l
}

func waitForSocket(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		conn, err := net.Dial("unix", socketPath)
		if err == nil {
			_ = conn.Close()
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("socket %s not ready before timeout", socketPath)
		case <-tick.C:
		}
	}
}

func TestSupervisorStartStop(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "dhcp.sock")

	sup := newStartedSupervisor(
		fakeDaemonPath(t), socketPath,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Start(ctx) }()

	waitForSocket(t, socketPath, 10*time.Second)

	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket file not created: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for supervisor to stop")
	}
}

func TestSupervisorRestartsOnExit(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "dhcp.sock")

	sup := newStartedSupervisor(
		fakeDaemonPath(t), socketPath,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Start(ctx) }()

	waitForSocket(t, socketPath, 10*time.Second)

	// Kill the fake daemon to trigger a restart.
	killFakeDaemon(t, socketPath)

	waitForSocket(t, socketPath, 15*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for supervisor to stop")
	}
}

func TestSupervisorOnRestartCallback(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "dhcp.sock")

	restarted := make(chan struct{}, 1)
	sup := newStartedSupervisor(
		fakeDaemonPath(t), socketPath,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)
	sup.OnRestart = func() {
		select {
		case restarted <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Start(ctx) }()

	waitForSocket(t, socketPath, 10*time.Second)

	// Kill the fake daemon to trigger a restart.
	killFakeDaemon(t, socketPath)

	select {
	case <-restarted:
	case <-time.After(15 * time.Second):
		t.Fatal("OnRestart callback was not called after daemon restart")
	}

	cancel()
	<-done
}

func TestSupervisorOnRestartNotCalledOnFirstStart(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "dhcp.sock")

	restarted := make(chan struct{}, 1)
	sup := newStartedSupervisor(
		fakeDaemonPath(t), socketPath,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)
	sup.OnRestart = func() {
		select {
		case restarted <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Start(ctx) }()

	waitForSocket(t, socketPath, 10*time.Second)

	// Give enough time for a spurious callback to fire.
	select {
	case <-restarted:
		t.Fatal("OnRestart callback was called on initial start")
	case <-time.After(500 * time.Millisecond):
	}

	cancel()
	<-done
}

func TestSupervisorStaleSocketRemoved(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "dhcp.sock")

	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	sup := newStartedSupervisor(
		fakeDaemonPath(t), socketPath,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Start(ctx) }()

	waitForSocket(t, socketPath, 10*time.Second)

	cancel()
	<-done
}

func TestSupervisorDefaultSocketPath(t *testing.T) {
	sup := &Supervisor{startCh: make(chan struct{})}
	if sup.socketPath() != DefaultSocketPath {
		t.Errorf("default socket path = %q, want %q", sup.socketPath(), DefaultSocketPath)
	}
}

func TestSupervisorCustomSocketPath(t *testing.T) {
	sup := &Supervisor{SocketPath: "/custom/path.sock", startCh: make(chan struct{})}
	if sup.socketPath() != "/custom/path.sock" {
		t.Errorf("custom socket path = %q, want /custom/path.sock", sup.socketPath())
	}
}

func TestSupervisorNeedLeaderElection(t *testing.T) {
	sup := &Supervisor{startCh: make(chan struct{})}
	if sup.NeedLeaderElection() {
		t.Error("NeedLeaderElection() should return false")
	}
}

func TestEnsureUpBlocksUntilReady(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "dhcp.sock")

	sup := &Supervisor{
		binPath:    fakeDaemonPath(t),
		SocketPath: socketPath,
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		startCh:    make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Start(ctx) }()

	if err := sup.EnsureUp(ctx); err != nil {
		t.Fatalf("EnsureUp failed: %v", err)
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("socket not accepting connections after EnsureUp: %v", err)
	}
	_ = conn.Close()

	cancel()
	<-done
}

func TestEnsureUpReturnsImmediatelyWhenAlreadyRunning(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "dhcp.sock")

	sup := newStartedSupervisor(
		fakeDaemonPath(t), socketPath,
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sup.Start(ctx) }()

	waitForSocket(t, socketPath, 10*time.Second)

	start := time.Now()
	if err := sup.EnsureUp(ctx); err != nil {
		t.Fatalf("EnsureUp failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("EnsureUp took %v on an already-running daemon, expected immediate return", elapsed)
	}

	cancel()
	<-done
}

func TestEnsureUpRespectsContextCancellation(t *testing.T) {
	sup := &Supervisor{
		binPath:    "/nonexistent/dhcp",
		SocketPath: filepath.Join(t.TempDir(), "never.sock"),
		Logger:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		startCh:    make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = sup.Start(ctx) }()

	cancel()
	err := sup.EnsureUp(ctx)
	if err == nil {
		t.Fatal("EnsureUp should have returned an error when context is cancelled")
	}
}

func TestLogWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	w := logWriter{logger: logger}

	n, err := w.Write([]byte("line one\nline two\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 18 {
		t.Errorf("Write returned %d, want 18", n)
	}
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("line one")) {
		t.Errorf("output missing 'line one': %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("line two")) {
		t.Errorf("output missing 'line two': %s", out)
	}
}
