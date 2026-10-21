// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"syscall"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const UnderlayPortNamePrefix = "u_"

// UnderlayPortName returns the name of the grout port attached to the given
// underlay interface.
func UnderlayPortName(underlayInterface string) string {
	return UnderlayPortNamePrefix + underlayInterface
}

// UnderlayPortMode selects how grout attaches to an underlay device.
type UnderlayPortMode string

const (
	PortModeAccelerated UnderlayPortMode = "accelerated"
	PortModeTAP         UnderlayPortMode = "tap"
)

// SetupUnderlay configures the underlay interfaces via the grout dataplane.
// PortModeTAP selects TAP+remote= ports; otherwise kernel netdevs are bound
// directly as PCI ports. Grout owns the underlay addresses and forwarding.
func SetupUnderlay(
	ctx context.Context,
	client *Client,
	params hostnetwork.UnderlayParams,
	mode UnderlayPortMode,
	store devicestate.Store,
	sysfs pci.Sysfs,
) error {
	slog.DebugContext(ctx, "setup underlay", "params", params)
	defer slog.DebugContext(ctx, "setup underlay done")

	perouterNetNS, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("setupUnderlay: Failed to find network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := perouterNetNS.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	// If any existing underlay interfaces were removed from the new list,
	// clean them up before setting up the new ones, tearing down their
	// grout state first.
	existing, err := UnderlayInterfaces(store)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	if toRemove := hostnetwork.UnderlayInterfacesToRemove(existing, params.UnderlayInterfaces); len(toRemove) > 0 {
		slog.InfoContext(ctx, "underlay interfaces changed, removing old interfaces before setup",
			"toRemove", toRemove, "requested", params.UnderlayInterfaces)
		if err := RestoreUnderlay(ctx, client, params.TargetNS, toRemove, store, sysfs); err != nil {
			return fmt.Errorf("failed to remove old underlay interfaces: %w", err)
		}
	}

	for _, iface := range params.UnderlayInterfaces {
		if iface.Kind != hostnetwork.UnderlayInterfaceNetDev {
			return fmt.Errorf("underlay interface has unsupported kind %q", iface.Kind)
		}

		// device state is the source of truth for the underlay interface, it is created as the first step
		// of the setup process with the information contained in the UnderlayInterface struct and the state
		// of the netlink interface.
		// In the teardown process, it is removed as the last step.
		state, err := ensureDeviceState(iface.InterfaceName, mode, store, sysfs)
		if err != nil {
			return err
		}

		opts := underlayPortOptions(iface)
		if mode == PortModeTAP {
			err = setupTapUnderlay(ctx, client, perouterNetNS, opts, state)
		} else {
			err = setupAcceleratedUnderlay(ctx, client, perouterNetNS, opts, state, sysfs)
		}
		if err != nil {
			return err
		}
	}

	if params.TunnelEndpoint != nil {
		if err := setupTunnelEndpoint(ctx, client, *params.TunnelEndpoint); err != nil {
			return err
		}
	}

	return nil
}

// UnderlayInterfaces returns the netdev underlays currently tracked as underlays, read
// from the saved device states. An underlay is tracked from the moment its
// state is saved, before grout or the namespace are touched, until its
// teardown completes, so an interrupted setup or teardown is resumed on the
// next reconcile.
func UnderlayInterfaces(store devicestate.Store) ([]hostnetwork.UnderlayInterface, error) {
	states, err := store.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list saved device states: %w", err)
	}
	ret := make([]hostnetwork.UnderlayInterface, 0, len(states))
	for _, state := range states {
		ret = append(ret, hostnetwork.UnderlayInterface{
			InterfaceName: state.InterfaceName,
			Kind:          hostnetwork.UnderlayInterfaceNetDev,
		})
	}
	return ret, nil
}

