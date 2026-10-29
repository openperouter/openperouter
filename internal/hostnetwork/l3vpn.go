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
	// SRv6OverheadEncapReduced is the number of bytes added by SRv6 reduced encapsulation: 40 bytes for the IPv6 header.
	SRv6OverheadEncapReduced = 40
)

type L3VPNParams struct {
	Name             string
	LinkIPs          *LinkIPs
	VRF              string
	TargetNS         string
	RDAssignedNumber int32
	TunnelOverhead   int
}

// SetupL3VPN sets up a Layer 3 VPN in the target namespace.
// It uses setupL3VPN to create the necessary VRF, and moves the veth to the
// VRF corresponding to the L3 routing domain, exposing it to the default host
// namespace. shared is the state shared by every VNI of the pass; when nil,
// SetupL3VPN resolves its own state and closes it before returning.
func SetupL3VPN(ctx context.Context, params L3VPNParams, shared *VNISharedState) error {
	slog.DebugContext(ctx, "setting up L3VPN", "params", params)
	defer slog.DebugContext(ctx, "end setting up L3VPN", "params", params)

	if shared == nil {
		var err error
		shared, err = newOwnedVNISharedState(params.TargetNS)
		if err != nil {
			return err
		}
		defer closeVNISharedState(shared)
	}

	if err := setupL3VPN(ctx, params, shared.ns); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup L3VPN: %w", err)
	}

	if params.LinkIPs == nil {
		slog.DebugContext(ctx, "no host veth configured, skipping setup")
		return nil
	}

	if err := setupHostVeth(
		ctx,
		vethNamesFromL3VPN(params.RDAssignedNumber),
		params.LinkIPs,
		params.VRF,
		params.TunnelOverhead,
		shared); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup host veth pair: %w", err)
	}
	return nil
}

// setupL3VPN sets up the configuration required by FRR to
// serve a given L3VPN in the target namespace. This includes:
// - a linux VRF
// - sysctl strict mode and disable RP filter (for SRv6)
func setupL3VPN(ctx context.Context, params L3VPNParams, ns netns.NsHandle) error {
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
	hostLinks, err := listHostLinks()
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: %w", err)
	}
	if err := removeL3VPNVeths(hostLinks, params); err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to remove veths: %w", err)
	}
	return nil
}

// RemoveNonConfiguredL3VPNs removes the host namespace leftovers
// corresponding to L3VPNs that are not configured anymore.
func (l *CleanupLinks) RemoveNonConfiguredL3VPNs(params []L3VPNParams) error {
	if err := removeL3VPNVeths(l.Host, params); err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to remove veths: %w", err)
	}
	return nil
}

func removeL3VPNVeths(hostLinks []netlink.Link, params []L3VPNParams) error {
	rdAssignedNumbers := map[int32]bool{}
	for _, p := range params {
		rdAssignedNumbers[p.RDAssignedNumber] = true
	}
	return errors.Join(removeHostSideVeths(hostLinks, HostVethPrefix+SRv6Infix, rdAssignedNumbers)...)
}
