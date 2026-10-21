// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package grout

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	accelTestNS     = "acceltest"
	accelTestNSPath = "/var/run/netns/" + accelTestNS
	accelIface      = "accel0"
	accelPort       = UnderlayPortNamePrefix + accelIface
	accelCIDR       = "192.168.50.2/24"
)

var accelUnderlay = hostnetwork.UnderlayInterface{
	InterfaceName: accelIface,
	Kind:          hostnetwork.UnderlayInterfaceNetDev,
}

func TestSetupAcceleratedUnderlayRebindsKernelDriver(t *testing.T) {
	ns := newAccelTestNS(t)
	addGroutMainInterface(t, ns)
	addHostDummy(t, accelCIDR)
	store := devicestate.Store{Dir: t.TempDir()}

	sysfs := newFakeSysfs(t, testPCIAddr, "igb")
	sysfs.addDriver(pci.DriverVFIOPCI)
	sysfs.addNetDevice(accelIface)

	calls := recordCmdExec(t, accelPortCalls()...)
	require.NoError(t, setupAcceleratedUnderlayFromSavedState(ns, store, sysfs.Sysfs))

	t.Run("saves the original device state", func(t *testing.T) {
		state, err := store.Load(accelIface)
		require.NoError(t, err)
		assert.Equal(t, devicestate.State{
			InterfaceName:  accelIface,
			PCIAddress:     testPCIAddr,
			OriginalDriver: "igb",
			Addresses:      []string{accelCIDR},
			AddrGenMode:    hostSysctl(t, sysctl.AddrGenMode("default", "")),
			AcceptRA:       hostSysctl(t, sysctl.AcceptRA("default", "")),
			MTU:            1500,
		}, state)
	})

	t.Run("rebinds the device to vfio-pci", func(t *testing.T) {
		assert.Equal(t, pci.DriverVFIOPCI, sysfs.read(sysfs.devicePath("driver_override")))
		assert.Equal(t, testPCIAddr, sysfs.read(sysfs.driverPath("igb", "unbind")))
		assert.Equal(t, testPCIAddr, sysfs.read(filepath.Join(sysfs.Root, "bus", "pci", "drivers_probe")))
	})

	t.Run("creates the grout PCI port with the scraped addresses", func(t *testing.T) {
		assert.Contains(t, *calls, accelPortAddCmd().cmd)
		assert.Contains(t, *calls, accelAddressAddCmd().cmd)
	})

	t.Run("adds the kernel subnet route towards grout", func(t *testing.T) {
		assert.True(t, hasSubnetRoute(t, ns, "192.168.50.0/24"))
	})

	t.Run("reconciles from the saved state once the netdev is gone", func(t *testing.T) {
		// Once bound to vfio-pci the kernel netdev disappears: the setup must
		// rely on the saved state and keep the existing port.
		sysfs.bind(pci.DriverVFIOPCI)
		deleteHostDummy(t)
		calls := recordCmdExec(t,
			okCmd(
				"grcli --err-exit --json --socket sock interface show name "+accelPort,
				fmt.Sprintf(`{"name":%q,"type":"port","devargs":%q,"mtu":1500}`, accelPort, testPCIAddr),
			),
			accelAddressAddCmd(),
			accelPortUpCmd(),
		)

		require.NoError(t, setupAcceleratedUnderlayFromSavedState(ns, store, sysfs.Sysfs))
		assert.NotContains(t, *calls, accelPortAddCmd())
	})
}

