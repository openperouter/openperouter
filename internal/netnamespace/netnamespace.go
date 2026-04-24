// SPDX-License-Identifier:Apache-2.0

package netnamespace

import (
	"fmt"
	"log/slog"
	"runtime"

	"github.com/vishvananda/netns"
)

var hostNS netns.NsHandle

// InitHostNS opens the host network namespace from the given path
// and stores it for use by InHost(). Must be called once at startup.
func InitHostNS(path string) error {
	ns, err := netns.GetFromPath(path)
	if err != nil {
		return fmt.Errorf("failed to open host network namespace from %s: %w", path, err)
	}
	hostNS = ns
	return nil
}

// InHost executes the given function inside the host network namespace.
func InHost(fn func() error) error {
	if hostNS == 0 {
		return fmt.Errorf("host network namespace not initialized; call InitHostNS first")
	}
	return In(hostNS, fn)
}

// HostNS returns the stored host network namespace handle.
func HostNS() netns.NsHandle {
	return hostNS
}

type SetNamespaceError string

func (i SetNamespaceError) Error() string {
	return string(i)
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
