// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	underlayPortName = "underlay"
)

func HasUnderlayInterface(ctx context.Context, client *Client) (bool, error) {
	return client.portExists(ctx, underlayPortName)
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

	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("setupUnderlay: Failed to find network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if params.UnderlayInterface == "" {
		return nil
	}

	if err := hostnetwork.MoveUnderlayInterface(ctx, params.UnderlayInterface, ns); err != nil {
		return err
	}

	var underlayAddrs []netlink.Addr
	if err := netnamespace.In(ns, func() error {
		link, err := netlink.LinkByName(params.UnderlayInterface)
		if err != nil {
			return fmt.Errorf("failed to find underlay interface %s: %w", params.UnderlayInterface, err)
		}
		addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return fmt.Errorf("failed to list addresses on %s: %w", params.UnderlayInterface, err)
		}
		for _, addr := range addrs {
			if addr.IPNet.String() == hostnetwork.UnderlayInterfaceSpecialAddr {
				continue
			}
			if addr.IP.IsLinkLocalMulticast() {
				continue
			}
			if addr.IP.IsLinkLocalUnicast() {
				continue
			}
			underlayAddrs = append(underlayAddrs, addr)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to read underlay interface addresses: %w", err)
	}

	// Create grout TAP port with remote= so TC ingress rules redirect
	// incoming packets from the physical underlay interface to grout.
	devargs := fmt.Sprintf("net_tap0,remote=%s,iface=%s", params.UnderlayInterface, "tap_"+params.UnderlayInterface)
	if err := client.ensurePort(ctx, underlayPortName, devargs); err != nil {
		return fmt.Errorf("failed to create grout underlay port: %w", err)
	}

	// For each address: assign to grout, add kernel route, then delete from
	// the kernel interface. If step 1 or 2 fails, the address stays on the
	// kernel interface and will be picked up on the next reconcile.
	for _, addr := range underlayAddrs {
		cidr := addr.IPNet.String()

		if err := client.ensureAddress(ctx, underlayPortName, cidr); err != nil {
			return fmt.Errorf("failed to assign address %s to grout underlay port: %w", cidr, err)
		}

		if err := deleteAddressFromKernelInterface(ns, params.UnderlayInterface, addr); err != nil {
			return fmt.Errorf("failed to remove address %s from underlay interface: %w", cidr, err)
		}

		slog.InfoContext(ctx, "migrated underlay address to grout", "cidr", cidr, "iface", underlayPortName)
	}

	portAddresses, err := client.getAddresses(ctx, underlayPortName)
	if err != nil {
		return fmt.Errorf("failed to get grout underlay port addresses: %w", err)
	}
	for _, addr := range portAddresses {
		if err := ensureKernelSubnetRoute(ns, "main", addr); err != nil {
			return fmt.Errorf("failed to add kernel route for underlay subnet %s: %w", addr, err)
		}
	}

	return nil
}