// RestoreUnderlay removes the given underlay interfaces: it tears down their
// grout ports first (migrating the underlay addresses back to the kernel
// interfaces), then hands the devices back to the kernel as described by
// their saved device state. Every step is idempotent and the device state is
// deleted last, so a failed teardown is resumed by the next call.
func RestoreUnderlay(
	ctx context.Context,
	client *Client,
	targetNS string,
	toRemove []hostnetwork.UnderlayInterface,
	store devicestate.Store,
	sysfs pci.Sysfs,
) error {
	if len(toRemove) == 0 {
		return nil
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to find network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	for _, iface := range toRemove {
		if iface.Kind != hostnetwork.UnderlayInterfaceNetDev {
			return fmt.Errorf("underlay interface has unsupported kind %q", iface.Kind)
		}

		// An underlay without saved state is still torn down, but its driver
		// and addresses cannot be restored.
		state, err := store.Load(iface.InterfaceName)
		if errors.Is(err, devicestate.ErrNotFound) {
			slog.WarnContext(ctx, "no saved device state, cannot restore driver/IPs", "interfaceName", iface.InterfaceName)
			state, err = devicestate.State{InterfaceName: iface.InterfaceName}, nil
		}
		if err != nil {
			return fmt.Errorf("RestoreUnderlay: failed to load device state for %s: %w", iface.InterfaceName, err)
		}

		if err := teardownUnderlayInterface(ctx, client, ns, targetNS, state, sysfs); err != nil {
			return err
		}

		if err := store.Delete(iface.InterfaceName); err != nil {
			return fmt.Errorf("RestoreUnderlay: failed to delete device state for %s: %w", iface.InterfaceName, err)
		}
	}

	return nil
}

// ensureDeviceState returns the saved state of the given underlay, retrieving
// it from the host device and saving it when missing. The state is retrieved
// once, before the device is touched, and never updated afterwards. The saved
// addresses are then removed from the host device, as from here on the state
// is their only owner.
func ensureDeviceState(
	interfaceName string,
	mode UnderlayPortMode,
	store devicestate.Store,
	sysfs pci.Sysfs,
) (devicestate.State, error) {
	state, err := store.Load(interfaceName)
	if errors.Is(err, devicestate.ErrNotFound) {
		state, err = retrieveAndSaveDeviceState(interfaceName, mode, store, sysfs)
		if err != nil {
			return devicestate.State{}, err
		}
	}
	if err != nil {
		return devicestate.State{}, fmt.Errorf("failed to load device state for %s: %w", interfaceName, err)
	}
	if err := removeSavedAddressesFromKernel(state); err != nil {
		return devicestate.State{}, err
	}
	return state, nil
}

func retrieveAndSaveDeviceState(
	interfaceName string,
	mode UnderlayPortMode,
	store devicestate.Store,
	sysfs pci.Sysfs,
) (devicestate.State, error) {
	state, err := retrieveDeviceState(interfaceName, mode, sysfs)
	if err != nil {
		return devicestate.State{}, err
	}
	if err := store.Save(state); err != nil {
		return devicestate.State{}, fmt.Errorf("failed to save device state for %s: %w", interfaceName, err)
	}
	return state, nil
}

func setupTapUnderlay(
	ctx context.Context,
	client *Client,
	perouterNetNS netns.NsHandle,
	opts portOptions,
	state devicestate.State,
) error {
	if err := hostnetwork.SetupUnderlayNetDevInterface(
		ctx,
		perouterNetNS,
		hostnetwork.UnderlayInterface{
			InterfaceName: state.InterfaceName,
			Kind:          hostnetwork.UnderlayInterfaceNetDev,
		}); err != nil {
		return err
	}
	return netnamespace.In(perouterNetNS, func() error {
		return configureUnderlayTapPort(ctx, client, opts, state)
	})
}

func setupTunnelEndpoint(ctx context.Context, client *Client, ep hostnetwork.UnderlayTunnelEndpointParams) error {
	var addrs []string
	for _, addr := range []string{ep.IPv4CIDR, ep.IPv6CIDR} {
		if addr != "" {
			addrs = append(addrs, addr)
		}
	}
	if len(addrs) == 0 {
		return fmt.Errorf("at least one tunnel endpoint IP must be provided (IPv4 or IPv6)")
	}

	if err := assignAddressesToGroutPort(ctx, client, defaultVRFName, addrs); err != nil {
		return fmt.Errorf("failed to assign tunnel endpoint IPs to grout underlay: %w", err)
	}
	return nil
}

func teardownUnderlayInterface(
	ctx context.Context,
	client *Client,
	ns netns.NsHandle,
	targetNS string,
	state devicestate.State,
	sysfs pci.Sysfs,
) error {
	// The port must be gone before the device is handed back to the kernel:
	// DPDK may still own it.
	if err := deleteUnderlayPort(ctx, client, ns, UnderlayPortName(state.InterfaceName)); err != nil {
		return err
	}
	if state.PCIAddress != "" {
		return restoreAcceleratedDevice(ctx, targetNS, state, sysfs)
	}
	return restoreNetdev(ctx, targetNS, state)
}

func deleteUnderlayPort(ctx context.Context, client *Client, ns netns.NsHandle, portName string) error {
	exists, err := client.portExists(ctx, portName)
	if err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to inspect grout port %s: %w", portName, err)
	}
	if !exists {
		return nil
	}
	if err := removeAddressFromGroutPort(ctx, client, ns, portName); err != nil {
		return err
	}
	if err := client.deletePort(ctx, portName); err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to delete grout port %s: %w", portName, err)
	}
	return nil
}

