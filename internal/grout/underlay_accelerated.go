// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func setupAcceleratedUnderlay(ctx context.Context, client *Client, perouterNetNS netns.NsHandle, iface hostnetwork.UnderlayInterface) error {
	devState, err := devicestate.Load(iface.InterfaceName)
	if errors.Is(err, devicestate.ErrDeviceStateNotFound) {
		devState, err = initializeAcceleratedDeviceState(iface.InterfaceName)
		if err != nil {
			return err
		}
	}

	if err != nil {
		return fmt.Errorf("failed to load device state for %s: %w", iface.InterfaceName, err)
	}

	if err := prepareAcceleratedDriver(ctx, perouterNetNS, devState.PCIAddress, devState.InterfaceName); err != nil {
		return fmt.Errorf("failed to prepare grout port driver for %s: %w", devState.PCIAddress, err)
	}
	return netnamespace.In(perouterNetNS, func() error {
		return configureAcceleratedPort(ctx, client, iface, devState)
	})
}

// prepareAcceleratedDriver inspects the driver bound to a PCI device and
// takes the appropriate action:
//   - Intel kernel drivers (igb, iavf, ice, i40e): rebind to vfio-pci
//   - vfio-pci: already bound, nothing to do
//   - mlx5_core: move the kernel netlink interface to the perouter namespace (bifurcated driver)
//   - unknown/unbound: bind to vfio-pci
func prepareAcceleratedDriver(ctx context.Context, perouterNetNS netns.NsHandle, pciAddr, netlinkName string) error {
	driver, err := pci.GetPCIDriver(pciAddr)
	if err != nil {
		return fmt.Errorf("failed to get PCI driver for %s: %w", pciAddr, err)
	}

	switch {
	case pci.IntelKernelDrivers[driver]:
		if err := pci.BindVFIOPCI(pciAddr); err != nil {
			return fmt.Errorf("failed to rebind PCI device %s from %s to vfio-pci: %w",
				pciAddr, driver, err)
		}
		return nil

	case driver == pci.DriverVFIOPCI:
		return nil

	case driver == pci.DriverMlx5Core:
		name := netlinkName
		if name == "" {
			name, err = pci.GetPCINetDevice(pciAddr)
			if err != nil {
				return fmt.Errorf("mlx5 PCI device %s has no kernel netlink interface: %w", pciAddr, err)
			}
		}
		if err := hostnetwork.SetupUnderlayNetDevInterface(ctx, perouterNetNS, hostnetwork.UnderlayInterface{
			InterfaceName: name,
			Kind:          hostnetwork.UnderlayInterfaceNetDev,
		}); err != nil {
			return fmt.Errorf("failed to move mlx5 netlink device %s to namespace: %w", name, err)
		}
		return nil

	default:
		slog.Info("binding GroutPort PCI device to vfio-pci",
			"pciAddress", pciAddr, "currentDriver", driver)
		if err := pci.BindVFIOPCI(pciAddr); err != nil {
			return fmt.Errorf("failed to bind PCI device %s to vfio-pci: %w", pciAddr, err)
		}
		return nil
	}
}

// configureAcceleratedPort creates a DPDK port in grout directly from a PCI
// device address, loads the scraped IP addresses from the saved device
// state, and sets up the kernel routes needed by FRR.
func configureAcceleratedPort(ctx context.Context, client *Client, iface hostnetwork.UnderlayInterface, state *devicestate.Entry) error {
	portName := PortName(iface)

	opts := PortOptions{
		RXQueues:    iface.AcceleratedConfig.RXQueues,
		QSize:       iface.AcceleratedConfig.QSize,
		Promiscuous: iface.AcceleratedConfig.Promiscuous,
		Description: UnderlayInterfaceDescriptionMarker,
		MTU:         &state.MTU,
	}

	if err := client.ensurePortWithOptions(ctx, portName, state.PCIAddress, opts); err != nil {
		return fmt.Errorf("failed to create grout DPDK port %s: %w", portName, err)
	}

	for _, addr := range state.Addresses {
		if err := client.ensureAddress(ctx, portName, addr); err != nil {
			return fmt.Errorf("failed to assign address %s to grout port %s: %w", addr, portName, err)
		}

		if err := ensureKernelSubnetRoute(defaultVRFName, addr); err != nil {
			return fmt.Errorf("failed to add kernel route for underlay subnet %s: %w", addr, err)
		}

		slog.InfoContext(ctx, "configured grout DPDK port address", "cidr", addr, "port", portName)
	}

	if err := sysctl.Ensure(sysctl.DisableRPFilter(portName)); err != nil {
		return fmt.Errorf("failed to disable rp_filter on %s: %w", portName, err)
	}

	return nil
}

