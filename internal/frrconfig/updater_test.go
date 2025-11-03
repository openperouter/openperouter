// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
)

func TestUpdaterForSocket(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer func() {
		if err := os.Remove(tmpfile.Name()); err != nil {
			t.Fatalf("failed to remove temp file: %v", err)
		}
	}()

	// Create a temporary socket
	socketPath := "/tmp/test_socket"
	defer func() {
		_ = os.Remove(socketPath)
	}()

	// Create a unix socket server
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create unix socket: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
	}()

	updater := UpdaterForSocket(socketPath, tmpfile.Name())

	err = updater(context.Background(), "test config")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	content, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}
	expectedContent := "test config"
	if string(content) != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, string(content))
	}
}
