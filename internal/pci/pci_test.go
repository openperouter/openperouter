// SPDX-License-Identifier:Apache-2.0

package pci

import (
	"os"
	"path/filepath"
	"testing"
)

const testPCIAddress = "0000:03:02.0"

func TestIsPCIAddress(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{s: "0000:01:00.0", want: true},
		{s: "0000:03:02.7", want: true},
		{s: "ffff:ff:ff.7", want: true},
		{s: "invalid", want: false},
		{s: "net_tap0,remote=eth0,iface=tap_eth0", want: false},
		{s: "0000:01:00.8", want: false},
		{s: "00:01:00.0", want: false},
		{s: "", want: false},
	}
	for _, tt := range tests {
		if got := IsPCIAddress(tt.s); got != tt.want {
			t.Errorf("IsPCIAddress(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestIsBifurcated(t *testing.T) {
	if !IsBifurcated(DriverMlx5Core) {
		t.Fatal("expected mlx5_core to be bifurcated")
	}
	if IsBifurcated("iavf") {
		t.Fatal("expected iavf not to be bifurcated")
	}
}

func TestResolveNetlinkName(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	name := "enp3s0f0v0"
	netDir := filepath.Join(SysfsRoot, "class", "net", name)
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../devices/"+testPCIAddress, filepath.Join(netDir, "device")); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveNetlinkName(name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testPCIAddress {
		t.Fatalf("expected %s, got %s", testPCIAddress, got)
	}
}

func TestResolveNetlinkName_MissingDevice(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	if _, err := ResolveNetlinkName("does-not-exist"); err == nil {
		t.Fatal("expected error for missing netdev")
	}
}

func TestResolveNetlinkName_NotPCI(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	name := "virbr0"
	netDir := filepath.Join(SysfsRoot, "class", "net", name)
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../devices/virtual/net/"+name, filepath.Join(netDir, "device")); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveNetlinkName(name); err == nil {
		t.Fatal("expected error for non-PCI symlink target")
	}
}
