// SPDX-License-Identifier:Apache-2.0

package netnamespace

import (
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/vishvananda/netns"
)

type SetNamespaceError string

func (i SetNamespaceError) Error() string {
	return string(i)
}

// IsCurrent compares namespace identities, including paths that alias the same
// namespace. Call it from the host-networked controller, outside In callbacks.
func IsCurrent(path string) (bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	target, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat target namespace %s: %w", path, err)
	}
	current, err := os.Stat("/proc/thread-self/ns/net")
	if err != nil {
		return false, fmt.Errorf("stat current namespace: %w", err)
	}
	return os.SameFile(target, current), nil
}

// In execs the provided function in the given network namespace.
func In(ns netns.NsHandle, execInNamespace func() error) error {
	// required as a change of context might wake up the goroutine
	// in a different thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origns, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get current network namespace")
	}
	defer func() {
		if err := origns.Close(); err != nil {
			slog.Error("failed to close default namespace", "error", err)
		}
	}()

	if err := netns.Set(ns); err != nil {
		return SetNamespaceError(fmt.Sprintf("failed to set current network namespace to %s", ns.String()))
	}

	defer func() {
		if err := netns.Set(origns); err != nil {
			slog.Error("failed to set default namespace", "error", err)
		}
	}()

	if err := execInNamespace(); err != nil {
		return err
	}
	return nil
}
