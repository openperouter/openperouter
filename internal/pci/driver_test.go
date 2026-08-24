// SPDX-License-Identifier:Apache-2.0

package pci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDriverForPCIAddress_Bound(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(sysfs.Root, "bus", "pci", "drivers", "iavf")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	drv, err := sysfs.DriverForPCIAddress(pciAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if drv != "iavf" {
		t.Fatalf("expected iavf, got %s", drv)
	}
}

func TestDriverForPCIAddress_Unbound(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr)
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	drv, err := sysfs.DriverForPCIAddress(pciAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if drv != "" {
		t.Fatalf("expected empty string for unbound device, got %s", drv)
	}
}

func TestNetDeviceForPCIAddress(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	netDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr, "net", "enp3s0f0v0")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	name, err := sysfs.NetDeviceForPCIAddress(pciAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "enp3s0f0v0" {
		t.Fatalf("expected enp3s0f0v0, got %s", name)
	}
}

func TestNetDeviceForPCIAddress_NoNetDir(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr)
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := sysfs.NetDeviceForPCIAddress(pciAddr); err == nil {
		t.Fatal("expected error for missing net directory")
	}
}

func TestIsVFIODriverLoaded_Missing(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	loaded, err := sysfs.isVFIODriverLoaded()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded {
		t.Fatal("expected vfio-pci driver to be not loaded")
	}
}
func TestBindVFIOPCI_AlreadyBound(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(sysfs.Root, "bus", "pci", "drivers", "vfio-pci")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	if err := sysfs.BindVFIOPCI(pciAddr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The fake sysfs does not rebind the device on drivers_probe, so this covers
// the case where the kernel accepts the probe but vfio-pci does not bind.
func TestBindVFIOPCI_ProbeDoesNotBind(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(sysfs.Root, "bus", "pci", "drivers", "iavf")
	vfioDir := filepath.Join(sysfs.Root, "bus", "pci", "drivers", "vfio-pci")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(vfioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	unbindPath := filepath.Join(driverDir, "unbind")
	if err := os.WriteFile(unbindPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	probePath := filepath.Join(sysfs.Root, "bus", "pci", "drivers_probe")
	if err := os.WriteFile(probePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err := sysfs.BindVFIOPCI(pciAddr)
	if err == nil {
		t.Fatal("expected error when the device is not bound to vfio-pci after probe")
	}
	if !strings.Contains(err.Error(), `bound to "iavf"`) {
		t.Fatalf("expected error to report the current driver, got %v", err)
	}

	override, err := os.ReadFile(filepath.Join(deviceDir, "driver_override"))
	if err != nil {
		t.Fatalf("failed to read driver_override: %v", err)
	}
	if string(override) != DriverVFIOPCI {
		t.Fatalf("expected driver_override=%s, got %s", DriverVFIOPCI, string(override))
	}

	unbindContent, err := os.ReadFile(unbindPath)
	if err != nil {
		t.Fatalf("failed to read unbind: %v", err)
	}
	if string(unbindContent) != pciAddr {
		t.Fatalf("expected unbind=%s, got %s", pciAddr, string(unbindContent))
	}

	probeContent, err := os.ReadFile(probePath)
	if err != nil {
		t.Fatalf("failed to read drivers_probe: %v", err)
	}
	if string(probeContent) != pciAddr {
		t.Fatalf("expected drivers_probe=%s, got %s", pciAddr, string(probeContent))
	}
}

func TestRestoreDriver_AlreadyOriginal(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(sysfs.Root, "bus", "pci", "drivers", "iavf")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	if err := sysfs.RestoreDriver(pciAddr, "iavf"); err != nil {
		t.Fatalf("expected no-op, got %v", err)
	}
}

func TestRestoreDriver_Rebind(t *testing.T) {
	t.Parallel()
	sysfs := Sysfs{Root: t.TempDir()}

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(sysfs.Root, "bus", "pci", "devices", pciAddr)
	vfioDir := filepath.Join(sysfs.Root, "bus", "pci", "drivers", "vfio-pci")
	origDir := filepath.Join(sysfs.Root, "bus", "pci", "drivers", "iavf")
	if err := os.MkdirAll(vfioDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(origDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vfioDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vfioDir, "unbind"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(origDir, "bind"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := sysfs.RestoreDriver(pciAddr, "iavf"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	override, err := os.ReadFile(filepath.Join(deviceDir, "driver_override"))
	if err != nil {
		t.Fatalf("failed to read driver_override: %v", err)
	}
	if string(override) != "\x00" {
		t.Fatalf("expected a newline to clear the sysfs driver_override, got %q", string(override))
	}
	bindContent, err := os.ReadFile(filepath.Join(origDir, "bind"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bindContent) != pciAddr {
		t.Fatalf("expected bind=%s, got %s", pciAddr, string(bindContent))
	}
}
