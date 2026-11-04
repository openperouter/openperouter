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
		if err := quiesceBifurcatedNetdev(state.InterfaceName, state.PCIAddress); err != nil {
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
func quiesceBifurcatedNetdev(netdev, pciAddr string) error {
	handle, err := netlink.NewHandle()
	if err != nil {
		return fmt.Errorf("failed to get netlink handle: %w", err)
	}
	defer handle.Close()

	link, err := findNetdev(handle, netdev, pciAddr)
	if err != nil {
		return fmt.Errorf("failed to find bifurcated netdev %s: %w", netdev, err)
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

	_, err = findNetdev(handle, netdev, pciAddr)
	if errors.Is(err, errNetdevNotFound) {
		return hostnetwork.SetupUnderlayNetDevInterface(ctx, perouterNetNS, hostnetwork.UnderlayInterface{
			InterfaceName: netdev,
			Kind:          hostnetwork.UnderlayInterfaceNetDev,
		})
	}
	return err
}

// findNetdev returns the kernel netdev of a device in the namespace of handle.
// While grout holds a port on a bifurcated device it renames the device's
// netdev, and it gives the original name back only when the port is deleted
// by the same grout process. A netdev not found by its saved name is then
// looked up by the PCI device it belongs to, which no rename changes.
func findNetdev(handle *netlink.Handle, name, pciAddr string) (netlink.Link, error) {
	link, err := handle.LinkByName(name)
	if err == nil {
		return link, nil
	}
	if !errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil, fmt.Errorf("failed to find netdev %s: %w", name, err)
	}
	if pciAddr == "" {
		return nil, fmt.Errorf("%w: %s", errNetdevNotFound, name)
	}

	links, err := handle.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list netdevs: %w", err)
	}
	for _, link := range links {
		if isNetdevOfPCIDevice(link, pciAddr) {
			return link, nil
		}
	}
	return nil, fmt.Errorf("%w: %s, PCI device %s", errNetdevNotFound, name, pciAddr)
}

func isNetdevOfPCIDevice(link netlink.Link, pciAddr string) bool {
	return link.Attrs().ParentDevBus == pciBus && link.Attrs().ParentDev == pciAddr
}
