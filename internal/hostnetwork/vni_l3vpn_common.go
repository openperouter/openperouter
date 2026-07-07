// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	// MinVethMTU is the minimum MTU we will set on the veth.
	// 1280 is the IPv6 minimum MTU (RFC 8200); the kernel will reject
	// or disable IPv6 on the link below this.
	MinVethMTU = 1280
)

// RemoveAllVRFs removes all VRFs from the target namespace.
func RemoveAllVRFs(targetNS string) error {
	return RemoveNonConfiguredVRFs(targetNS, map[string]bool{})
}

// RemoveNonConfiguredVRFs removes from the target namespace the leftover VRFs
// that are not configured anymore.
func RemoveNonConfiguredVRFs(targetNS string, vrfs map[string]bool) error {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredVRFs: failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	return netnamespace.In(ns, func() error {
		return errors.Join(removeNamespaceSideVRFs(vrfs)...)
	})
}

// setupHostVeth configures the veth pair that connects the host to the perouter namespace, for
// L3VNI and L3VPN.
func setupHostVeth(ctx context.Context, vethNames VethNames, targetNS string, hostVeth *Veth,
	vrfName string, tunnelOverhead int) error {
	if err := setupNamespacedVeth(ctx, vethNames, targetNS); err != nil {
		return fmt.Errorf("failed to setup veth: %w", err)
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	hostVethLink, err := netlink.LinkByName(vethNames.HostSide)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("host veth %s does not exist, cannot setup", vethNames.HostSide)
	}
	if err != nil {
		return fmt.Errorf("failed to get host veth %s: %w", vethNames.HostSide, err)
	}

	err = assignIPsToInterface(hostVethLink, hostVeth.HostIPv4, hostVeth.HostIPv6)
	if err != nil {
		return fmt.Errorf("failed to assign IPs to host veth: %w", err)
	}

	underlayMTU, err := findUnderlayMTU(ns)
	if err != nil {
		return fmt.Errorf("could not find underlay MTU: %w", err)
	}

	if err := setVethMTUForTunnelOverhead(hostVethLink, underlayMTU, tunnelOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on host veth %s: %w", vethNames.HostSide, err)
	}

	return netnamespace.In(ns, func() error {
		peVethLink, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", vethNames.NamespaceSide, targetNS, err)
		}

		if err := setVethMTUForTunnelOverhead(peVethLink, underlayMTU, tunnelOverhead); err != nil {
			return fmt.Errorf("failed to set MTU on pe veth %s: %w", vethNames.NamespaceSide, err)
		}

		vrfLink, err := netlink.LinkByName(vrfName)
		if err != nil {
			return fmt.Errorf("could not find vrf %s in namespace %s: %w", vrfName, targetNS, err)
		}

		err = linkSetMaster(peVethLink, vrfLink)
		if err != nil {
			return fmt.Errorf("failed to set vrf %s as master of pe veth %s: %w", vrfName, peVethLink.Attrs().Name, err)
		}
		// Note: since the ipv6 address is removed after enslaving the veth to the vrf, this has to
		// be performed after the veth is enslaved to the vrf.
		err = assignIPsToInterface(peVethLink, hostVeth.NSIPv4, hostVeth.NSIPv6)
		if err != nil {
			return fmt.Errorf("failed to assign IPs to PE veth: %w", err)
		}
		return nil
	})
}

// assignIPsToInterface assigns both IPv4 and IPv6 addresses to an interface.
// It fails if no IPs are provided (both IPv4 and IPv6 are empty).
func assignIPsToInterface(link netlink.Link, ipv4, ipv6 string) error {
	if ipv4 == "" && ipv6 == "" {
		return fmt.Errorf("at least one IP address must be provided (IPv4 or IPv6)")
	}

	if ipv4 != "" {
		if err := assignIPToInterface(link, ipv4); err != nil {
			return fmt.Errorf("failed to assign IPv4 address %s: %w", ipv4, err)
		}
	}

	if ipv6 != "" {
		if err := assignIPToInterface(link, ipv6); err != nil {
			return fmt.Errorf("failed to assign IPv6 address %s: %w", ipv6, err)
		}
	}

	return nil
}

// setVethMTUForTunnelOverhead sets the MTU on a veth interface to account for tunnel overhead.
// If the underlay MTU is not found, or if the resulting MTU would be too small,
// the MTU is left unchanged.
func setVethMTUForTunnelOverhead(link netlink.Link, underlayMTU int, overhead int) error {
	if underlayMTU == 0 {
		slog.Debug("No underlay MTU found, leaving veth MTU at default", "veth", link.Attrs().Name)
		return nil
	}
	targetMTU := underlayMTU - overhead
	if targetMTU <= MinVethMTU {
		slog.Warn("Calculated veth MTU is too low, leaving at default",
			"veth", link.Attrs().Name,
			"underlayMTU", underlayMTU,
			"calculatedMTU", targetMTU)
		return nil
	}
	return linkSetMTU(link, targetMTU)
}

func removeHostSideVeths(hostLinks []netlink.Link, prefix string, interfaceIDs map[int32]bool) []error {
	var failedDeletes []error
	for _, hl := range hostLinks {
		if hl.Type() != VethLinkType {
			continue
		}
		if !strings.HasPrefix(hl.Attrs().Name, prefix) {
			continue
		}
		interfaceID, err := interfaceIDFromPrefix(hl.Attrs().Name, prefix)
		if err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
			continue
		}
		if interfaceIDs[interfaceID] {
			continue
		}
		if err := netlink.LinkDel(hl); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove host leg: %s %w", hl.Attrs().Name, err))
		}
	}
	return failedDeletes
}

func removeNamespaceSideVRFs(vrfs map[string]bool) []error {
	links, err := netlink.LinkList()
	if err != nil {
		return []error{fmt.Errorf("removeNamespaceSideVRFs: failed to list links: %w", err)}
	}

	var failedDeletes []error
	for _, l := range links {
		if l.Type() != VRFLinkType {
			continue
		}
		if vrfs[l.Attrs().Name] {
			continue
		}
		if err := netlink.LinkDel(l); err != nil {
			failedDeletes = append(failedDeletes, fmt.Errorf("remove non configured interfaces: failed to delete vrf %s %w", l.Attrs().Name, err))
		}
	}
	return failedDeletes
}
