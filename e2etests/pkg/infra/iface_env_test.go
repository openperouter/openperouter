// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"testing"
)

func TestResolveUnderlayHostIfacesDefaults(t *testing.T) {
	cfg, err := ResolveUnderlayHostIfaces(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Iface1 != "toswitch1" {
		t.Fatalf("default iface1 = %q, want toswitch1", cfg.Iface1)
	}
	if cfg.Iface2 != "toswitch2" {
		t.Fatalf("default iface2 = %q, want toswitch2", cfg.Iface2)
	}
	if !cfg.DualLeaf {
		t.Fatalf("DualLeaf = false, want true for kind defaults")
	}
	if got := cfg.Ifaces(); len(got) != 2 || got[0] != "toswitch1" || got[1] != "toswitch2" {
		t.Fatalf("Ifaces() = %v, want [toswitch1 toswitch2]", got)
	}
}

func TestResolveUnderlayHostIfacesDualLeaf(t *testing.T) {
	cfg, err := ResolveUnderlayHostIfaces([]string{"eth1", "eth2"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Iface1 != "eth1" {
		t.Fatalf("iface1 = %q, want eth1", cfg.Iface1)
	}
	if cfg.Iface2 != "eth2" {
		t.Fatalf("iface2 = %q, want eth2", cfg.Iface2)
	}
	if !cfg.DualLeaf {
		t.Fatalf("DualLeaf = false, want true when two ifaces set")
	}
}

func TestResolveUnderlayHostIfacesSingleUplink(t *testing.T) {
	cfg, err := ResolveUnderlayHostIfaces([]string{"eth1"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Iface1 != "eth1" {
		t.Fatalf("iface1 = %q, want eth1", cfg.Iface1)
	}
	if cfg.Iface2 != "toswitch2" {
		t.Fatalf("iface2 = %q, want default toswitch2 (held but unused)", cfg.Iface2)
	}
	if cfg.DualLeaf {
		t.Fatalf("DualLeaf = true, want false for single-uplink")
	}
	if got := cfg.Ifaces(); len(got) != 1 || got[0] != "eth1" {
		t.Fatalf("Ifaces() = %v, want [eth1]", got)
	}

	ifaces := buildDefaultInterfaces(cfg)
	if len(ifaces) != 1 {
		t.Fatalf("buildDefaultInterfaces len = %d, want 1 when DualLeaf false", len(ifaces))
	}
	if ifaces[0].NetworkDevice.InterfaceName != "eth1" {
		t.Fatalf("iface = %q, want eth1", ifaces[0].NetworkDevice.InterfaceName)
	}
}

func TestResolveUnderlayHostIfacesTooMany(t *testing.T) {
	_, err := ResolveUnderlayHostIfaces([]string{"a", "b", "c"})
	if err == nil {
		t.Fatal("expected error for more than two underlayHostIfaces")
	}
}

func TestResolveUnderlayHostIfacesEmptyEntry(t *testing.T) {
	_, err := ResolveUnderlayHostIfaces([]string{""})
	if err == nil {
		t.Fatal("expected error for empty underlayHostIfaces entry")
	}
}

func TestBuildDefaultInterfacesDualLeaf(t *testing.T) {
	cfg, err := ResolveUnderlayHostIfaces(nil)
	if err != nil {
		t.Fatal(err)
	}
	ifaces := buildDefaultInterfaces(cfg)
	if len(ifaces) != 2 {
		t.Fatalf("buildDefaultInterfaces len = %d, want 2 when DualLeaf true", len(ifaces))
	}
	if ifaces[0].NetworkDevice.InterfaceName != "toswitch1" {
		t.Fatalf("iface[0] = %q, want toswitch1", ifaces[0].NetworkDevice.InterfaceName)
	}
	if ifaces[1].NetworkDevice.InterfaceName != "toswitch2" {
		t.Fatalf("iface[1] = %q, want toswitch2", ifaces[1].NetworkDevice.InterfaceName)
	}
}

func TestApplyUnderlayNeighborsDualLeaf(t *testing.T) {
	t.Cleanup(func() {
		cfg, err := ResolveUnderlayHostIfaces(nil)
		if err != nil {
			t.Error(err)
			return
		}
		ConfigureUnderlayIfaces(cfg)
	})

	cfg, err := ResolveUnderlayHostIfaces(nil)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureUnderlayIfaces(cfg)
	if len(Underlay.Spec.Neighbors) != 2 {
		t.Fatalf("Underlay neighbors = %d, want 2 when DualLeaf true", len(Underlay.Spec.Neighbors))
	}
	if len(UnderlayListenRange.Spec.Neighbors) != 2 {
		t.Fatalf("UnderlayListenRange neighbors = %d, want 2 when DualLeaf true", len(UnderlayListenRange.Spec.Neighbors))
	}

	cfg, err = ResolveUnderlayHostIfaces([]string{"eth1"})
	if err != nil {
		t.Fatal(err)
	}
	ConfigureUnderlayIfaces(cfg)
	if len(Underlay.Spec.Neighbors) != 1 {
		t.Fatalf("Underlay neighbors = %d, want 1 when DualLeaf false", len(Underlay.Spec.Neighbors))
	}
	if Underlay.Spec.Neighbors[0].Address == nil || *Underlay.Spec.Neighbors[0].Address != "192.168.11.2" {
		t.Fatalf("primary neighbor = %#v, want 192.168.11.2", Underlay.Spec.Neighbors[0].Address)
	}
	if len(Underlay.Spec.Interfaces) != 1 {
		t.Fatalf("Underlay interfaces = %d, want 1 when DualLeaf false", len(Underlay.Spec.Interfaces))
	}
	if len(UnderlayListenRange.Spec.Neighbors) != 1 {
		t.Fatalf("UnderlayListenRange neighbors = %d, want 1 when DualLeaf false", len(UnderlayListenRange.Spec.Neighbors))
	}

	// Restoring DualLeaf rebuilds the second neighbor.
	cfg, err = ResolveUnderlayHostIfaces(nil)
	if err != nil {
		t.Fatal(err)
	}
	ConfigureUnderlayIfaces(cfg)
	if len(Underlay.Spec.Neighbors) != 2 {
		t.Fatalf("Underlay neighbors = %d after restore, want 2", len(Underlay.Spec.Neighbors))
	}
}