func TestSetupAcceleratedUnderlayMovesBifurcatedNetdev(t *testing.T) {
	ns := newAccelTestNS(t)
	addGroutMainInterface(t, ns)
	addHostDummy(t, accelCIDR)
	store := devicestate.Store{Dir: t.TempDir()}

	sysfs := newFakeSysfs(t, testPCIAddr, pci.DriverMlx5Core)
	sysfs.addDriver(pci.DriverVFIOPCI)
	sysfs.addNetDevice(accelIface)

	calls := recordCmdExec(t, accelPortCalls()...)
	require.NoError(t, setupAcceleratedUnderlayFromSavedState(ns, store, sysfs.Sysfs))

	assert.Empty(t, sysfs.read(sysfs.devicePath("driver_override")), "mlx5 must not be rebound to vfio-pci")

	_, err := netlink.LinkByName(accelIface)
	assert.ErrorAs(t, err, &netlink.LinkNotFoundError{}, "netdev must leave the host namespace")
	nsLink := nsLinkByName(t, ns, accelIface)
	assert.Equal(t, uint32(hostnetwork.UnderlayGroupID), nsLink.Attrs().Group)

	state, err := store.Load(accelIface)
	require.NoError(t, err)
	assert.Equal(t, pci.DriverMlx5Core, state.OriginalDriver)

	t.Run("migrates the addresses from the netdev to the grout port", func(t *testing.T) {
		assert.Contains(t, *calls, accelAddressAddCmd().cmd)
		assert.Empty(t, nsLinkAddresses(t, ns, nsLink))
		assert.True(t, hasSubnetRoute(t, ns, "192.168.50.0/24"))
	})

	t.Run("disables accept_ra on the netdev", func(t *testing.T) {
		assert.Equal(t, "0", nsSysctl(t, ns, "net/ipv6/conf/"+accelIface+"/accept_ra"))
	})

	t.Run("reconciles once the netdev is already in the router namespace", func(t *testing.T) {
		calls := recordCmdExec(t,
			okCmd(
				"grcli --err-exit --json --socket sock interface show name "+accelPort,
				fmt.Sprintf(`{"name":%q,"type":"port","devargs":%q,"mtu":1500}`, accelPort, testPCIAddr),
			),
			accelAddressAddCmd(),
			accelPortUpCmd(),
		)

		require.NoError(t, setupAcceleratedUnderlayFromSavedState(ns, store, sysfs.Sysfs))
		assert.NotContains(t, *calls, accelPortAddCmd())

		_, err := netlink.LinkByName(accelIface)
		assert.ErrorAs(t, err, &netlink.LinkNotFoundError{}, "netdev must stay out of the host namespace")
		assert.Equal(t, uint32(hostnetwork.UnderlayGroupID), nsLinkByName(t, ns, accelIface).Attrs().Group)
		assert.Empty(t, sysfs.read(sysfs.devicePath("driver_override")), "mlx5 must not be rebound to vfio-pci")
	})
}

func TestRestoreAcceleratedUnderlayRestoresKernelDriver(t *testing.T) {
	newAccelTestNS(t)
	store := devicestate.Store{Dir: t.TempDir()}

	sysfs := newFakeSysfs(t, testPCIAddr, pci.DriverVFIOPCI)
	sysfs.addDriver("igb")
	// The netdev the kernel creates once igb probes the device again.
	sysfs.addNetDevice(accelIface)
	addHostDummy(t, "")

	require.NoError(t, store.Save(devicestate.State{
		InterfaceName:  accelIface,
		PCIAddress:     testPCIAddr,
		OriginalDriver: "igb",
		Addresses:      []string{accelCIDR},
	}))

	// The port is already gone, as after a teardown interrupted right after
	// deleting it: the driver and addresses must still be restored.
	recordCmdExec(t, cmdCall{
		cmd:    "grcli --err-exit --json --socket sock interface show name " + accelPort,
		output: interfaceNotFoundOutput,
		err:    fmt.Errorf("exit status 1"),
	})
	require.NoError(t, RestoreUnderlay(context.Background(), NewClient("sock"), accelTestNSPath,
		[]hostnetwork.UnderlayInterface{accelUnderlay}, store, sysfs.Sysfs))

	assert.Equal(t, testPCIAddr, sysfs.read(sysfs.driverPath(pci.DriverVFIOPCI, "unbind")))
	assert.Equal(t, testPCIAddr, sysfs.read(sysfs.driverPath("igb", "bind")))
	assert.Equal(t, "\x00", sysfs.read(sysfs.devicePath("driver_override")))

	link, err := netlink.LinkByName(accelIface)
	require.NoError(t, err)
	assert.Contains(t, linkAddresses(t, link), accelCIDR)

	_, err = store.Load(accelIface)
	assert.ErrorIs(t, err, devicestate.ErrNotFound)
}

