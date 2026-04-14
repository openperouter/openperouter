// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// assignIPsToLink assigns IPv4 and/or IPv6 addresses to a netlink interface.
func assignIPsToLink(link netlink.Link, ipv4, ipv6 string) error {
	if ipv4 == "" && ipv6 == "" {
		return fmt.Errorf("at least one IP address must be provided (IPv4 or IPv6)")
	}

	for _, addr := range []string{ipv4, ipv6} {
		if addr == "" {
			continue
		}
		parsed, err := netlink.ParseAddr(addr)
		if err != nil {
			return fmt.Errorf("failed to parse address %s: %w", addr, err)
		}
		if err := netlink.AddrReplace(link, parsed); err != nil {
			return fmt.Errorf("failed to assign address %s to %s: %w", addr, link.Attrs().Name, err)
		}
	}
	return nil
}

// assignIPsToGroutPort assigns IPv4 and IPv6 addresses to a grout port via grcli.
func assignIPsToGroutPort(ctx context.Context, client *Client, portName string, ipv4, ipv6 string) error {
	if ipv4 == "" && ipv6 == "" {
		return fmt.Errorf("at least one IP address must be provided (IPv4 or IPv6)")
	}

	for _, addr := range []string{ipv4, ipv6} {
		if addr == "" {
			continue
		}
		slog.DebugContext(ctx, "assigning IP to grout port", "port", portName, "addr", addr)
		if err := client.ensureAddress(ctx, portName, addr); err != nil {
			return fmt.Errorf("failed to assign address %s to grout port %s: %w", addr, portName, err)
		}
	}
	return nil
}

// moveLinkToHostNamespace moves a link from the given namespace to the current
// (host) namespace. If the link is already in the host namespace, this is a no-op.
func moveLinkToHostNamespace(ctx context.Context, name string, srcNS netns.NsHandle) error {
	// Check if already in the host namespace.
	_, err := netlink.LinkByName(name)
	if err == nil {
		slog.DebugContext(ctx, "link already in host namespace", "name", name)
		return nil
	}

	// Get the current (host) namespace fd.
	hostNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get host namespace: %w", err)
	}
	defer func() {
		if err := hostNS.Close(); err != nil {
			slog.Error("failed to close host namespace", "error", err)
		}
	}()

	// Move the link from srcNS to host namespace.
	if err := netnamespace.In(srcNS, func() error {
		link, err := netlink.LinkByName(name)
		if err != nil {
			return fmt.Errorf("link %s not found in source namespace: %w", name, err)
		}
		return netlink.LinkSetNsFd(link, int(hostNS))
	}); err != nil {
		return err
	}

	// Bring it up in the host namespace.
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("link %s not found after move to host: %w", name, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring %s up: %w", name, err)
	}

	slog.DebugContext(ctx, "link moved to host namespace", "name", name)
	return nil
}

func deleteAddressFromKernelInterface(ns netns.NsHandle, ifaceName string, addr netlink.Addr) error {
	if err := netnamespace.In(ns, func() error {
		link, err := netlink.LinkByName(ifaceName)
		if err != nil {
			return fmt.Errorf("failed to find underlay interface %s: %w", ifaceName, err)
		}
		return netlink.AddrDel(link, &addr)
	}); err != nil {
		return fmt.Errorf("failed to remove address %s from underlay interface: %w", addr.IPNet.String(), err)
	}
	return nil
}

// setUniqueMAC assigns a random locally-administered unicast MAC to the named
// link. TAP devices share the same MAC on both ends, which causes IPv6
// DAD failures on the link-local address and prevents NDP from working.
// Giving the host-side TAP its own MAC avoids both problems.
func setUniqueMAC(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("link %s not found: %w", name, err)
	}

	mac := make(net.HardwareAddr, 6)
	if _, err := rand.Read(mac); err != nil {
		return fmt.Errorf("failed to generate random bytes: %w", err)
	}
	mac[0] = (mac[0] | 0x02) & 0xfe // locally administered, unicast

	// Bring the link down before changing the MAC, then back up.
	// This ensures the kernel regenerates the link-local address
	// from the new MAC.
	if err := netlink.LinkSetDown(link); err != nil {
		return fmt.Errorf("failed to bring %s down: %w", name, err)
	}
	if err := netlink.LinkSetHardwareAddr(link, mac); err != nil {
		return fmt.Errorf("failed to set MAC on %s: %w", name, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring %s up: %w", name, err)
	}
	return nil
}

// ensureKernelSubnetRoute ensures a kernel route exists for the given CIDR's
// subnet via the named interface inside the given namespace. Grout assigns /32
// to its control plane kernel interfaces, so the kernel has no connected route
// for the subnet and might fall back to the default route. This route lets
// the kernel TCP stack (used by FRR bgpd) reach peers on the same subnet
// through grout.
func ensureKernelSubnetRoute(ns netns.NsHandle, ifaceName, addr string) error {
	srcAddr, ipNet, err := net.ParseCIDR(addr)
	if err != nil {
		return fmt.Errorf("failed to parse CIDR %s: %w", addr, err)
	}
	ones, bits := ipNet.Mask.Size()
	if ones == bits {
		return nil
	}

	if srcAddr.IsLinkLocalUnicast() {
		return nil
	}

	return netnamespace.In(ns, func() error {
		link, err := netlink.LinkByName(ifaceName)
		if err != nil {
			return fmt.Errorf("failed to find interface %s: %w", ifaceName, err)
		}

		existing, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, &netlink.Route{
			Dst:       ipNet,
			LinkIndex: link.Attrs().Index,
			Src:       srcAddr,
		}, netlink.RT_FILTER_DST|netlink.RT_FILTER_OIF)
		if err != nil {
			return fmt.Errorf("failed to list routes for %s dev %s: %w", ipNet, ifaceName, err)
		}
		if len(existing) > 0 {
			return nil
		}

		if err := netlink.RouteAdd(&netlink.Route{
			Dst:       ipNet,
			LinkIndex: link.Attrs().Index,
			Src:       srcAddr,
		}); err != nil {
			return fmt.Errorf("failed to add route for %s dev %s: %w", ipNet, ifaceName, err)
		}

		slog.Info("added kernel route for subnet", "cidr", addr, "src", srcAddr, "ipnet", ipNet, "iface", ifaceName)
		return nil
	})
}

func removeLinkByName(name string) error {
	link, err := netlink.LinkByName(name)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove link by name: failed to get link %s: %w", name, err)
	}
	return netlink.LinkDel(link)
}
