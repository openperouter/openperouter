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
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	UnderlayPortNamePrefix = "u_"
)

func UnderlayInterfaces(ctx context.Context, client *Client) (map[string]struct{}, error) {
	interfaces, err := client.listInterfaces(ctx)
	if err != nil {
		return map[string]struct{}{}, fmt.Errorf("HasUnderlayInterface: Failed to list interfaces: %w", err)
	}

	indexedIfaces := make(map[string]struct{}, len(interfaces))
	for _, iface := range interfaces {
		if strings.HasPrefix(iface.Name, UnderlayPortNamePrefix) {
			indexedIfaces[iface.Name] = struct{}{}
		}
	}
	return indexedIfaces, nil
}

// SetupUnderlay configures the underlay interface via the grout dataplane.
// It moves the kernel interface into the router namespace, creates a grout
// TAP port with remote= so TC ingress rules redirect incoming packets from
// the physical interface to grout, and assigns the underlay IPs to the grout
// port. Grout handles all L2 (ARP) and L3 forwarding; it also creates a
// NOARP kernel interface for kernel TCP (used by FRR bgpd for BGP sessions).
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

	defaultNetNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netns handle for default namespace: %w", err)
	}
	defer func() {
		if err := defaultNetNS.Close(); err != nil {
			slog.Error("failed to close default namespace", "error", err)
		}
	}()

	existingIfaces, err := hostnetwork.FindInterfacesInGroup(perouterNetNS, hostnetwork.UnderlayGroupID)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	if removedInterfaces := hostnetwork.UnderlayInterfacesToRemove(existingIfaces, params.UnderlayInterfaces); len(removedInterfaces) > 0 {
		slog.InfoContext(ctx, "underlay interfaces changed, restoring old grout state before setup",
			"removed", removedInterfaces, "requested", params.UnderlayInterfaces)
		if err := RestoreUnderlay(ctx, client, params.TargetNS, removedInterfaces); err != nil {
			return fmt.Errorf("failed to restore old underlay interfaces: %w", err)
		}
	}

	perouterNetNSHandle, err := netlink.NewHandleAt(perouterNetNS)
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netlink handle for namespace %s: %w", perouterNetNS.String(), err)
	}
	defer perouterNetNSHandle.Close()
	defaultNetNSHandle, err := netlink.NewHandleAt(defaultNetNS)
	if err != nil {
		return fmt.Errorf("SetupUnderlay: failed to get netlink handle for default namespace: %w", err)
	}
	defer defaultNetNSHandle.Close()

	for _, underlayInterface := range params.UnderlayInterfaces {
		if err := hostnetwork.MoveInterfaceToNamespace(ctx, underlayInterface, defaultNetNSHandle, perouterNetNSHandle, perouterNetNS,
			hostnetwork.UnderlayGroupID); err != nil {
			return err
		}
		if err := netnamespace.In(perouterNetNS, func() error {
			return configureUnderlayPort(ctx, client, underlayInterface)
		}); err != nil {
			return err
		}
	}

	return nil
}

// RestoreUnderlay tears down the grout underlay ports and resets the kernel
// group IDs on moved NICs so UnderlayInterfaces returns false on the next
// reconcile.
func RestoreUnderlay(ctx context.Context, client *Client, targetNS string, ifacesToRemove map[string]struct{}) error {
	interfaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("HasUnderlayInterface: Failed to list interfaces: %w", err)
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("setupUnderlay: Failed to find network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	for _, iface := range interfaces {
		if !strings.HasPrefix(iface.Name, UnderlayPortNamePrefix) {
			continue
		}

		if err := netnamespace.In(ns, func() error {
			if err := migrateAddressesToKernel(ctx, client, iface.Name); err != nil {
				return fmt.Errorf("RestoreUnderlay: failed to migrate addresses back to kernel: %w", err)
			}

			return nil
		}); err != nil {
			return err
		}

		if err := client.deletePort(ctx, iface.Name); err != nil {
			return fmt.Errorf("RestoreUnderlay: failed to delete grout port %s: %w", iface.Name, err)
		}
	}

	if err := hostnetwork.RestoreUnderlay(ctx, targetNS, ifacesToRemove); err != nil {
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
		// traffic must enter grout via the `main` TAP device.
		if err := ensureKernelSubnetRoute("main", addr.IPNet.String()); err != nil {
			return fmt.Errorf("failed to add kernel route for underlay subnet %s: %w", addr, err)
		}

		slog.InfoContext(ctx, "migrated underlay address to grout", "cidr", cidr, "iface", UnderlayPortNamePrefix+underlayInterface)
	}

	// for each port, grout creates a NOARP kernel interface to make FRR zebra daemon work.
	// 5: u_enp3s0: <BROADCAST,MULTICAST,NOARP,UP,LOWER_UP> mtu 1500 qdisc fq_codel state UP mode DEFAULT group default qlen 1000
	//    link/ether 00:09:a8:38:8e:3b brd ff:ff:ff:ff:ff:ff promiscuity 0 allmulti 0 minmtu 68 maxmtu 65521
	//    tun type tap ...
	//    alias Grout control plane interface
	// bgpd packets will leave through the `main` intrerface and will come back on the `u_xxx` interface, hence the
	// need to disable rp_filter on the `u_xxx` interface.
	if err := sysctl.Ensure(sysctl.DisableRPFilter(UnderlayPortNamePrefix + underlayInterface)); err != nil {
		return fmt.Errorf("failed to disable rp_filter on underlay interface %s: %w", UnderlayPortNamePrefix+underlayInterface, err)
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
