// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/vishvananda/netlink"
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

// SetupL3VPN sets up a Layer 3 VPN in the router namespace the configurator
// was built for. It creates the VRF and moves the veth to it, exposing the VPN
// to the default host namespace.
func (c *VNIConfigurator) SetupL3VPN(ctx context.Context, params L3VPNParams) error {
	slog.DebugContext(ctx, "setting up L3VPN", "params", params)
	defer slog.DebugContext(ctx, "end setting up L3VPN", "params", params)

	if err := c.setupVRFInNS(ctx, params.VRF, srv6VRF); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup VRF: %w", err)
	}

	if params.LinkIPs == nil {
		slog.DebugContext(ctx, "no host veth configured, skipping setup")
		return nil
	}

	if err := c.setupHostVeth(
		ctx,
		vethNamesFromL3VPN(params.RDAssignedNumber),
		params.LinkIPs,
		params.VRF,
		params.TunnelOverhead); err != nil {
		return fmt.Errorf("SetupL3VPN: failed to setup host veth pair: %w", err)
	}
	return nil
}

// RemoveAllL3VPNs removes the host side of the veths of all L3VPNs.
func RemoveAllL3VPNs() error {
	return RemoveNonConfiguredL3VPNs([]L3VPNParams{})
}

// RemoveNonConfiguredL3VPNs removes the host side of the veths of the
// L3VPNs that are not configured anymore.
func RemoveNonConfiguredL3VPNs(params []L3VPNParams) error {
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
	if err := errors.Join(removeHostSideVeths(hostLinks, HostSRv6Prefix, rdAssignedNumbers)...); err != nil {
		return fmt.Errorf("RemoveNonConfiguredL3VPNs: failed to remove veths: %w", err)
	}
	return nil
}
