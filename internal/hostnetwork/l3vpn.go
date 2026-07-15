// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	// SRv6Overhead is the number of bytes added by SRv6 encapsulation (outer IPv6 header + SRH)
	// 40 bytes IPv6 header + 24 bytes for the SRH.
	SRv6Overhead = 64
)

type L3VPNParams struct {
	Name             string   `json:"name"`
	LinkIPs          *LinkIPs `json:"link_ips"`
	VRF              string   `json:"vrf"`
	TargetNS         string   `json:"targetns"`
	RDAssignedNumber int32    `json:"rdassignednumber"`
	TunnelOverhead   int      `json:"tunneloverhead"`
}

// SetupL3VPN sets up a Layer 3 VPN in the target namespace.
// It uses setupL3VPN to create the necessary VRF, and moves the veth to the
// VRF corresponding to the L3 routing domain, exposing it to the default host
// namespace.
func SetupL3VPN(ctx context.Context, params L3VPNParams) error {
	slog.DebugContext(ctx, "setting up L3VPN", "params", params)
	defer slog.DebugContext(ctx, "end setting up L3VPN", "params", params)

	if err := setupL3VPN(ctx, params); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup L3VPN: %w", err)
	}

	if params.LinkIPs == nil {
		slog.DebugContext(ctx, "no host veth configured, skipping setup")
		return nil
	}

	if err := setupHostVeth(
		ctx,
		vethNamesFromL3VPN(params.RDAssignedNumber),
		params.TargetNS,
		params.LinkIPs,
		params.VRF,
		params.TunnelOverhead); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup host veth pair: %w", err)
	}
	return nil
}

// setupL3VPN sets up the configuration required by FRR to
// serve a given L3VPN in the target namespace. This includes:
// - a linux VRF
// - sysctl strict mode and disable RP filter (for SRv6)
func setupL3VPN(ctx context.Context, params L3VPNParams) error {
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	return netnamespace.In(ns, func() error {
		slog.DebugContext(ctx, "setting up vrf", "vrf", params.VRF)
		return setupVRF(params.VRF, srv6VRF)
	})
}

// RemoveAllL3VPNs removes from the target namespace the veths
// for all L3VPNs.
func RemoveAllL3VPNs(targetNS string) error {
	return RemoveNonConfiguredL3VPNs(targetNS, []L3VPNParams{})
}

// RemoveNonConfiguredL3VPNs removes from the target namespace the
// leftovers corresponding to L3VPNs that are not configured anymore.
func RemoveNonConfiguredL3VPNs(targetNS string, params []L3VPNParams) error {
	vrfs := map[string]bool{}
	rdAssignedNumbers := map[int32]bool{}
	for _, p := range params {
		vrfs[p.VRF] = true
		rdAssignedNumbers[p.RDAssignedNumber] = true
	}

	hostLinks, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to list links: %w", err)
	}
	if err := errors.Join(removeHostSideVeths(hostLinks, HostVethPrefix+SRv6Infix, rdAssignedNumbers)...); err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to remove veths: %w", err)
	}
	return nil
}
