// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package netnamespace

import (
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func TestEnsureNamespace(t *testing.T) {
	// First call should create the namespace
	if err := EnsureNamespace(); err != nil {
		t.Fatalf("first EnsureNamespace() failed: %v", err)
	}

	// Verify the namespace exists
	ns, err := netns.GetFromPath(NamedNSPath)
	if err != nil {
		t.Fatalf("namespace should exist after EnsureNamespace(): %v", err)
	}
	if ns.Close() != nil {
		t.Fatalf("namespace should close after EnsureNamespace(): %v", err)
	}

	// Second call should be idempotent (no error)
	if err := EnsureNamespace(); err != nil {
		t.Fatalf("second EnsureNamespace() should be idempotent: %v", err)
	}

	// Clean up
	if err := DeleteNamespace(); err != nil {
		t.Logf("cleanup: failed to delete netns: %v", err)
	}
}

func TestDeleteNamespacePreDeletesDevices(t *testing.T) {
	if err := EnsureNamespace(); err != nil {
		t.Fatalf("EnsureNamespace() failed: %v", err)
	}

	ns, err := netns.GetFromPath(NamedNSPath)
	if err != nil {
		t.Fatalf("failed to open netns: %v", err)
	}
	defer ns.Close()

	// Add a dummy device and a veth pair inside the netns
	if err := In(ns, func() error {
		if err := netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "test-dummy"}}); err != nil {
			return err
		}
		return netlink.LinkAdd(&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{Name: "test-veth0"},
			PeerName:  "test-veth1",
		})
	}); err != nil {
		t.Fatalf("failed to add devices to netns: %v", err)
	}

	// DeleteNamespace should pre-delete devices then remove the netns
	if err := DeleteNamespace(); err != nil {
		t.Fatalf("DeleteNamespace() failed: %v", err)
	}

	// Verify the namespace is gone
	if _, err := netns.GetFromPath(NamedNSPath); err == nil {
		t.Fatal("namespace should not exist after DeleteNamespace()")
	}
}

func TestDeleteNamespaceEmptyNetns(t *testing.T) {
	if err := EnsureNamespace(); err != nil {
		t.Fatalf("EnsureNamespace() failed: %v", err)
	}

	// DeleteNamespace on a netns with only lo should succeed
	if err := DeleteNamespace(); err != nil {
		t.Fatalf("DeleteNamespace() on empty netns failed: %v", err)
	}

	if _, err := netns.GetFromPath(NamedNSPath); err == nil {
		t.Fatal("namespace should not exist after DeleteNamespace()")
	}
}

func TestDeleteNamespaceNonexistent(t *testing.T) {
	// DeleteNamespace should return an error from ip netns delete
	if err := DeleteNamespace(); err == nil {
		t.Fatal("DeleteNamespace() on nonexistent netns should fail")
	}
}
