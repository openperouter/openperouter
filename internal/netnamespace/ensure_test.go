// SPDX-License-Identifier:Apache-2.0

package netnamespace

import (
	"os"
	"os/exec"
	"testing"

	"github.com/vishvananda/netns"
)

func TestEnsureNamespace(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("skipping test: requires root privileges")
	}

	// Clean up in case it already exists from a previous test run
	_ = exec.Command("ip", "netns", "delete", "perouter").Run()

	// First call should create the namespace
	if err := EnsureNamespace(); err != nil {
		t.Fatalf("first EnsureNamespace() failed: %v", err)
	}

	// Verify the namespace exists
	ns, err := netns.GetFromPath(NamedNSPath)
	if err != nil {
		t.Fatalf("namespace should exist after EnsureNamespace(): %v", err)
	}
	ns.Close()

	// Second call should be idempotent (no error)
	if err := EnsureNamespace(); err != nil {
		t.Fatalf("second EnsureNamespace() should be idempotent: %v", err)
	}

	// Clean up
	if err := exec.Command("ip", "netns", "delete", "perouter").Run(); err != nil {
		t.Logf("cleanup: failed to delete netns: %v", err)
	}
}
