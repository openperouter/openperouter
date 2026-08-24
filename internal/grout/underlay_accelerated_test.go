// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netns"
)

const testPCIAddr = "0000:03:00.0"

func TestSetupInterfaceForHWAcceleration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		driver          string
		vfioLoaded      bool
		expectOverride  string
		expectUnbind    bool
		expectProbe     bool
		expectErrSubstr string
	}{
		{
			name:       "already bound to vfio-pci is a no-op",
			driver:     pci.DriverVFIOPCI,
			vfioLoaded: true,
		},
		{
			name:           "kernel driver is rebound to vfio-pci",
			driver:         "igb",
			vfioLoaded:     true,
			expectOverride: pci.DriverVFIOPCI,
			expectUnbind:   true,
			expectProbe:    true,
		},
		{
			name:           "unbound device is bound to vfio-pci",
			vfioLoaded:     true,
			expectOverride: pci.DriverVFIOPCI,
			expectProbe:    true,
		},
		{
			name:            "vfio-pci module not loaded",
			driver:          "igb",
			expectErrSubstr: "vfio-pci driver is not loaded",
		},
		{
			name:            "mlx5 without a kernel netdev",
			driver:          pci.DriverMlx5Core,
			vfioLoaded:      true,
			expectErrSubstr: "failed to move bifurcated netlink device nonexistent0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sysfs := newFakeSysfs(t, testPCIAddr, tc.driver)
			if tc.vfioLoaded {
				sysfs.addDriver(pci.DriverVFIOPCI)
			}

			err := setupInterfaceForHWAcceleration(context.Background(), netns.None(),
				testPCIAddr, "nonexistent0", sysfs.Sysfs)
			if tc.expectErrSubstr != "" {
				require.ErrorContains(t, err, tc.expectErrSubstr)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, tc.expectOverride, sysfs.read(sysfs.devicePath("driver_override")))
			if tc.expectUnbind {
				assert.Equal(t, testPCIAddr, sysfs.read(sysfs.driverPath(tc.driver, "unbind")))
			}
			if tc.expectProbe {
				assert.Equal(t, testPCIAddr, sysfs.read(filepath.Join(sysfs.Root, "bus", "pci", "drivers_probe")))
			}
		})
	}
}

func TestConfigureAcceleratedPort(t *testing.T) {
	t.Run("creates a PCI port with the saved addresses", func(t *testing.T) {
		calls := recordCmdExec(t,
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_ens1f0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_ens1f0 devargs " + testPCIAddr + " down",
			},
			// Host routes need no kernel connected route, so no netlink access is required.
			cmdCall{cmd: "grcli --err-exit --json --socket sock address add 192.168.11.3/32 iface u_ens1f0"},
			cmdCall{cmd: "grcli --err-exit --json --socket sock address add 2001:db8::3/128 iface u_ens1f0"},
			cmdCall{cmd: "grcli --err-exit --json --socket sock interface set port u_ens1f0 up"},
		)

		state := devicestate.State{
			InterfaceName: "ens1f0",
			PCIAddress:    testPCIAddr,
			Addresses:     []string{"192.168.11.3/32", "2001:db8::3/128"},
		}
		require.NoError(t, configureUnderlayAcceleratedPort(context.Background(), NewClient("sock"), portOptions{}, state))
		assert.Len(t, *calls, 5)
	})

	t.Run("applies the accelerated config options", func(t *testing.T) {
		calls := recordCmdExec(t,
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_ens1f0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_ens1f0 devargs " + testPCIAddr +
					" rxqs 4 qsize 1024 down",
			},
			cmdCall{cmd: "grcli --err-exit --json --socket sock interface set port u_ens1f0 up"},
		)

		opts := portOptions{RXQueues: new(int32(4)), QSize: new(int32(1024))}
		state := devicestate.State{InterfaceName: "ens1f0", PCIAddress: testPCIAddr}
		require.NoError(t, configureUnderlayAcceleratedPort(context.Background(), NewClient("sock"), opts, state))
		assert.Len(t, *calls, 3)
	})

	t.Run("existing matching port is kept", func(t *testing.T) {
		calls := recordCmdExec(t,
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name u_ens1f0",
				output: `{"name":"u_ens1f0","type":"port","devargs":"` + testPCIAddr +
					`","mtu":1500,"description":"underlay-for=ens1f0"}`,
			},
			cmdCall{cmd: "grcli --err-exit --json --socket sock interface set port u_ens1f0 up"},
		)

		state := devicestate.State{InterfaceName: "ens1f0", PCIAddress: testPCIAddr}
		require.NoError(t, configureUnderlayAcceleratedPort(context.Background(), NewClient("sock"), portOptions{}, state))
		assert.Len(t, *calls, 2)
	})
}