// restoreNetdev moves a netdev that grout shares with the kernel (the one
// backing a TAP port, or a bifurcated device) back to the host namespace and
// re-applies its saved addresses.
func restoreNetdev(ctx context.Context, targetNS string, state devicestate.State) error {
	if err := hostnetwork.RestoreUnderlayNetDevInterface(ctx, targetNS, state.InterfaceName); err != nil {
		return fmt.Errorf("failed to move netdev %s back to the host namespace: %w", state.InterfaceName, err)
	}

	link, err := netlink.LinkByName(state.InterfaceName)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		slog.WarnContext(ctx, "kernel netdev is gone, nothing to restore", "interfaceName", state.InterfaceName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find kernel netdev %s: %w", state.InterfaceName, err)
	}

	return applyStateToLink(ctx, link, state)
}

func applyStateToLink(ctx context.Context, link netlink.Link, state devicestate.State) error {
	name := link.Attrs().Name
	if state.MTU > 0 {
		if err := netlink.LinkSetMTU(link, state.MTU); err != nil {
			return fmt.Errorf("failed to restore MTU %d on kernel netdev [%s]: %w", state.MTU, name, err)
		}
	}
	if err := restoreIPv6Settings(name, state); err != nil {
		return err
	}
	for _, addr := range state.Addresses {
		if err := hostnetwork.AssignIPToInterface(link, addr); err != nil {
			return fmt.Errorf("failed to restore IP address %s to kernel netdev [%s]: %w", addr, name, err)
		}
		slog.InfoContext(ctx, "restored IP addresses to kernel netdev",
			"interfaceName", name, "addresses", addr)
	}
	return nil
}

func configureUnderlayTapPort(
	ctx context.Context,
	client *Client,
	opts portOptions,
	state devicestate.State,
) error {
	underlayInterface := state.InterfaceName
	portName := UnderlayPortName(underlayInterface)

	// Suppress the netdev's kernel IPv6 link-local so it doesn't collide with
	// the identical EUI-64 link-local on the grout port's kernel interface,
	// which shares the netdev's MAC. The collision makes DAD strip the port's
	// link-local, breaking any session with an IPv6 nexthop. The netdev's
	// link-local is unused since grout owns all forwarding.
	slog.InfoContext(ctx, "suppressing kernel link-local on underlay interface", "iface", underlayInterface)
	if err := hostnetwork.SuppressLinkLocal(underlayInterface); err != nil {
		return fmt.Errorf("failed to suppress link-local on underlay interface %s: %w", underlayInterface, err)
	}

	opts.Devargs = fmt.Sprintf("net_tap%s,remote=%s,iface=%s",
		makeTapRandomString(), underlayInterface, "tap_"+underlayInterface)
	opts.MTU = state.MTU
	if err := client.ensurePort(ctx, portName, opts); err != nil {
		return fmt.Errorf("failed to create grout underlay port: %w", err)
	}

	// Disable SLAAC on the underlying netlink interface: once the intended
	// addresses are migrated to the grout port, the kernel must not
	// autoconfigure new ones from Router Advertisements. A SLAAC address
	// on the netlink device would be picked up on the next reconcile and
	// clash with the existing connected route in grout's RIB (EBUSY).
	if err := sysctl.Ensure(sysctl.DisableAcceptRA(underlayInterface)); err != nil {
		return fmt.Errorf("failed to disable accept_ra on underlay interface %s: %w", underlayInterface, err)
	}

	if err := assignAddressesToGroutPort(ctx, client, portName, state.Addresses); err != nil {
		return err
	}

	if err := client.setPortUp(ctx, portName); err != nil {
		return fmt.Errorf("failed to set grout port up: %w", err)
	}

	return nil
}

