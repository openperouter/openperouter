// SPDX-License-Identifier:Apache-2.0

// Package pci resolves kernel netdevs to PCI addresses and manages DPDK
// driver binding for grout-accelerated underlay ports.
package pci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

const (
	DriverVFIOPCI  = "vfio-pci"
	DriverMlx5Core = "mlx5_core"
)

// Sysfs resolves and binds PCI devices under Root.
type Sysfs struct {
	Root string
	// Probe overrides the drivers_probe write to emulate kernel binding in tests.
	// When nil, probing writes to drivers_probe under Root.
	Probe func(pciAddr string) error
}

var pciAddressRegex = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`)

// IsBifurcated reports whether the kernel driver shares the device with
// DPDK (e.g. mlx5) instead of requiring a vfio-pci rebind.
func IsBifurcated(driver string) bool {
	return driver == DriverMlx5Core
}

// PCIAddressForNetDevice takes a kernel netlink device name and returns its PCI
// address by reading the "device" symlink under the device's sysfs
// class/net directory.
func (s Sysfs) PCIAddressForNetDevice(name string) (string, error) {
	deviceLink := filepath.Join(s.Root, "class", "net", name, "device")
	target, err := os.Readlink(deviceLink)
	if err != nil {
		return "", fmt.Errorf("failed to resolve netlink device %q to PCI address: %w", name, err)
	}
	pciAddr := filepath.Base(target)
	if !isPCIAddress(pciAddr) {
		return "", fmt.Errorf("resolved device symlink target %q for %q does not look like a PCI address", pciAddr, name)
	}
	return pciAddr, nil
}

// isPCIAddress reports whether s is a PCI BDF address (DDDD:BB:DD.F).
func isPCIAddress(s string) bool {
	return pciAddressRegex.MatchString(s)
}
