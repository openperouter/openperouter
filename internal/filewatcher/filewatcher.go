// SPDX-License-Identifier:Apache-2.0

package filewatcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// FileWatcher monitors a directory for configuration file changes
// and triggers reconciliation through a channel.
type FileWatcher struct {
	watchDir         string
	logger           *slog.Logger
	debounceDuration time.Duration

	watcher     *fsnotify.Watcher
	triggerChan chan<- event.GenericEvent
}

// New creates a new FileWatcher for the specified directory.
// triggerChan is where reconciliation events are sent.
func New(watchDir string, triggerChan chan<- event.GenericEvent, logger *slog.Logger) (*FileWatcher, error) {
	if watchDir == "" {
		return nil, fmt.Errorf("watch directory cannot be empty")
	}
	if triggerChan == nil {
		return nil, fmt.Errorf("trigger channel cannot be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	return &FileWatcher{
		watchDir:         watchDir,
		logger:           logger,
		triggerChan:      triggerChan,
		debounceDuration: 500 * time.Millisecond, // 500ms debounce window
	}, nil
}

// Start begins watching the directory for changes.
// Returns immediately; watching happens in background goroutine.
// Cleanup is handled automatically when context is cancelled.
func (fw *FileWatcher) Start(ctx context.Context) error {
	if fw.watcher != nil {
		return fmt.Errorf("file watcher already started")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	fw.watcher = watcher

	err = fw.watcher.Add(fw.watchDir)
	if err != nil {
		_ = fw.watcher.Close()
		fw.watcher = nil
		return fmt.Errorf("failed to add watch directory %s: %w", fw.watchDir, err)
	}

	fw.logger.Info("file watcher started", "directory", fw.watchDir, "debounce", fw.debounceDuration)

	// Start background goroutine for event processing
	go fw.watchLoop(ctx)

	return nil
}

// watchLoop runs in background goroutine to process file system events
func (fw *FileWatcher) watchLoop(ctx context.Context) {
	defer func() {
		if err := fw.watcher.Close(); err != nil {
			fw.logger.Error("error closing watcher", "error", err)
		}
		fw.logger.Info("file watcher stopped")
	}()

	timeOut := make(<-chan time.Time)
	for {
		timerSet := false
		select {
		case <-ctx.Done():
			fw.logger.Debug("file watcher context cancelled")
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				fw.logger.Debug("file watcher events channel closed")
				return
			}

			if !timerSet {
				timeOut = time.After(fw.debounceDuration)
				timerSet = true
			}
			fw.logEvent(event)

		case <-timeOut:
			timerSet = false
			fw.logger.Debug("file watcher: trigger reconciliaton")
			fw.triggerReconciliation()

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				fw.logger.Debug("file watcher errors channel closed")
				return
			}

			fw.logger.Error("file watcher error", "error", err)
			// Continue watching despite errors
		}
	}
}

// handleEvent processes a file system event with debouncing
func (fw *FileWatcher) logEvent(fileEvent fsnotify.Event) {
	switch {
	case fileEvent.Op&fsnotify.Write == fsnotify.Write:
		fw.logger.Info("static file modified", "path", fileEvent.Name, "op", "WRITE", "source", "static")
	case fileEvent.Op&fsnotify.Create == fsnotify.Create:
		fw.logger.Info("static file created", "path", fileEvent.Name, "op", "CREATE", "source", "static")
	case fileEvent.Op&fsnotify.Remove == fsnotify.Remove:
		fw.logger.Info("static file removed", "path", fileEvent.Name, "op", "REMOVE", "source", "static")
	case fileEvent.Op&fsnotify.Rename == fsnotify.Rename:
		fw.logger.Info("static file renamed", "path", fileEvent.Name, "op", "RENAME", "source", "static")
	case fileEvent.Op&fsnotify.Chmod == fsnotify.Chmod:
		fw.logger.Debug("chmod event", "path", fileEvent.Name, "op", "CHMOD")
	default:
		fw.logger.Info("static file event", "path", fileEvent.Name, "op", fileEvent.Op, "source", "static")
	}
}

// triggerReconciliation sends an event to the trigger channel
func (fw *FileWatcher) triggerReconciliation() {
	select {
	case fw.triggerChan <- event.GenericEvent{
		Object: &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "static-config-trigger",
				Namespace: "default",
			},
		},
	}:
		fw.logger.Info("triggered reconciliation from static file change", "source", "static", "directory", fw.watchDir)
	default:
		fw.logger.Debug("reconciliation already queued, skipping trigger", "source", "static")
	}
}
