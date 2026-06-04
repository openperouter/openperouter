// SPDX-License-Identifier:Apache-2.0

package netnamespace

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const NamedNSName = "perouter"
const NamedNSPath = "/var/run/netns/" + NamedNSName

// EnsureNamespace ensures the named network namespace "perouter" exists at
// /var/run/netns/perouter. It is idempotent: if the namespace already exists
// and is valid, it returns nil.
func EnsureNamespace() error {
	ns, err := netns.GetFromPath(NamedNSPath)
	if err == nil {
		if closeErr := ns.Close(); closeErr != nil {
			slog.Error("failed to close namespace handle", "error", closeErr)
		}
		slog.Debug("named netns already exists", "path", NamedNSPath)
		return nil
	}

	slog.Info("creating named netns", "path", NamedNSPath)
	out, err := exec.Command("ip", "netns", "add", NamedNSName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create named netns: %w, output: %s", err, string(out))
	}

	// Verify the namespace was created successfully
	ns, err = netns.GetFromPath(NamedNSPath)
	if err != nil {
		return fmt.Errorf("named netns created but failed to verify: %w", err)
	}
	if closeErr := ns.Close(); closeErr != nil {
		slog.Error("failed to close namespace handle", "error", closeErr)
	}

	slog.Info("named netns created successfully", "path", NamedNSPath)
	return nil
}

// DeleteNamespace removes the named network namespace "perouter".
func DeleteNamespace() error {
	slog.Info("deleting named netns", "path", NamedNSPath)

	if err := deleteAllDevices(NamedNSPath); err != nil {
		slog.Warn("pre-deletion of devices failed, proceeding with netns delete", "error", err)
	}

	out, err := exec.Command("ip", "netns", "delete", NamedNSName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete named netns: %w, output: %s", err, string(out))
	}
	return nil
}

// deleteAllDevices enters the given network namespace and deletes all
// non-loopback devices. This accelerates kernel netns cleanup by reducing
// the number of devices that cleanup_net() must tear down asynchronously.
// Deletion is best-effort: individual failures are accumulated and returned
// as a joined error, but do not prevent deletion of remaining devices.
func deleteAllDevices(nsPath string) error {
	ns, err := netns.GetFromPath(nsPath)
	if err != nil {
		return fmt.Errorf("failed to open netns %s: %w", nsPath, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace handle", "namespace", nsPath, "error", err)
		}
	}()

	return In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list devices: %w", err)
		}

		slog.Info("pre-deleting devices from netns", "path", nsPath, "count", len(links))

		var failedDeletes []error
		for _, l := range links {
			name := l.Attrs().Name
			if name == "lo" {
				continue
			}
			if err := netlink.LinkDel(l); err != nil {
				var notFound netlink.LinkNotFoundError
				if errors.As(err, &notFound) {
					continue
				}
				if errors.Is(err, syscall.EOPNOTSUPP) {
					continue
				}
				slog.Warn("failed to delete device", "device", name, "error", err)
				failedDeletes = append(failedDeletes, fmt.Errorf("delete %s: %w", name, err))
				continue
			}
			slog.Info("deleted device from netns", "device", name)
		}
		return errors.Join(failedDeletes...)
	})
}
