// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

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

	if err := validateNoStaleUnderlays(ns, params.UnderlayInterfaces); err != nil {
		return err
	}

	for _, underlayInterface := range params.UnderlayInterfaces {
		if err := hostnetwork.MoveInterfaceToNamespace(ctx, underlayInterface, ns); err != nil {
			return err
		}
		if err := netnamespace.In(ns, func() error {
			return configureUnderlayInterface(ctx, client, ns, underlayInterface)
		}); err != nil {
			return err
		}
	}

	return nil
}

func validateNoStaleUnderlays(ns netns.NsHandle, wanted []string) error {
	existingIfaces, err := hostnetwork.FindInterfacesInGroup(ns, hostnetwork.UnderlayGroupID)
	if err != nil {
		return fmt.Errorf("failed to check existing underlay interfaces: %w", err)
	}
	for _, name := range existingIfaces {
		if !slices.Contains(wanted, name) {
			return hostnetwork.UnderlayExistsError(fmt.Sprintf(
				"existing underlay found: %s, new inteUnderlayGroupIDrfaces are %v", name, wanted))
		}
	}
	return nil
}

func configureUnderlayInterface(ctx context.Context, client *Client, ns netns.NsHandle, underlayInterface string) error {
	underlay, err := netlink.LinkByName(underlayInterface)
	if err != nil {
		return fmt.Errorf("failed to get underlay nic by name %s: %w", underlayInterface, err)
	}
	if underlay.Attrs().Group != hostnetwork.UnderlayGroupID {
		if err := netlink.LinkSetGroup(underlay, int(hostnetwork.UnderlayGroupID)); err != nil {
			return fmt.Errorf("failed to set group ID on underlay interface %s: %w", underlayInterface, err)
		}
	}
	if err := netlink.LinkSetUp(underlay); err != nil {
		return fmt.Errorf("could not set link up for VRF %s: %v", underlay.Attrs().Name, err)
	}

	underlayAddrs, err := readUnderlayAddresses(underlayInterface)
	if err != nil {
		return fmt.Errorf("failed to read underlay interface addresses: %w", err)
	}

	devargs := fmt.Sprintf("net_tap0,remote=%s,iface=%s", underlayInterface, "tap_"+underlayInterface)
	if err := client.ensurePort(ctx, underlayPortName, devargs); err != nil {
		return fmt.Errorf("failed to create grout underlay port: %w", err)
	}

	if err := migrateAddressesToGrout(ctx, client, underlayInterface, underlayAddrs); err != nil {
		return err
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

func migrateAddressesToGrout(ctx context.Context, client *Client, underlayInterface string, addrs []netlink.Addr) error {
	for _, addr := range addrs {
		cidr := addr.IPNet.String()

		if err := client.ensureAddress(ctx, underlayPortName, cidr); err != nil {
			return fmt.Errorf("failed to assign address %s to grout underlay port: %w", cidr, err)
		}

		if err := deleteAddressFromKernelInterface(underlayInterface, addr); err != nil {
			return fmt.Errorf("failed to remove address %s from underlay interface: %w", cidr, err)
		}

		slog.InfoContext(ctx, "migrated underlay address to grout", "cidr", cidr, "iface", underlayPortName)
	}
	return nil
}

// RemoveUnderlay tears down the grout underlay port and resets the kernel
// group IDs on moved NICs so HasUnderlayInterface returns false on the next
// reconcile.
func RemoveUnderlay(ctx context.Context, client *Client, targetNS string) error {
	if err := client.deletePort(ctx, underlayPortName); err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to delete grout port %s: %w", underlayPortName, err)
	}

	if err := hostnetwork.RemoveUnderlay(targetNS); err != nil {
		return fmt.Errorf("RemoveUnderlay: failed to clean kernel underlay state: %w", err)
	}

	return nil
}

func readUnderlayAddresses(ifaceName string) ([]netlink.Addr, error) {
	var addrs []netlink.Addr

	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to find underlay interface %s: %w", ifaceName, err)
	}
	all, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to list addresses on %s: %w", ifaceName, err)
	}
	for _, addr := range all {
		if addr.IP.IsLinkLocalMulticast() {
			continue
		}
		if addr.IP.IsLinkLocalUnicast() {
			continue
		}
		addrs = append(addrs, addr)
	}

	return addrs, nil
}