func TestRestoreAcceleratedDeviceMovesBifurcatedNetdevBack(t *testing.T) {
	ns := newAccelTestNS(t)
	sysfs := newFakeSysfs(t, testPCIAddr, pci.DriverMlx5Core)

	link := addHostDummy(t, "")
	require.NoError(t, netlink.LinkSetNsFd(link, int(ns)))

	state := devicestate.State{
		InterfaceName:  accelIface,
		PCIAddress:     testPCIAddr,
		OriginalDriver: pci.DriverMlx5Core,
		Addresses:      []string{accelCIDR},
		AddrGenMode:    "1",
		AcceptRA:       "2",
		MTU:            9000,
	}
	require.NoError(t, restoreAcceleratedDevice(context.Background(), accelTestNSPath, state, sysfs.Sysfs))

	link, err := netlink.LinkByName(accelIface)
	require.NoError(t, err)
	assert.Contains(t, linkAddresses(t, link), accelCIDR)
	assert.Equal(t, "1", hostSysctl(t, sysctl.AddrGenMode(accelIface, "")))
	assert.Equal(t, "2", hostSysctl(t, sysctl.AcceptRA(accelIface, "")))
	assert.Equal(t, 9000, link.Attrs().MTU)
}

func TestRetrieveDeviceStateRecordsIPv6SettingsAndMTU(t *testing.T) {
	link := addHostDummy(t, accelCIDR)
	require.NoError(t, sysctl.Ensure(sysctl.AddrGenMode(accelIface, "1"), sysctl.AcceptRA(accelIface, "2")))
	require.NoError(t, netlink.LinkSetMTU(link, 9000))

	state, err := retrieveDeviceState(accelIface, PortModeTAP, pci.Sysfs{})
	require.NoError(t, err)

	assert.Equal(t, devicestate.State{
		InterfaceName: accelIface,
		Addresses:     []string{accelCIDR},
		AddrGenMode:   "1",
		AcceptRA:      "2",
		MTU:           9000,
	}, state)
}

// setupAcceleratedUnderlayFromSavedState sets up the test underlay as
// SetupUnderlay does: from its saved device state, retrieved on first use.
func setupAcceleratedUnderlayFromSavedState(ns netns.NsHandle, store devicestate.Store, sysfs pci.Sysfs) error {
	state, err := ensureDeviceState(accelIface, PortModeAccelerated, store, sysfs)
	if err != nil {
		return err
	}
	return setupAcceleratedUnderlay(context.Background(), NewClient("sock"), ns, underlayPortOptions(accelUnderlay), state, sysfs)
}

// addNetDevice exposes a kernel netdev under the PCI device, as the kernel
// does when a netdev driver is bound.
func (s *fakeSysfs) addNetDevice(name string) {
	s.t.Helper()
	s.mkdir(s.devicePath("net", name))
	s.addNetClassDevice(name, "../../devices/pci0000:00/0000:00:02.0/"+s.pciAddr)
}

func accelPortCalls() []cmdCall {
	return []cmdCall{
		interfaceShowNotFoundCmd(),
		accelPortAddCmd(),
		accelAddressAddCmd(),
		accelPortUpCmd(),
	}
}

func accelPortAddCmd() cmdCall {
	return okCmdEmptyOutput(
		fmt.Sprintf("grcli --err-exit --json --socket sock interface add port %s devargs %s mtu 1500 down",
			accelPort, testPCIAddr),
	)
}

func accelAddressAddCmd() cmdCall {
	return okCmdEmptyOutput(fmt.Sprintf("grcli --err-exit --json --socket sock address add %s iface %s", accelCIDR, accelPort))
}

func accelPortUpCmd() cmdCall {
	return okCmdEmptyOutput("grcli --err-exit --json --socket sock interface set port " + accelPort + " up")
}

func interfaceShowNotFoundCmd() cmdCall {
	return errorCmd(
		"grcli --err-exit --json --socket sock interface show name "+accelPort,
		interfaceNotFoundOutput,
		fmt.Errorf("exit status 1"),
	)
}

func errorCmd(cmd string, output string, err error) cmdCall {
	return cmdCall{
		cmd:    cmd,
		output: output,
		err:    err,
	}
}

func okCmd(cmd string, output string) cmdCall {
	return cmdCall{
		cmd:    cmd,
		output: output,
		err:    nil,
	}
}

func okCmdEmptyOutput(cmd string) cmdCall {
	return cmdCall{
		cmd:    cmd,
		output: "",
		err:    nil,
	}
}

