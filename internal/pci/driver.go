// SPDX-License-Identifier:Apache-2.0

package pci

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// DriverForPCIAddress returns the name of the kernel driver currently bound to
// the given PCI device. It reads the "driver" symlink under the device's
// sysfs directory. If no driver is bound the returned string is empty.
func (s Sysfs) DriverForPCIAddress(pciAddr string) (string, error) {
	driverLink := filepath.Join(s.Root, "bus", "pci", "devices", pciAddr, "driver")
	target, err := os.Readlink(driverLink)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read driver for PCI device %s: %w", pciAddr, err)
	}
	return filepath.Base(target), nil
}

// NetDeviceForPCIAddress returns the name of the kernel network interface
// backed by the given PCI device. Virtio netdevs live under a virtio child
// of the PCI device rather than directly under its net/ directory.
func (s Sysfs) NetDeviceForPCIAddress(pciAddr string) (string, error) {
	deviceDir := filepath.Join(s.Root, "bus", "pci", "devices", pciAddr)
	netDirs := []string{filepath.Join(deviceDir, "net")}
	virtioNetDirs, err := filepath.Glob(filepath.Join(deviceDir, "virtio[0-9]*", "net"))
	if err != nil {
		return "", fmt.Errorf("failed to find virtio net directories for PCI device %s: %w", pciAddr, err)
	}
	for _, netDir := range append(netDirs, virtioNetDirs...) {
		entries, err := os.ReadDir(netDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to read kernel net devices under %s: %w", netDir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				return e.Name(), nil
			}
		}
	}
	return "", fmt.Errorf("no kernel net device found for PCI device %s", pciAddr)
}

// RestoreDriver rebinds a PCI device from vfio-pci back to its original
// kernel driver. It clears the driver_override, unbinds from vfio-pci,
// and binds the original driver.
func (s Sysfs) RestoreDriver(pciAddr, originalDriver string) error {
	current, err := s.DriverForPCIAddress(pciAddr)
	if err != nil {
		return err
	}
	if current == originalDriver {
		slog.Debug("driver already bound to original driver", "pciAddr", pciAddr, "originalDriver", originalDriver)
		return nil
	}

	devicePath := filepath.Join(s.Root, "bus", "pci", "devices", pciAddr)

	if err := os.WriteFile(filepath.Join(devicePath, "driver_override"),
		[]byte("\x00"), 0o644); err != nil {
		return fmt.Errorf("failed to clear driver_override for %s: %w", pciAddr, err)
	}

	if current != "" {
		unbindPath := filepath.Join(devicePath, "driver", "unbind")
		if err := os.WriteFile(unbindPath, []byte(pciAddr), 0o644); err != nil {
			return fmt.Errorf("failed to unbind driver %s from %s: %w", current, pciAddr, err)
		}
	}

	bindPath := filepath.Join(s.Root, "bus", "pci", "drivers", originalDriver, "bind")
	if err := os.WriteFile(bindPath, []byte(pciAddr), 0o644); err != nil {
		return fmt.Errorf("failed to bind %s to driver %s: %w", pciAddr, originalDriver, err)
	}

	return nil
}

// BindVFIOPCI rebinds a PCI device to the vfio-pci driver.
// It is a no-op if the device is already bound to vfio-pci.
func (s Sysfs) BindVFIOPCI(pciAddr string) error {
	current, err := s.DriverForPCIAddress(pciAddr)
	if err != nil {
		return err
	}
	if current == DriverVFIOPCI {
		return nil
	}

	loaded, err := s.isVFIODriverLoaded()
	if err != nil {
		return fmt.Errorf("failed to check if vfio-pci driver is loaded: %w", err)
	}
	if !loaded {
		return fmt.Errorf("vfio-pci driver is not loaded; load the module before binding DPDK ports")
	}

	devicePath := filepath.Join(s.Root, "bus", "pci", "devices", pciAddr)

	if err := os.WriteFile(filepath.Join(devicePath, "driver_override"),
		[]byte(DriverVFIOPCI), 0o644); err != nil {
		return fmt.Errorf("failed to set driver_override to vfio-pci for %s: %w", pciAddr, err)
	}

	if current != "" {
		unbindPath := filepath.Join(devicePath, "driver", "unbind")
		if err := os.WriteFile(unbindPath, []byte(pciAddr), 0o644); err != nil {
			return fmt.Errorf("failed to unbind driver %s from %s: %w", current, pciAddr, err)
		}
	}

	if err := s.probeDriver(pciAddr); err != nil {
		return fmt.Errorf("failed to probe driver for %s: %w", pciAddr, err)
	}

	// A successful drivers_probe write does not mean vfio-pci bound: the probe
	// can fail silently, e.g. when the device has no IOMMU group.
	bound, err := s.DriverForPCIAddress(pciAddr)
	if err != nil {
		return err
	}
	if bound != DriverVFIOPCI {
		return fmt.Errorf("PCI device %s is bound to %q instead of %s after probe; check that the IOMMU is enabled",
			pciAddr, bound, DriverVFIOPCI)
	}

	return nil
}

func (s Sysfs) probeDriver(pciAddr string) error {
	if s.Probe != nil {
		return s.Probe(pciAddr)
	}
	return os.WriteFile(filepath.Join(s.Root, "bus", "pci", "drivers_probe"), []byte(pciAddr), 0o644)
}

// isVFIODriverLoaded checks that the vfio-pci driver is available in sysfs.
func (s Sysfs) isVFIODriverLoaded() (bool, error) {
	driverDir := filepath.Join(s.Root, "bus", "pci", "drivers", DriverVFIOPCI)
	_, err := os.Stat(driverDir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check vfio-pci driver: %w", err)
	}
	return true, nil
}
