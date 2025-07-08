package hostnetwork

import (
	"fmt"
	"os"
	"strings"

	"github.com/vishvananda/netns"
)

// EnsureIPv6Forwarding checks if IPv6 forwarding is enabled in the target namespace
// and enables it if not already set to 1.
func EnsureIPv6Forwarding(namespace string) error {
	ns, err := netns.GetFromName(namespace)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", namespace, err)
	}
	defer ns.Close()

	var checkErr, setErr error
	var currentValue string

	err = inNamespace(ns, func() error {
		// Read current value from sysfs
		data, err := os.ReadFile("/proc/sys/net/ipv6/conf/all/forwarding")
		checkErr = err
		if err != nil {
			return err
		}
		currentValue = strings.TrimSpace(string(data))

		// Check if already enabled
		if currentValue == "1" {
			return nil
		}

		// Write 1 to enable forwarding
		setErr = os.WriteFile("/proc/sys/net/ipv6/conf/all/forwarding", []byte("1"), 0644)
		return setErr
	})
	if err != nil {
		return fmt.Errorf("failed to ensure IPv6 forwarding: %w", err)
	}
	if checkErr != nil {
		return fmt.Errorf("failed to check IPv6 forwarding status: %w", checkErr)
	}
	if setErr != nil {
		return fmt.Errorf("failed to enable IPv6 forwarding: %w", setErr)
	}
	return nil
}