func newAccelTestNS(t *testing.T) netns.NsHandle {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	current, err := netns.Get()
	require.NoError(t, err)
	defer current.Close()

	ns, err := netns.NewNamed(accelTestNS)
	require.NoError(t, err)
	require.NoError(t, netns.Set(current))

	t.Cleanup(func() {
		ns.Close()
		if err := netns.DeleteNamed(accelTestNS); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("failed to delete netns %s: %v", accelTestNS, err)
		}
	})
	return ns
}

// addGroutMainInterface stands in for the TAP grout creates in the router
// namespace: the kernel subnet routes towards grout go through it. The
// underlay address is local to it so the route preferred source is valid.
func addGroutMainInterface(t *testing.T, ns netns.NsHandle) {
	t.Helper()
	handle, err := netlink.NewHandleAt(ns)
	require.NoError(t, err)
	defer handle.Close()

	main := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: defaultVRFName}}
	require.NoError(t, handle.LinkAdd(main))
	require.NoError(t, handle.LinkSetUp(main))
	addr, err := netlink.ParseAddr(strings.Split(accelCIDR, "/")[0] + "/32")
	require.NoError(t, err)
	require.NoError(t, handle.AddrAdd(main, addr))
}

func addHostDummy(t *testing.T, cidr string) netlink.Link {
	t.Helper()
	link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: accelIface}}
	require.NoError(t, netlink.LinkAdd(link))
	t.Cleanup(func() { deleteHostDummy(t) })
	require.NoError(t, netlink.LinkSetUp(link))
	if cidr != "" {
		addr, err := netlink.ParseAddr(cidr)
		require.NoError(t, err)
		require.NoError(t, netlink.AddrAdd(link, addr))
	}
	return link
}

func deleteHostDummy(t *testing.T) {
	t.Helper()
	link, err := netlink.LinkByName(accelIface)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return
	}
	require.NoError(t, err)
	require.NoError(t, netlink.LinkDel(link))
}

func nsLinkByName(t *testing.T, ns netns.NsHandle, name string) netlink.Link {
	t.Helper()
	handle, err := netlink.NewHandleAt(ns)
	require.NoError(t, err)
	defer handle.Close()
	link, err := handle.LinkByName(name)
	require.NoError(t, err)
	return link
}

func hasSubnetRoute(t *testing.T, ns netns.NsHandle, subnet string) bool {
	t.Helper()
	_, dst, err := net.ParseCIDR(subnet)
	require.NoError(t, err)
	main := nsLinkByName(t, ns, defaultVRFName)

	handle, err := netlink.NewHandleAt(ns)
	require.NoError(t, err)
	defer handle.Close()
	routes, err := handle.RouteListFiltered(netlink.FAMILY_V4,
		&netlink.Route{Dst: dst, LinkIndex: main.Attrs().Index}, netlink.RT_FILTER_DST|netlink.RT_FILTER_OIF)
	require.NoError(t, err)
	return len(routes) > 0
}

func linkAddresses(t *testing.T, link netlink.Link) []string {
	t.Helper()
	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	require.NoError(t, err)
	ret := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ret = append(ret, a.IPNet.String())
	}
	return ret
}

func nsLinkAddresses(t *testing.T, ns netns.NsHandle, link netlink.Link) []string {
	t.Helper()
	handle, err := netlink.NewHandleAt(ns)
	require.NoError(t, err)
	defer handle.Close()
	addrs, err := handle.AddrList(link, netlink.FAMILY_ALL)
	require.NoError(t, err)
	ret := make([]string, 0, len(addrs))
	for _, a := range addrs {
		ret = append(ret, a.IPNet.String())
	}
	return ret
}

func hostSysctl(t *testing.T, s sysctl.Sysctl) string {
	t.Helper()
	value, err := sysctl.Read(s)
	require.NoError(t, err)
	return value
}

func nsSysctl(t *testing.T, ns netns.NsHandle, path string) string {
	t.Helper()
	var value string
	require.NoError(t, netnamespace.In(ns, func() error {
		raw, err := os.ReadFile(filepath.Join("/proc/sys", path))
		value = strings.TrimSpace(string(raw))
		return err
	}))
	return value
}
