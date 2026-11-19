// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// setupHostTap connects the VRF to the host through a TAP: grout owns the
// namespace side as a port in the VRF, and the host side is moved to the
// default namespace.
func setupHostTap(ctx context.Context, client *Client, linkPair hostnetwork.VethNames, targetNS, vrf string,
	linkIPs hostnetwork.LinkIPs, tunnelOverhead int) error {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("failed to find namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	if err := ensureTapPortInHostNamespace(ctx, client, linkPair.NamespaceSide, linkPair.HostSide, vrf, ns); err != nil {
		return err
	}

	hostTap, err := netlink.LinkByName(linkPair.HostSide)
	if err != nil {
		return fmt.Errorf("host TAP %s not found after move: %w", linkPair.HostSide, err)
	}
	if err := hostnetwork.AssignIPsToInterface(hostTap, linkIPs.HostIPv4, linkIPs.HostIPv6); err != nil {
		return fmt.Errorf("failed to assign IPs to host TAP: %w", err)
	}

	underlayMTU, err := hostnetwork.FindUnderlayMTU(ns)
	if err != nil {
		return fmt.Errorf("could not find underlay MTU: %w", err)
	}
	if err := hostnetwork.SetVethMTUForTunnelOverhead(hostTap, underlayMTU, tunnelOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on host TAP %s: %w", linkPair.HostSide, err)
	}

	if err := ensurePortAddresses(ctx, client, linkPair.NamespaceSide, linkIPs.NSIPv4, linkIPs.NSIPv6); err != nil {
		return fmt.Errorf("failed to assign IPs to grout port: %w", err)
	}
	if err := client.setPortUp(ctx, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("failed to set grout port up: %w", err)
	}
	return nil
}

func removeHostTap(ctx context.Context, client *Client, linkPair hostnetwork.VethNames) error {
	if err := client.deletePort(ctx, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("failed to delete grout port %s: %w", linkPair.NamespaceSide, err)
	}
	if err := hostnetwork.RemoveLinkByName(linkPair.HostSide); err != nil {
		return fmt.Errorf("failed to delete host TAP %s: %w", linkPair.HostSide, err)
	}
	return nil
}
