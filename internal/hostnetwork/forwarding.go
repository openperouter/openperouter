// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/openperouter/openperouter/internal/netnamespace"
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

	err = netnamespace.In(ns, func() error {
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

// EnsureArpAccept enables arp_accept on all interfaces in the target namespace.
// This allows the kernel to create neighbor entries from received Gratuitous ARP packets,
// which is critical for fast EVPN MAC/IP route advertisement during VM migrations.
// Without this, the kernel ignores GARP packets and FRR cannot advertise MAC/IP routes
// until a regular ARP exchange occurs (which can take 20+ seconds).
func EnsureArpAccept(namespace string) error {
	ns, err := netns.GetFromPath(namespace)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", namespace, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("ensureArpAccept: failed to close namespace", "error", err, "namespace", namespace)
		}
	}()

	err = netnamespace.In(ns, func() error {
		// Set arp_accept on "all" interface - this affects all current interfaces
		allPath := "/proc/sys/net/ipv4/conf/all/arp_accept"
		data, err := os.ReadFile(allPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", allPath, err)
		}
		currentValue := strings.TrimSpace(string(data))

		if currentValue != "1" {
			if err := os.WriteFile(allPath, []byte("1"), 0644); err != nil {
				return fmt.Errorf("failed to write to %s: %w", allPath, err)
			}
			slog.Info("arp_accept enabled on all interfaces", "namespace", namespace)
		}

		// Set arp_accept on "default" interface - this affects newly created interfaces
		defaultPath := "/proc/sys/net/ipv4/conf/default/arp_accept"
		data, err = os.ReadFile(defaultPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", defaultPath, err)
		}
		currentValue = strings.TrimSpace(string(data))

		if currentValue != "1" {
			if err := os.WriteFile(defaultPath, []byte("1"), 0644); err != nil {
				return fmt.Errorf("failed to write to %s: %w", defaultPath, err)
			}
			slog.Info("arp_accept enabled on default interface", "namespace", namespace)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to ensure arp_accept: %w", err)
	}
	return nil
}