func initializeAcceleratedDeviceState(netlinkName string) (*devicestate.Entry, error) {
	devState := devicestate.Entry{
		InterfaceName: netlinkName,
	}
	var err error
	devState.PCIAddress, err = pci.ResolveNetlinkName(netlinkName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve PCI address for %s: %w", netlinkName, err)
	}
	devState.OriginalDriver, err = pci.GetPCIDriver(devState.PCIAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to read driver for %s: %w", devState.PCIAddress, err)
	}

	link, err := netlink.LinkByName(netlinkName)
	if err != nil {
		return nil, fmt.Errorf("failed to find kernel interface %s: %w", netlinkName, err)
	}
	devState.MTU = int32(link.Attrs().MTU)

	netlinkAddrs, err := hostnetwork.AddressesForInterface(netlinkName, hostnetwork.ExcludeLinkLocal())
	if err != nil {
		return nil, fmt.Errorf("failed to read addresses from %s: %w", netlinkName, err)
	}
	for _, a := range netlinkAddrs {
		devState.Addresses = append(devState.Addresses, a.IPNet.String())
	}
	if err := devicestate.Save(netlinkName, devState); err != nil {
		return nil, fmt.Errorf("failed to save device state for %s: %w", netlinkName, err)
	}
	return &devState, nil
}

func teardownAcceleratedUnderlay(ctx context.Context, client *Client, ns netns.NsHandle, targetNS string, iface hostnetwork.UnderlayInterface) error {
	portName := PortName(iface)
	if err := removeGroutPortAddresses(ctx, client, ns, portName); err != nil {
		return err
	}

	if err := client.deletePort(ctx, portName); err != nil {
		slog.ErrorContext(ctx, "failed to delete grout port", "port", portName, "error", err)
	}

	netlinkName := iface.InterfaceName
	state, err := devicestate.Load(netlinkName)
	if err != nil {
		slog.WarnContext(ctx, "no saved device state, cannot restore driver/IPs",
			"interfaceName", netlinkName, "error", err)
		return nil
	}

	if err := restoreDeviceDriver(ctx, targetNS, netlinkName, state); err != nil {
		return err
	}

	if err := restoreNetlinkInterface(ctx, *state); err != nil {
		return err
	}

	if err := devicestate.Delete(netlinkName); err != nil {
		return fmt.Errorf("failed to delete device state file for %s: %w", netlinkName, err)
	}

	return nil
}

func restoreDeviceDriver(ctx context.Context, targetNS string, netlinkName string, state *devicestate.Entry) error {
	if pci.IsBifurcated(state.OriginalDriver) {
		return restoreBifurcatedDevice(ctx, targetNS, netlinkName)
	}

	if state.PCIAddress != "" && state.OriginalDriver != "" && state.OriginalDriver != pci.DriverVFIOPCI {
		return restorePCIDriver(ctx, state)
	}

	return nil
}

func restoreBifurcatedDevice(ctx context.Context, targetNS string, netlinkName string) error {
	if err := hostnetwork.RestoreUnderlayNetDevInterface(ctx, targetNS, netlinkName); err != nil {
		return fmt.Errorf("failed to move bifurcated netdev %s back to the host namespace: %w",
			netlinkName, err)
	}
	return nil
}

func restorePCIDriver(ctx context.Context, state *devicestate.Entry) error {
	if err := pci.RestoreDriver(state.PCIAddress, state.OriginalDriver); err != nil {
		return fmt.Errorf("failed to restore driver %s on %s: %w",
			state.OriginalDriver, state.PCIAddress, err)
	}
	slog.InfoContext(ctx, "restored original driver",
		"pciAddress", state.PCIAddress, "driver", state.OriginalDriver)
	return nil
}
