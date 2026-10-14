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

const (
	hostTapSRv6Prefix = "host-s-"
	pePortSRv6Prefix  = "pe-s-"
)

// SetupL3VPN creates the grout VRF backing the L3VPN and, when the L3VPN
// has a host session, the TAP port connecting the VRF to the host.
func SetupL3VPN(ctx context.Context, client *Client, params hostnetwork.L3VPNParams) error {
	slog.DebugContext(ctx, "setup L3VPN", "vrf", params.VRF, "rd", params.RDAssignedNumber)
	defer slog.DebugContext(ctx, "setup L3VPN done", "vrf", params.VRF, "rd", params.RDAssignedNumber)

	if err := client.ensureVRF(ctx, params.VRF); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to create VRF %s: %w", params.VRF, err)
	}

	if params.LinkIPs == nil {
		slog.DebugContext(ctx, "no host TAP configured, skipping setup")
		return nil
	}

	linkPair := linkPairFromL3VPN(params.RDAssignedNumber)

	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("SetupL3VPN: failed to find namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := ensureTapPortInHostNamespace(ctx, client, linkPair.NamespaceSide, linkPair.HostSide, params.VRF, ns); err != nil {
		return err
	}

	hostTap, err := netlink.LinkByName(linkPair.HostSide)
	if err != nil {
		return fmt.Errorf("host TAP %s not found after move: %w", linkPair.HostSide, err)
	}
	if err := hostnetwork.AssignIPsToInterface(hostTap, params.LinkIPs.HostIPv4, params.LinkIPs.HostIPv6); err != nil {
		return fmt.Errorf("failed to assign IPs to host TAP: %w", err)
	}

	underlayMTU, err := hostnetwork.FindUnderlayMTU(ns)
	if err != nil {
		return fmt.Errorf("could not find underlay MTU: %w", err)
	}
	if err := hostnetwork.SetVethMTUForTunnelOverhead(hostTap, underlayMTU, params.TunnelOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on host TAP %s: %w", linkPair.HostSide, err)
	}

	if err := ensurePortAddresses(ctx, client, linkPair.NamespaceSide, params.LinkIPs.NSIPv4, params.LinkIPs.NSIPv6); err != nil {
		return fmt.Errorf("failed to assign IPs to grout port: %w", err)
	}
	if err := client.setPortUp(ctx, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("failed to set grout port up: %w", err)
	}

	return nil
}

func RemoveAllL3VPNs(ctx context.Context, client *Client) error {
	return RemoveNonConfiguredL3VPNs(ctx, client, []hostnetwork.L3VPNParams{})
}

// RemoveNonConfiguredL3VPNs removes the TAP ports of the L3VPNs that are
// not configured anymore. The VRFs are removed by RemoveNonConfiguredVRFs.
func RemoveNonConfiguredL3VPNs(ctx context.Context, client *Client, configured []hostnetwork.L3VPNParams) error {
	slog.DebugContext(ctx, "removing stale L3VPNs")
	defer slog.DebugContext(ctx, "removing stale L3VPNs done")

	configuredRDs := make(map[int32]bool)
	for _, p := range configured {
		configuredRDs[p.RDAssignedNumber] = true
	}

	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to list interfaces: %w", err)
	}

	for _, rd := range findStaleL3VPNs(ifaces, configuredRDs) {
		slog.InfoContext(ctx, "removing stale L3VPN", "rd", rd)
		if err := removeL3VPN(ctx, client, rd); err != nil {
			return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to remove L3VPN with RD %d: %w", rd, err)
		}
	}

	return nil
}

func removeL3VPN(ctx context.Context, client *Client, rdAssignedNumber int32) error {
	linkPair := linkPairFromL3VPN(rdAssignedNumber)

	if err := client.deletePort(ctx, linkPair.NamespaceSide); err != nil {
		return fmt.Errorf("failed to delete grout port %s: %w", linkPair.NamespaceSide, err)
	}

	if err := hostnetwork.RemoveLinkByName(linkPair.HostSide); err != nil {
		return fmt.Errorf("failed to delete host TAP %s: %w", linkPair.HostSide, err)
	}

	return nil
}

func findStaleL3VPNs(ifaces []groutInterface, configuredRDs map[int32]bool) []int32 {
	var stale []int32
	for _, iface := range ifaces {
		if iface.Type != "port" {
			continue
		}
		var rd int32
		if n, _ := fmt.Sscanf(iface.Name, pePortSRv6Prefix+"%d", &rd); n != 1 {
			continue
		}
		if linkPairFromL3VPN(rd).NamespaceSide != iface.Name {
			continue
		}
		if !configuredRDs[rd] {
			stale = append(stale, rd)
		}
	}
	return stale
}

// linkPairFromL3VPN returns the names of the link pair legs
// corresponding to the default namespace and the target namespace,
// based on the L3VPN route distinguisher assigned number.
func linkPairFromL3VPN(rdAssignedNumber int32) hostnetwork.VethNames {
	hostSide := fmt.Sprintf("%s%d", hostTapSRv6Prefix, rdAssignedNumber)
	peSide := fmt.Sprintf("%s%d", pePortSRv6Prefix, rdAssignedNumber)
	return hostnetwork.VethNames{HostSide: hostSide, NamespaceSide: peSide}
}
