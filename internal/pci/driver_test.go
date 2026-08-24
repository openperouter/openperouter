// SPDX-License-Identifier:Apache-2.0

package pci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetPCIDriver_Bound(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(SysfsRoot, "bus", "pci", "drivers", "iavf")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	drv, err := GetPCIDriver(pciAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if drv != "iavf" {
		t.Fatalf("expected iavf, got %s", drv)
	}
}

func TestGetPCIDriver_Unbound(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr)
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	drv, err := GetPCIDriver(pciAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if drv != "" {
		t.Fatalf("expected empty string for unbound device, got %s", drv)
	}
}

func TestGetPCINetDevice(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	netDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr, "net", "enp3s0f0v0")
	if err := os.MkdirAll(netDir, 0o755); err != nil {
		t.Fatal(err)
	}

	name, err := GetPCINetDevice(pciAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "enp3s0f0v0" {
		t.Fatalf("expected enp3s0f0v0, got %s", name)
	}
}

func TestGetPCINetDevice_NoNetDir(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr)
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := GetPCINetDevice(pciAddr); err == nil {
		t.Fatal("expected error for missing net directory")
	}
}

func TestEnsureVFIODriver_Missing(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	if err := EnsureVFIODriver(); err == nil {
		t.Fatal("expected error when vfio-pci is not loaded")
	}
}

func TestEnsureVFIODriver_Present(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	if err := os.MkdirAll(filepath.Join(SysfsRoot, "bus", "pci", "drivers", DriverVFIOPCI), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureVFIODriver(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBindVFIOPCI_AlreadyBound(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(SysfsRoot, "bus", "pci", "drivers", "vfio-pci")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	if err := BindVFIOPCI(pciAddr); err != nil {
		t.Fatalf("expected no-op for already bound device, got %v", err)
	}
}

func TestBindVFIOPCI_Rebind(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(SysfsRoot, "bus", "pci", "drivers", "iavf")
	vfioDir := filepath.Join(SysfsRoot, "bus", "pci", "drivers", "vfio-pci")
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
	probePath := filepath.Join(SysfsRoot, "bus", "pci", "drivers_probe")
	if err := os.WriteFile(probePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := BindVFIOPCI(pciAddr); err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr)
	driverDir := filepath.Join(SysfsRoot, "bus", "pci", "drivers", "iavf")
	if err := os.MkdirAll(driverDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deviceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(driverDir, filepath.Join(deviceDir, "driver")); err != nil {
		t.Fatal(err)
	}

	if err := RestoreDriver(pciAddr, "iavf"); err != nil {
		t.Fatalf("expected no-op, got %v", err)
	}
}

func TestRestoreDriver_Rebind(t *testing.T) {
	origRoot := SysfsRoot
	t.Cleanup(func() { SysfsRoot = origRoot })
	SysfsRoot = t.TempDir()

	pciAddr := testPCIAddress
	deviceDir := filepath.Join(SysfsRoot, "bus", "pci", "devices", pciAddr)
	vfioDir := filepath.Join(SysfsRoot, "bus", "pci", "drivers", "vfio-pci")
	origDir := filepath.Join(SysfsRoot, "bus", "pci", "drivers", "iavf")
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

	if err := RestoreDriver(pciAddr, "iavf"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	override, err := os.ReadFile(filepath.Join(deviceDir, "driver_override"))
	if err != nil {
		t.Fatalf("failed to read driver_override: %v", err)
	}
	if string(override) != "" {
		t.Fatalf("expected empty driver_override, got %q", string(override))
	}
	bindContent, err := os.ReadFile(filepath.Join(origDir, "bind"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bindContent) != pciAddr {
		t.Fatalf("expected bind=%s, got %s", pciAddr, string(bindContent))
	}
}