func underlayPortOptions(iface hostnetwork.UnderlayInterface) portOptions {
	if iface.AcceleratedConfig == nil {
		return portOptions{}
	}
	return portOptions{RXQueues: iface.AcceleratedConfig.RXQueues, QSize: iface.AcceleratedConfig.QSize}
}

func retrieveDeviceState(
	interfaceName string,
	mode UnderlayPortMode,
	sysfs pci.Sysfs,
) (devicestate.State, error) {
	state := devicestate.State{InterfaceName: interfaceName}

	if mode == PortModeAccelerated {
		pciAddr, driver, err := retrievePCIState(interfaceName, sysfs)
		if err != nil {
			return devicestate.State{}, err
		}
		state.PCIAddress = pciAddr
		state.OriginalDriver = driver
	}

	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return devicestate.State{}, fmt.Errorf("failed to find interface %s: %w", interfaceName, err)
	}
	state.MTU = link.Attrs().MTU

	netlinkAddrs, err := hostnetwork.AddressesForInterface(interfaceName, hostnetwork.ExcludeLinkLocal())
	if err != nil {
		return devicestate.State{}, fmt.Errorf("failed to read addresses from %s: %w", interfaceName, err)
	}
	for _, a := range netlinkAddrs {
		state.Addresses = append(state.Addresses, a.IPNet.String())
	}

	state.AddrGenMode, err = sysctl.Read(sysctl.AddrGenMode(interfaceName, ""))
	if err != nil {
		return devicestate.State{}, fmt.Errorf("failed to read addr_gen_mode of %s: %w", interfaceName, err)
	}
	state.AcceptRA, err = sysctl.Read(sysctl.AcceptRA(interfaceName, ""))
	if err != nil {
		return devicestate.State{}, fmt.Errorf("failed to read accept_ra of %s: %w", interfaceName, err)
	}
	return state, nil
}

// restoreIPv6Settings re-applies the IPv6 sysctls the setup overrides to keep
// the kernel from managing addresses on the netdev.
func restoreIPv6Settings(netdev string, state devicestate.State) error {
	var sysctls []sysctl.Sysctl
	if state.AddrGenMode != "" {
		sysctls = append(sysctls, sysctl.AddrGenMode(netdev, state.AddrGenMode))
	}
	if state.AcceptRA != "" {
		sysctls = append(sysctls, sysctl.AcceptRA(netdev, state.AcceptRA))
	}
	if err := sysctl.Ensure(sysctls...); err != nil {
		return fmt.Errorf("failed to restore IPv6 settings on kernel netdev %s: %w", netdev, err)
	}
	return nil
}

// retrievePCIState returns the PCI address of netdev and the driver it is
// currently bound to.
func retrievePCIState(netdev string, sysfs pci.Sysfs) (string, string, error) {
	pciAddr, err := sysfs.PCIAddressForNetDevice(netdev)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve PCI address for %s: %w", netdev, err)
	}
	driver, err := sysfs.DriverForPCIAddress(pciAddr)
	if err != nil {
		return "", "", fmt.Errorf("failed to read driver for %s: %w", pciAddr, err)
	}
	return pciAddr, driver, nil
}