func TestSetupAcceleratedUnderlayRejectsTapDeviceState(t *testing.T) {
	t.Parallel()
	sysfs := pci.Sysfs{Root: t.TempDir()}
	state := devicestate.State{InterfaceName: "ens1f0", Addresses: []string{"192.168.11.3/24"}}

	err := setupAcceleratedUnderlay(context.Background(), NewClient("sock"), netns.None(), portOptions{}, state, sysfs)
	require.ErrorContains(t, err, "expecting a PCI address")
}

func TestDeviceStateForAcceleratedDeviceRejectsNonPCIDevices(t *testing.T) {
	t.Parallel()
	sysfs := newFakeSysfs(t, testPCIAddr, "igb")
	sysfs.addNetClassDevice("veth0", "../../devices/virtual/net/veth0")

	_, err := retrieveDeviceState("veth0", PortModeAccelerated, sysfs.Sysfs)
	require.ErrorContains(t, err, "does not look like a PCI address")

	_, err = retrieveDeviceState("missing0", PortModeAccelerated, sysfs.Sysfs)
	require.ErrorContains(t, err, "failed to resolve PCI address for missing0")
}

// fakeSysfs is a minimal /sys tree holding one PCI device, enough for the pci
// package to read and change the bound driver.
type fakeSysfs struct {
	pci.Sysfs
	t       *testing.T
	pciAddr string
}

func newFakeSysfs(t *testing.T, pciAddr, driver string) *fakeSysfs {
	t.Helper()
	s := &fakeSysfs{t: t, Sysfs: pci.Sysfs{Root: t.TempDir()}, pciAddr: pciAddr}
	s.mkdir(s.devicePath())
	s.write(filepath.Join(s.Root, "bus", "pci", "drivers_probe"), "")
	if driver != "" {
		s.bind(driver)
	}
	s.Probe = s.probe
	return s
}

// probe records the drivers_probe write and, like the kernel, binds the device
// to the driver named in driver_override.
func (s *fakeSysfs) probe(pciAddr string) error {
	s.write(filepath.Join(s.Root, "bus", "pci", "drivers_probe"), pciAddr)
	if override := s.read(s.devicePath("driver_override")); override != "" {
		s.bind(override)
	}
	return nil
}

// bind points the device "driver" symlink at the given driver, as the kernel
// does once a driver probes the device.
func (s *fakeSysfs) bind(driver string) {
	s.t.Helper()
	s.addDriver(driver)
	link := s.devicePath("driver")
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		s.t.Fatal(err)
	}
	if err := os.Symlink(s.driverPath(driver), link); err != nil {
		s.t.Fatal(err)
	}
}

func (s *fakeSysfs) addDriver(driver string) {
	s.t.Helper()
	s.mkdir(s.driverPath(driver))
	s.write(s.driverPath(driver, "bind"), "")
	s.write(s.driverPath(driver, "unbind"), "")
}

func (s *fakeSysfs) addNetClassDevice(name, deviceTarget string) {
	s.t.Helper()
	dir := filepath.Join(s.Root, "class", "net", name)
	s.mkdir(dir)
	if err := os.Symlink(deviceTarget, filepath.Join(dir, "device")); err != nil {
		s.t.Fatal(err)
	}
}

func (s *fakeSysfs) devicePath(elem ...string) string {
	return filepath.Join(append([]string{s.Root, "bus", "pci", "devices", s.pciAddr}, elem...)...)
}

func (s *fakeSysfs) driverPath(driver string, elem ...string) string {
	return filepath.Join(append([]string{s.Root, "bus", "pci", "drivers", driver}, elem...)...)
}

func (s *fakeSysfs) read(path string) string {
	s.t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
}

func (s *fakeSysfs) write(path, content string) {
	s.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *fakeSysfs) mkdir(path string) {
	s.t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		s.t.Fatal(err)
	}
}

// recordCmdExec mocks grcli like mockCmdExec and records the commands that
// were run, so a test can assert which grout calls happened.
func recordCmdExec(t *testing.T, cmdCalls ...cmdCall) *[]string {
	t.Helper()
	t.Cleanup(mockCmdExec(cmdCalls...))
	mocked := execCmd
	var calls []string
	execCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return mocked(ctx, name, args...)
	}
	return &calls
}
