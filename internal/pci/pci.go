// SPDX-License-Identifier:Apache-2.0

// Package pci resolves kernel netdevs to PCI addresses and manages DPDK
// driver binding for grout-accelerated underlay ports.
package pci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/vishvananda/netlink"
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

// emulatedVFsEnv is a temporary test-only knob, read here rather than in main
// to keep it out of the controller configuration. When set, the VFs of a PF
// resolve to the netdevs named "<pf>v<index>" instead of the PF's virtfn
// symlinks, for test environments where independent NICs on a bridge emulate
// the VFs.
const emulatedVFsEnv = "TEST_FLAG_DO_NOT_USE_EMULATED_SRIOV_VFS"

var pciAddressRegex = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`)
var virtioDeviceRegex = regexp.MustCompile(`^virtio[0-9]+$`)

// IsBifurcated reports whether the kernel driver shares the device with
// DPDK (e.g. mlx5) instead of requiring a vfio-pci rebind.
func IsBifurcated(driver string) bool {
	return driver == DriverMlx5Core
}

// PCIAddressForNetDevice takes a kernel netlink device name and returns its PCI
// address by reading the "device" symlink under the device's sysfs
// class/net directory. Virtio netdevs have a virtio device below the PCI device.
func (s Sysfs) PCIAddressForNetDevice(name string) (string, error) {
	deviceLink := filepath.Join(s.Root, "class", "net", name, "device")
	target, err := filepath.EvalSymlinks(deviceLink)
	if err != nil {
		return "", fmt.Errorf("failed to resolve netlink device %q to PCI address: %w", name, err)
	}
	pciAddr := filepath.Base(target)
	if virtioDeviceRegex.MatchString(pciAddr) {
		pciAddr = filepath.Base(filepath.Dir(target))
	}
	if !isPCIAddress(pciAddr) {
		return "", fmt.Errorf("resolved device symlink target %q for %q does not look like a PCI address", pciAddr, name)
	}
	return pciAddr, nil
}

// PCIAddressForVF returns the PCI address of the VF with the given index on
// the PF netdev, by reading the PF's "virtfn<index>" sysfs symlink.
// pfName may be an alternative name; sysfs only exposes the primary name.
func (s Sysfs) PCIAddressForVF(pfName string, vfIndex int) (string, error) {
	link, err := netlink.LinkByName(pfName)
	switch {
	case err == nil:
		pfName = link.Attrs().Name
	case !errors.As(err, &netlink.LinkNotFoundError{}):
		return "", fmt.Errorf("failed to find PF %s: %w", pfName, err)
	}

	if os.Getenv(emulatedVFsEnv) != "" {
		return s.PCIAddressForNetDevice(emulatedVFName(pfName, vfIndex))
	}
	vfLink := filepath.Join(s.Root, "class", "net", pfName, "device", fmt.Sprintf("virtfn%d", vfIndex))
	target, err := os.Readlink(vfLink)
	if err != nil {
		return "", fmt.Errorf("failed to resolve VF %d of PF %q to PCI address: %w", vfIndex, pfName, err)
	}
	pciAddr := filepath.Base(target)
	if !isPCIAddress(pciAddr) {
		return "", fmt.Errorf("resolved VF symlink target %q for VF %d of %q does not look like a PCI address",
			pciAddr, vfIndex, pfName)
	}
	return pciAddr, nil
}

// emulatedVFName names the netdev emulating the VF with the given index on
// the PF.
func emulatedVFName(pfName string, vfIndex int) string {
	return fmt.Sprintf("%sv%d", pfName, vfIndex)
}

// isPCIAddress reports whether s is a PCI BDF address (DDDD:BB:DD.F).
func isPCIAddress(s string) bool {
	return pciAddressRegex.MatchString(s)
}
