// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/vishvananda/netns"
)

// EnsureIPv6Forwarding checks if IPv6 forwarding is enabled in the target namespace
// and enables it if not already set to 1.
func EnsureIPv6Forwarding(namespace string) error {
	ns, err := netns.GetFromPath(namespace)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", namespace, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("ensureIPv6Forwarding: failed to close namespace", "error", err, "namespace", namespace)
		}
	}()

	err = inNamespace(ns, func() error {
		// Read current value from sysfs
		data, err := os.ReadFile("/proc/sys/net/ipv6/conf/all/forwarding")
		if err != nil {
			return fmt.Errorf("failed to read /proc/sys/net/ipv6/conf/all/forwarding: %w", err)
		}
		currentValue := strings.TrimSpace(string(data))

		// Check if already enabled
		if currentValue == "1" {
			return nil
		}

		// Write 1 to enable forwarding
		if err := os.WriteFile("/proc/sys/net/ipv6/conf/all/forwarding", []byte("1"), 0644); err != nil {
			return fmt.Errorf("failed to write to /proc/sys/net/ipv6/conf/all/forwarding: %w", err)
		}
		slog.Info("IPv6 forwarding enabled", "namespace", namespace)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to ensure IPv6 forwarding: %w", err)
	}
	return nil
}
