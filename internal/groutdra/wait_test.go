// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsUnixSocket(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "f")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if isUnixSocket(regular) {
		t.Fatal("regular file")
	}
	if isUnixSocket(filepath.Join(dir, "missing")) {
		t.Fatal("missing")
	}

	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	if !isUnixSocket(sock) {
		t.Fatal("expected unix socket")
	}
}

func TestWaitForUnixSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "vhost.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errc := make(chan error, 1)
	go func() { errc <- waitForUnixSocket(ctx, sock) }()

	time.Sleep(50 * time.Millisecond)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestWaitForUnixSocketCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForUnixSocket(ctx, "/no/such/vhost.sock"); err == nil {
		t.Fatal("expected ctx error")
	}
}
