// SPDX-License-Identifier:Apache-2.0

package dhcp

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// fakeDaemonPath compiles a tiny Go program that listens on a unix socket at
// the path given by -socketpath and exits when the socket file is removed (used
// by killFakeDaemon). The "daemon" sub-command prefix is consumed to match the
// real dhcp binary invocation ("dhcp daemon -socketpath <path>").
func fakeDaemonPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "fakedaemon.go")

	program := `package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	socketPath := ""
	args := os.Args[1:]
	// Skip "daemon" sub-command.
	if len(args) > 0 && args[0] == "daemon" {
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "-socketpath" && i+1 < len(args) {
			socketPath = args[i+1]
			break
		}
	}
	if socketPath == "" {
		fmt.Fprintln(os.Stderr, "missing -socketpath")
		os.Exit(1)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}
	defer ln.Close()

	// Accept connections in a goroutine so readiness polling succeeds.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	// Stay alive until the socket file is deleted (test kills us this way).
	for {
		if _, err := os.Stat(socketPath); os.IsNotExist(err) {
			os.Exit(0)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dir, "fakedaemon")
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to compile fake daemon: %v\n%s", err, out)
	}
	return bin
}

// killFakeDaemon removes the socket file, causing the fake daemon to exit.
func killFakeDaemon(t *testing.T, socketPath string) {
	t.Helper()
	// Wait for the socket file to exist before removing it.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("socket file never appeared")
		case <-tick.C:
		}
	}

	// Close any listeners by connecting and then removing the file.
	if conn, err := net.Dial("unix", socketPath); err == nil {
		_ = conn.Close()
	}
	if err := os.Remove(socketPath); err != nil {
		t.Fatalf("failed to remove socket to kill fake daemon: %v", err)
	}
}
