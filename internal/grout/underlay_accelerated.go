// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	netdevPollInterval = 100 * time.Millisecond
	netdevPollTimeout  = 10 * time.Second
	pciBus             = "pci"
)

var errNetdevNotFound = errors.New("kernel netdev not found")

func setupAcceleratedUnderlay(
	ctx context.Context,
	client *Client,
	perouterNetNS netns.NsHandle,
	opts portOptions,
	state devicestate.State,
	sysfs pci.Sysfs,
) error {
	// A state file left by the TAP path has no PCI address
	if state.PCIAddress == "" {
		return fmt.Errorf("expecting a PCI address in the device state for %s, none found", state.InterfaceName)
	}

	if err := setupInterfaceForHWAcceleration(ctx, perouterNetNS, state.PCIAddress, state.InterfaceName, sysfs); err != nil {
		return fmt.Errorf("failed to prepare grout port driver for %s: %w", state.PCIAddress, err)
	}
	return netnamespace.In(perouterNetNS, func() error {
		return configureUnderlayAcceleratedPort(ctx, client, opts, state)
	})
}

// setupInterfaceForHWAcceleration inspects the driver bound to a PCI device and
// takes the appropriate action:
//   - vfio-pci: already bound, nothing to do
//   - bifurcated (e.g. mlx5_core): move the kernel netdev to the perouter namespace
//   - any other driver, or none: bind to vfio-pci
func setupInterfaceForHWAcceleration(
	ctx context.Context,
	perouterNetNS netns.NsHandle,
	pciAddr, netdev string,
	sysfs pci.Sysfs,
) error {
	driver, err := sysfs.DriverForPCIAddress(pciAddr)
	if err != nil {
		return fmt.Errorf("failed to get PCI driver for %s: %w", pciAddr, err)
	}

	switch {
	case driver == pci.DriverVFIOPCI:
		return nil

	case pci.IsBifurcated(driver):
		if err := moveBifurcatedNetdev(ctx, perouterNetNS, pciAddr, netdev); err != nil {
			return fmt.Errorf("failed to move bifurcated netlink device %s to namespace: %w", netdev, err)
		}
		return nil

	default:
		slog.InfoContext(ctx, "binding GroutPort PCI device to vfio-pci",
			"pciAddress", pciAddr, "currentDriver", driver)
		if err := sysfs.BindVFIOPCI(pciAddr); err != nil {
			return fmt.Errorf("failed to bind PCI device %s to vfio-pci: %w", pciAddr, err)
		}
		return nil
	}
}

// configureUnderlayAcceleratedPort creates a DPDK port in grout directly from a PCI
// device address, loads the scraped IP addresses from the saved device
// state, and sets up the kernel routes needed by FRR.
func configureUnderlayAcceleratedPort(
	ctx context.Context,
	client *Client,
	opts portOptions,
	state devicestate.State,
) error {
	portName := UnderlayPortName(state.InterfaceName)

	opts.Devargs = state.PCIAddress
	opts.MTU = state.MTU
	if err := client.ensurePort(ctx, portName, opts); err != nil {
		return fmt.Errorf("failed to create grout DPDK port %s: %w", portName, err)
	}

	if pci.IsBifurcated(state.OriginalDriver) {
		if err := quiesceBifurcatedNetdev(state.PCIAddress); err != nil {
			return err
		}
	}

	if err := assignAddressesToGroutPort(ctx, client, portName, state.Addresses); err != nil {
		return err
	}

	if err := client.setPortUp(ctx, portName); err != nil {
		return fmt.Errorf("failed to set grout DPDK port %s up: %w", portName, err)
	}

	return nil
}

// quiesceBifurcatedNetdev stops the kernel netdev of a bifurcated device from
// holding addresses of its own. The netdev lives on in the router namespace
// next to the DPDK port, so, as on the TAP path, it must give up its
// link-local and stop autoconfiguring new addresses from Router
// Advertisements: otherwise they clash with grout's connected routes (EBUSY)
// and with the identical link-local of the port sharing its MAC.
func quiesceBifurcatedNetdev(pciAddr string) error {
	handle, err := netlink.NewHandle()
	if err != nil {
		return fmt.Errorf("failed to get netlink handle: %w", err)
	}
	defer handle.Close()

	link, err := findNetdevByPCI(handle, pciAddr)
	if err != nil {
		return fmt.Errorf("failed to find bifurcated netdev: %w", err)
	}
	name := link.Attrs().Name
	if err := hostnetwork.SuppressLinkLocal(name); err != nil {
		return fmt.Errorf("failed to suppress link-local on bifurcated netdev %s: %w", name, err)
	}
	if err := hostnetwork.SuppressAutoconf(name); err != nil {
		return fmt.Errorf("failed to suppress autoconf on bifurcated netdev %s: %w", name, err)
	}
	return nil
}

