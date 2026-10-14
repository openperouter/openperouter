// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/hostnetwork"
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

	if err := setupHostTap(ctx, client, linkPairFromL3VPN(params.RDAssignedNumber), params.TargetNS, params.VRF,
		*params.LinkIPs, params.TunnelOverhead); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup host TAP: %w", err)
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

	configuredRDs := make(map[int32]struct{}, len(configured))
	for _, p := range configured {
		configuredRDs[p.RDAssignedNumber] = struct{}{}
	}

	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to list interfaces: %w", err)
	}

	for _, rd := range findStaleL3VPNs(ifaces, configuredRDs) {
		slog.InfoContext(ctx, "removing stale L3VPN", "rd", rd)
		if err := removeHostTap(ctx, client, linkPairFromL3VPN(rd)); err != nil {
			return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to remove L3VPN with RD %d: %w", rd, err)
		}
	}

	return nil
}

func findStaleL3VPNs(ifaces []groutInterface, configuredRDs map[int32]struct{}) []int32 {
	var stale []int32
	for _, iface := range ifaces {
		if iface.Type != "port" {
			continue
		}
		rd, ok := rdFromL3VPNPortName(iface.Name)
		if !ok {
			continue
		}
		if _, ok := configuredRDs[rd]; !ok {
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

// rdFromL3VPNPortName returns the route distinguisher assigned number of an
// L3VPN grout port, and false for any other name.
func rdFromL3VPNPortName(name string) (int32, bool) {
	var rd int32
	if n, _ := fmt.Sscanf(name, pePortSRv6Prefix+"%d", &rd); n != 1 {
		return 0, false
	}
	// Sscanf ignores trailing text after the number.
	return rd, linkPairFromL3VPN(rd).NamespaceSide == name
}