// removeSavedAddressesFromKernel removes the addresses recorded in the device
// state from the kernel netdev, so they do not follow it into the router
// namespace and clash with the grout port. It runs on every reconcile, as a
// failure right after the state was saved would otherwise leave them behind.
// A netdev no longer in the host namespace (moved, or unbound from its kernel
// driver) has nothing left to remove.
func removeSavedAddressesFromKernel(state devicestate.State) error {
	link, err := netlink.LinkByName(state.InterfaceName)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to find kernel interface %s: %w", state.InterfaceName, err)
	}
	for _, cidr := range state.Addresses {
		addr, err := netlink.ParseAddr(cidr)
		if err != nil {
			return fmt.Errorf("failed to parse saved address %s: %w", cidr, err)
		}
		err = netlink.AddrDel(link, addr)
		if err != nil && !errors.Is(err, syscall.EADDRNOTAVAIL) {
			return fmt.Errorf("failed to remove address %s from kernel interface %s: %w", cidr, state.InterfaceName, err)
		}
	}
	return nil
}

func assignAddressesToGroutPort(ctx context.Context, client *Client, portName string, addrs []string) error {
	for _, cidr := range addrs {
		if err := client.ensureAddress(ctx, portName, cidr); err != nil {
			return fmt.Errorf("failed to assign address %s to grout underlay port %s: %w", cidr, portName, err)
		}

		// FRR needs kernel routes to establish BGP connections. Grout requires that all the kernel
		// traffic must enter grout via the `main` TAP device.
		if err := ensureKernelSubnetRoute(defaultVRFName, cidr); err != nil {
			return fmt.Errorf("failed to add kernel route for underlay subnet %s: %w", cidr, err)
		}

		slog.InfoContext(ctx, "assigned underlay address to grout port", "cidr", cidr, "port", portName)
	}

	return nil
}

func removeAddressFromGroutPort(ctx context.Context, client *Client, ns netns.NsHandle, portName string) error {
	return netnamespace.In(ns, func() error {
		addrs, err := client.getAddresses(ctx, portName)
		if err != nil {
			return fmt.Errorf("RestoreUnderlay: failed to get addresses for grout port %s: %w", portName, err)
		}
		for _, addr := range addrs {
			if err := removeKernelSubnetRoute(defaultVRFName, addr); err != nil {
				return fmt.Errorf("RestoreUnderlay: failed to remove kernel route for %s: %w", addr, err)
			}
			if err := client.deleteAddress(ctx, portName, addr); err != nil {
				return fmt.Errorf("RestoreUnderlay: failed to delete address %s from grout port %s: %w", addr, portName, err)
			}
		}
		return nil
	})
}

func ensureKernelSubnetRoute(ifaceName, addr string) error {
	route, err := connectedRouteForAddress(ifaceName, addr)
	if err != nil {
		return err
	}
	if route == nil {
		return nil
	}

	existing, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, route, netlink.RT_FILTER_DST|netlink.RT_FILTER_OIF)
	if err != nil {
		return fmt.Errorf("failed to list routes for %s dev %s: %w", route.Dst, ifaceName, err)
	}
	if len(existing) > 0 {
		return nil
	}

	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("failed to add route for %s dev %s: %w", route.Dst, ifaceName, err)
	}

	slog.Info("added kernel route for subnet", "cidr", addr, "src", route.Src, "ipnet", route.Dst, "iface", ifaceName)
	return nil
}

func removeKernelSubnetRoute(ifaceName, addr string) error {
	route, err := connectedRouteForAddress(ifaceName, addr)
	if err != nil {
		return err
	}
	if route == nil {
		return nil
	}

	if err := netlink.RouteDel(route); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("failed to delete route for %s dev %s: %w", route.Dst, ifaceName, err)
	}

	slog.Info("removed kernel route for subnet", "cidr", addr, "src", route.Src, "ipnet", route.Dst, "iface", ifaceName)
	return nil
}

func connectedRouteForAddress(ifaceName, addr string) (*netlink.Route, error) {
	srcAddr, ipNet, err := net.ParseCIDR(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CIDR %s: %w", addr, err)
	}
	ones, bits := ipNet.Mask.Size()
	if ones == bits {
		return nil, nil
	}
	if srcAddr.IsLinkLocalUnicast() {
		return nil, nil
	}

	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to find interface %s: %w", ifaceName, err)
	}

	return &netlink.Route{
		Dst:       ipNet,
		LinkIndex: link.Attrs().Index,
		Src:       srcAddr,
	}, nil
}