// restoreAcceleratedDevice hands a PCI device back to its original kernel
// driver and re-applies the saved addresses to its netdev.
func restoreAcceleratedDevice(
	ctx context.Context,
	targetNS string,
	state devicestate.State,
	sysfs pci.Sysfs,
) error {
	switch {
	case state.OriginalDriver == "" || state.OriginalDriver == pci.DriverVFIOPCI:
		return nil
	case pci.IsBifurcated(state.OriginalDriver):
		if err := restoreBifurcatedNetdevName(targetNS, state); err != nil {
			return err
		}
		return restoreNetdev(ctx, targetNS, state)
	}

	if err := sysfs.RestoreDriver(state.PCIAddress, state.OriginalDriver); err != nil {
		return fmt.Errorf("failed to restore driver %s on %s: %w", state.OriginalDriver, state.PCIAddress, err)
	}
	slog.InfoContext(ctx, "restored original driver", "pciAddress", state.PCIAddress, "driver", state.OriginalDriver)

	link, err := waitForPCINetdev(ctx, state.PCIAddress, sysfs)
	if err != nil {
		return err
	}
	return applyStateToLink(ctx, link, state)
}

// restoreBifurcatedNetdevName gives the netdev of a bifurcated device, in the
// namespace at nsPath, its saved name back, so that it reaches the host under
// that name. grout renames the netdev of a device it holds a port on and
// renames it back only when the same grout process deletes the port.
func restoreBifurcatedNetdevName(nsPath string, state devicestate.State) error {
	ns, err := netns.GetFromPath(nsPath)
	if err != nil {
		return fmt.Errorf("failed to find network namespace %s: %w", nsPath, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", nsPath, "error", err)
		}
	}()
	handle, err := netlink.NewHandleAt(ns)
	if err != nil {
		return fmt.Errorf("failed to get netlink handle for namespace %s: %w", nsPath, err)
	}
	defer handle.Close()

	link, err := findNetdevByPCI(handle, state.PCIAddress)
	if errors.Is(err, errNetdevNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	currentName := link.Attrs().Name
	if currentName == state.InterfaceName {
		return nil
	}
	// The kernel refuses to rename a netdev that is up; moving it to the host
	// namespace sets it up again.
	if err := handle.LinkSetDown(link); err != nil {
		return fmt.Errorf("failed to set netdev %s down: %w", currentName, err)
	}
	if err := handle.LinkSetName(link, state.InterfaceName); err != nil {
		return fmt.Errorf("failed to rename netdev %s to %s: %w", currentName, state.InterfaceName, err)
	}
	return nil
}

// waitForPCINetdev returns the netdev of a PCI device just rebound to its
// kernel driver. The netdev appears asynchronously and udev may rename it, so
// it is looked up by PCI address until it shows up.
func waitForPCINetdev(ctx context.Context, pciAddr string, sysfs pci.Sysfs) (netlink.Link, error) {
	var link netlink.Link
	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, netdevPollInterval, netdevPollTimeout, true,
		func(context.Context) (bool, error) {
			name, err := sysfs.NetDeviceForPCIAddress(pciAddr)
			if err != nil {
				lastErr = err
				return false, nil
			}
			link, err = netlink.LinkByName(name)
			if err != nil {
				lastErr = fmt.Errorf("failed to find netdev %s: %w", name, err)
				return false, nil
			}
			return true, nil
		})
	if err != nil && lastErr != nil {
		return nil, fmt.Errorf("kernel netdev for PCI device %s did not appear: %w: %w", pciAddr, err, lastErr)
	}
	if err != nil {
		return nil, fmt.Errorf("kernel netdev for PCI device %s did not appear: %w", pciAddr, err)
	}
	return link, nil
}

// moveBifurcatedNetdev moves the netdev of a bifurcated device to the router
// namespace, unless it is there already.
func moveBifurcatedNetdev(ctx context.Context, perouterNetNS netns.NsHandle, pciAddr, netdev string) error {
	handle, err := netlink.NewHandleAt(perouterNetNS)
	if err != nil {
		return fmt.Errorf("failed to get netlink handle for the router namespace: %w", err)
	}
	defer handle.Close()

	_, err = findNetdevByPCI(handle, pciAddr)
	if errors.Is(err, errNetdevNotFound) {
		return hostnetwork.SetupUnderlayNetDevInterface(ctx, perouterNetNS, hostnetwork.UnderlayInterface{
			InterfaceName: netdev,
			Kind:          hostnetwork.UnderlayInterfaceNetDev,
		})
	}
	return err
}

// findNetdevByPCI returns the netdev of the PCI device at pciAddr in the
// namespace of handle, whatever its name: grout renames the netdev of a device
// it holds a port on. It returns errNetdevNotFound when there is none.
func findNetdevByPCI(handle *netlink.Handle, pciAddr string) (netlink.Link, error) {
	links, err := handle.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list netdevs: %w", err)
	}
	for _, link := range links {
		if isNetdevOfPCIDevice(link, pciAddr) {
			return link, nil
		}
	}
	return nil, fmt.Errorf("%w: PCI device %s", errNetdevNotFound, pciAddr)
}

// isNetdevOfPCIDevice is a variable so that tests, which have no PCI device,
// can make a dummy link stand in for one.
var isNetdevOfPCIDevice = func(link netlink.Link, pciAddr string) bool {
	return link.Attrs().ParentDevBus == pciBus && link.Attrs().ParentDev == pciAddr
}
