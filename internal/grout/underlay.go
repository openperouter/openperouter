// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"syscall"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	UnderlayPortNamePrefix = "u_"
)

// SetupUnderlay configures the underlay interfaces via the grout dataplane.
// Every interface is provisioned in the router namespace according to its
// kind (host network devices are moved in, CNI interfaces are added by
// invoking their plugin); then a grout TAP port with remote= is created so
// TC ingress rules redirect incoming packets from the interface to grout,
// and the underlay IPs are moved to the grout port. Grout handles all L2
// (ARP) and L3 forwarding; it also creates a NOARP kernel interface for
// kernel TCP (used by FRR bgpd for BGP sessions).
func SetupUnderlay(ctx context.Context, client *Client, params hostnetwork.UnderlayParams) error {
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
	existing, err := hostnetwork.UnderlayInterfaces(params.TargetNS)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	if toRemove := hostnetwork.UnderlayInterfacesToRemove(existing, params.UnderlayInterfaces); len(toRemove) > 0 {
		slog.InfoContext(ctx, "underlay interfaces changed, removing old interfaces before setup",
			"toRemove", toRemove, "requested", params.UnderlayInterfaces)
		if err := RestoreUnderlay(ctx, client, params.TargetNS, toRemove); err != nil {
			return fmt.Errorf("failed to remove old underlay interfaces: %w", err)
		}
	}

	for _, iface := range params.UnderlayInterfaces {
		switch iface.Kind {
		case hostnetwork.UnderlayInterfaceNetDev:
			if err := hostnetwork.SetupUnderlayNetDevInterface(ctx, perouterNetNS, iface); err != nil {
				return err
			}
		case hostnetwork.UnderlayInterfaceCNIDev:
			if err := hostnetwork.SetupUnderlayCNIDevInterface(ctx, params.TargetNS, iface); err != nil {
				return err
			}
		default:
			return fmt.Errorf("underlay interface %s has unsupported kind %q", iface.InterfaceName, iface.Kind)
		}
		if err := netnamespace.In(perouterNetNS, func() error {
			return configureUnderlayPort(ctx, client, iface.InterfaceName)
		}); err != nil {
			return err
		}
	}

	return nil
}

// RestoreUnderlay removes the given underlay interfaces: it tears
// down their grout ports first (migrating the underlay addresses back to the
// kernel interfaces), then removes the interfaces from the namespace
// according to how they were provisioned, exactly like the kernel datapath
// does.
func RestoreUnderlay(
	ctx context.Context,
	client *Client,
	targetNS string,
	toRemove []hostnetwork.UnderlayInterface,
) error {
	if len(toRemove) == 0 {
		return nil
	}

	interfaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to list grout interfaces: %w", err)
	}
	existingPorts := map[string]struct{}{}
	for _, iface := range interfaces {
		existingPorts[iface.Name] = struct{}{}
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
		portName := UnderlayPortNamePrefix + iface.InterfaceName
		if _, found := existingPorts[portName]; !found {
			slog.Debug("RestoreUnderlay: port already removed", "namespace", targetNS, "port", portName)
			continue
		}

		if err := netnamespace.In(ns, func() error {
			if err := migrateAddressesToKernel(ctx, client, portName); err != nil {
				return fmt.Errorf("RestoreUnderlay: failed to migrate addresses back to kernel: %w", err)
			}

			return nil
		}); err != nil {
			return err
		}

		if err := client.deletePort(ctx, portName); err != nil {
			return fmt.Errorf("RestoreUnderlay: failed to delete grout port %s: %w", portName, err)
		}
	}

	if err := hostnetwork.RestoreUnderlay(ctx, targetNS, toRemove); err != nil {
		return fmt.Errorf("RestoreUnderlay: failed to clean kernel underlay state: %w", err)
	}

	return nil
}

func configureUnderlayPort(ctx context.Context, client *Client, underlayInterface string) error {
	underlayAddrs, err := hostnetwork.AddressesForInterface(underlayInterface, hostnetwork.ExcludeLinkLocal())
	if err != nil {
		return fmt.Errorf("failed to read underlay interface addresses: %w", err)
	}

	devargs := fmt.Sprintf("net_tap%s,remote=%s,iface=%s", makeTapRandomString(), underlayInterface, "tap_"+underlayInterface)
	if err := client.ensurePort(ctx, UnderlayPortNamePrefix+underlayInterface, devargs); err != nil {
		return fmt.Errorf("failed to create grout underlay port: %w", err)
	}

	if err := migrateAddressesToGrout(ctx, client, underlayInterface, underlayAddrs); err != nil {
		return err
	}

	return nil
}

func migrateAddressesToGrout(ctx context.Context, client *Client, underlayInterface string, addrs []netlink.Addr) error {
	for _, addr := range addrs {
		cidr := addr.IPNet.String()

		// Move the address to the grout underlay port, so grout can register routes and nexthops
		if err := client.ensureAddress(ctx, UnderlayPortNamePrefix+underlayInterface, cidr); err != nil {
			return fmt.Errorf("failed to assign address %s to grout underlay port: %w", cidr, err)
		}

		if err := hostnetwork.DeleteAddressFromInterface(underlayInterface, addr); err != nil {
			return fmt.Errorf("failed to remove address %s from underlay interface: %w", cidr, err)
		}

		// FRR needs kernel routes to enstabilish BGP connections. Grout requires that all the kernel
		// traffic must enter/leave grout via the `main` TAP device.
		if err := ensureKernelSubnetRoute("main", addr.IPNet.String()); err != nil {
			return fmt.Errorf("failed to add kernel route for underlay subnet %s: %w", addr, err)
		}

		slog.InfoContext(ctx, "migrated underlay address to grout", "cidr", cidr, "iface", UnderlayPortNamePrefix+underlayInterface)
	}
	return nil
}

func migrateAddressesToKernel(ctx context.Context, client *Client, underlayPortName string) error {
	addrs, err := client.getAddresses(ctx, underlayPortName)
	if err != nil {
		return fmt.Errorf("failed to read addresses from grout port %s: %w", underlayPortName, err)
	}
	if len(addrs) == 0 {
		return nil
	}

	underlayLinkName := strings.Replace(underlayPortName, UnderlayPortNamePrefix, "", 1)

	link, err := netlink.LinkByName(underlayLinkName)
	if err != nil {
		return fmt.Errorf("failed to find link %s: %w", underlayLinkName, err)
	}

	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr)
		if err != nil {
			return fmt.Errorf("failed to parse address %s: %w", addr, err)
		}
		if ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
			continue
		}

		if err := hostnetwork.AssignIPToInterface(link, addr); err != nil {
			return fmt.Errorf("failed to assign address %s to link %s: %w", addr, underlayLinkName, err)
		}
	}

	for _, addr := range addrs {
		if err := removeKernelSubnetRoute("main", addr); err != nil {
			return fmt.Errorf("failed to remove kernel route for %s: %w", addr, err)
		}

		if err := client.deleteAddress(ctx, underlayPortName, addr); err != nil {
			return fmt.Errorf("failed to delete addresses on grout port %s: %w", underlayPortName, err)
		}

	}

	return nil
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
