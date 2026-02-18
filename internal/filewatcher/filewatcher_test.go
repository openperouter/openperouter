// SPDX-License-Identifier:Apache-2.0

package filewatcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestFileWatcherEventDetection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filewatcher-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	triggerChan := make(chan event.GenericEvent, 10)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	fw, err := New(tmpDir, triggerChan, logger)
	if err != nil {
		t.Fatalf("failed to create file watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = fw.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start file watcher: %v", err)
	}

	testFile := filepath.Join(tmpDir, "test-config.yaml")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	select {
	case <-triggerChan:
		// Event received - success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for file creation event")
	}

	err = os.WriteFile(testFile, []byte("modified content"), 0644)
	if err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	select {
	case <-triggerChan:
		// Event received - success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for file modification event")
	}

	err = os.Remove(testFile)
	if err != nil {
		t.Fatalf("failed to delete test file: %v", err)
	}

	select {
	case <-triggerChan:
		// Event received - success
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for file deletion event")
	}
}

func TestFileWatcherDebouncing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filewatcher-debounce-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	triggerChan := make(chan event.GenericEvent, 10)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	fw, err := New(tmpDir, triggerChan, logger)
	if err != nil {
		t.Fatalf("failed to create file watcher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = fw.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start file watcher: %v", err)
	}

	testFile := filepath.Join(tmpDir, "debounce-test.yaml")

	// Make rapid successive changes (5 changes within 200ms)
	for i := 0; i < 5; i++ {
		err = os.WriteFile(testFile, []byte(fmt.Sprintf("content-%d", i)), 0644)
		if err != nil {
			t.Fatalf("failed to write test file iteration %d: %v", i, err)
		}
		time.Sleep(40 * time.Millisecond) // 40ms between changes = 200ms total
	}

	// Should receive only ONE event after debounce window
	eventCount := 0
	timeout := time.After(1 * time.Second)

	for {
		select {
		case <-triggerChan:
			eventCount++
		case <-timeout:
			if eventCount != 1 {
				t.Errorf("expected 1 debounced event, got %d", eventCount)
			}
			return
		}
	}
}

func TestFileWatcherLifecycle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filewatcher-lifecycle-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	triggerChan := make(chan event.GenericEvent, 10)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Test New() with invalid parameters
	_, err = New("", triggerChan, logger)
	if err == nil {
		t.Error("expected error with empty watch directory")
	}

	_, err = New(tmpDir, nil, logger)
	if err == nil {
		t.Error("expected error with nil trigger channel")
	}

	_, err = New(tmpDir, triggerChan, nil)
	if err == nil {
		t.Error("expected error with nil logger")
	}

	// Test valid New()
	fw, err := New(tmpDir, triggerChan, logger)
	if err != nil {
		t.Fatalf("failed to create file watcher: %v", err)
	}

	// Test Start()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = fw.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start file watcher: %v", err)
	}

	// Test double start (should error)
	err = fw.Start(ctx)
	if err == nil {
		t.Error("expected error when starting already started watcher")
	}

	// Cancel context - watcher should stop automatically
	cancel()
	time.Sleep(100 * time.Millisecond) // Give time for goroutine to exit and cleanup

	// Test context cancellation with new watcher
	fw2, err := New(tmpDir, triggerChan, logger)
	if err != nil {
		t.Fatalf("failed to create second file watcher: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	err = fw2.Start(ctx2)
	if err != nil {
		t.Fatalf("failed to start second file watcher: %v", err)
	}

	// Cancel context and verify cleanup happens
	cancel2()
	time.Sleep(100 * time.Millisecond) // Give time for goroutine to exit and cleanup
}

func TestFileWatcherNonExistentDirectory(t *testing.T) {
	triggerChan := make(chan event.GenericEvent, 10)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	fw, err := New("/nonexistent/directory", triggerChan, logger)
	if err != nil {
		t.Fatalf("failed to create file watcher: %v", err)
	}

	ctx := context.Background()
	err = fw.Start(ctx)
	if err == nil {
		t.Error("expected error when starting watcher on non-existent directory")
	}
}
