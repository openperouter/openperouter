// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	netdevPollInterval = 100 * time.Millisecond
	netdevPollTimeout  = 10 * time.Second
)

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
		// The netdev keeps its name: grout renames it when the port is
		// created, before creating its own interface named after the port.
		if err := moveNetdevToRouterNS(ctx, perouterNetNS, netdev, netdev); err != nil {
			return fmt.Errorf("failed to move bifurcated netdev %s to namespace: %w", netdev, err)
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
	portName := state.InterfaceName

	if pci.IsBifurcated(state.OriginalDriver) {
		if err := prepareBifurcatedNetdev(ctx, client, state); err != nil {
			return err
		}
	}

	opts.Devargs = state.PCIAddress
	opts.MTU = state.MTU
	if err := client.ensurePort(ctx, portName, opts); err != nil {
		return fmt.Errorf("failed to create grout DPDK port %s: %w", portName, err)
	}

	if err := assignAddressesToGroutPort(ctx, client, portName, state.Addresses); err != nil {
		return err
	}

	if err := client.setPortUp(ctx, portName); err != nil {
		return fmt.Errorf("failed to set grout DPDK port %s up: %w", portName, err)
	}

	return nil
}

// prepareBifurcatedNetdev sets up the kernel netdev of a bifurcated device and
// stops it from holding addresses of its own. The netdev lives on in the
// router namespace next to the DPDK port, so, as on the TAP path, it must give
// up its link-local and stop autoconfiguring new addresses from Router
// Advertisements: otherwise they clash with grout's connected routes (EBUSY)
// and with the identical link-local of the port sharing its MAC.
//
// Creating the port, grout renames the netdev, which keeps these settings, and
// gives its name to its own interface: the netdev is prepared only before.
func prepareBifurcatedNetdev(ctx context.Context, client *Client, state devicestate.State) error {
	exists, err := client.portExists(ctx, state.InterfaceName)
	if err != nil {
		return fmt.Errorf("failed to inspect grout port %s: %w", state.InterfaceName, err)
	}
	if exists {
		return nil
	}

	netdev := state.InterfaceName
	if err := hostnetwork.EnsureLinkUp(netdev); err != nil {
		return err
	}
	if err := hostnetwork.SuppressLinkLocal(netdev); err != nil {
		return fmt.Errorf("failed to suppress link-local on bifurcated netdev %s: %w", netdev, err)
	}
	if err := sysctl.Ensure(sysctl.DisableAcceptRA(netdev)); err != nil {
		return fmt.Errorf("failed to disable accept_ra on bifurcated netdev %s: %w", netdev, err)
	}
	return nil
}

// restoreAcceleratedDevice hands a PCI device back to its original kernel
// driver and re-applies the saved addresses to its netdev. It must run once
// the grout port is deleted, as grout then gives a bifurcated netdev its name
// back.
func restoreAcceleratedDevice(
	ctx context.Context,
	ns netns.NsHandle,
	state devicestate.State,
	sysfs pci.Sysfs,
) error {
	switch {
	case state.OriginalDriver == "" || state.OriginalDriver == pci.DriverVFIOPCI:
		return nil
	case pci.IsBifurcated(state.OriginalDriver):
		return restoreNetdev(ctx, ns, state.InterfaceName, state)
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
