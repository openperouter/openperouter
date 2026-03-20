// SPDX-License-Identifier:Apache-2.0

package netnamespace

import (
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/vishvananda/netns"
)

const NamedNSPath = "/var/run/netns/perouter"

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
	out, err := exec.Command("ip", "netns", "add", "perouter").CombinedOutput()
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
