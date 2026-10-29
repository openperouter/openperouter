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

// CleanupLinks is a snapshot of the host and router namespace links, listed
// once so that the cleanup steps of a datapath pass share it instead of
// listing the same namespaces once per resource type.
type CleanupLinks struct {
	Host   []netlink.Link
	Router []netlink.Link
}

// ListCleanupLinks lists the links of the host namespace and of targetNS.
func ListCleanupLinks(targetNS string) (CleanupLinks, error) {
	hostLinks, err := listHostLinks()
	if err != nil {
		return CleanupLinks{}, err
	}
	routerLinks, err := listLinksInNamespace(targetNS)
	if err != nil {
		return CleanupLinks{}, err
	}
	return CleanupLinks{Host: hostLinks, Router: routerLinks}, nil
}

// RemoveAllVRFs removes all VRFs from the target namespace.
func RemoveAllVRFs(targetNS string) error {
	return RemoveNonConfiguredVRFs(targetNS, map[string]bool{})
}

// RemoveNonConfiguredVRFs removes from the target namespace the leftover VRFs
// that are not configured anymore.
func RemoveNonConfiguredVRFs(targetNS string, vrfs map[string]bool) error {
	links, err := listLinksInNamespace(targetNS)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredVRFs: %w", err)
	}
	return CleanupLinks{Router: links}.RemoveNonConfiguredVRFs(vrfs)
}

// RemoveNonConfiguredVRFs removes the VRFs of the router namespace that are
// not configured anymore.
func (l CleanupLinks) RemoveNonConfiguredVRFs(vrfs map[string]bool) error {
	if err := errors.Join(removeNamespaceSideVRFs(l.Router, vrfs)...); err != nil {
		return fmt.Errorf("RemoveNonConfiguredVRFs: %w", err)
	}
	return nil
}

// setupHostVeth configures the veth pair that connects the host to the perouter namespace, for
// L3VNI and L3VPN. underlayMTU is the MTU of the underlay, resolved once per
// datapath pass by the caller; zero leaves the veth MTU at its default.
func setupHostVeth(ctx context.Context, vethNames VethNames, targetNS string, linkIPs *LinkIPs,
	vrfName string, tunnelOverhead, underlayMTU int) error {
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

	err = AssignIPsToInterface(hostVethLink, linkIPs.HostIPv4, linkIPs.HostIPv6)
	if err != nil {
		return fmt.Errorf("failed to assign IPs to host veth: %w", err)
	}

	if err := SetVethMTUForTunnelOverhead(hostVethLink, underlayMTU, tunnelOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on host veth %s: %w", vethNames.HostSide, err)
	}

	return netnamespace.In(ns, func() error {
		peVethLink, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in namespace %s: %w", vethNames.NamespaceSide, targetNS, err)
		}

		if err := SetVethMTUForTunnelOverhead(peVethLink, underlayMTU, tunnelOverhead); err != nil {
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
		// Note: since the ipv6 address is removed after attaching the veth to the vrf, this has to
		// be performed after the veth is attached to the vrf.
		err = AssignIPsToInterface(peVethLink, linkIPs.NSIPv4, linkIPs.NSIPv6)
		if err != nil {
			return fmt.Errorf("failed to assign IPs to PE veth: %w", err)
		}
		return nil
	})
}

// AssignIPsToInterface assigns both IPv4 and IPv6 addresses to an interface.
// It fails if no IPs are provided (both IPv4 and IPv6 are empty).
func AssignIPsToInterface(link netlink.Link, ipv4, ipv6 string) error {
	if ipv4 == "" && ipv6 == "" {
		return fmt.Errorf("at least one IP address must be provided (IPv4 or IPv6)")
	}

	if ipv4 != "" {
		if err := AssignIPToInterface(link, ipv4); err != nil {
			return fmt.Errorf("failed to assign IPv4 address %s: %w", ipv4, err)
		}
	}

	if ipv6 != "" {
		if err := AssignIPToInterface(link, ipv6); err != nil {
			return fmt.Errorf("failed to assign IPv6 address %s: %w", ipv6, err)
		}
	}

	return nil
}

// SetVethMTUForTunnelOverhead sets the MTU on a veth interface to account for tunnel overhead.
// If the underlay MTU is not found, or if the resulting MTU would be too small,
// the MTU is left unchanged.
func SetVethMTUForTunnelOverhead(link netlink.Link, underlayMTU int, overhead int) error {
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

func removeNamespaceSideVRFs(links []netlink.Link, vrfs map[string]bool) []error {
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

func listHostLinks() ([]netlink.Link, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list host links: %w", err)
	}
	return links, nil
}

func listLinksInNamespace(targetNS string) ([]netlink.Link, error) {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return nil, fmt.Errorf("failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	var links []netlink.Link
	if err := netnamespace.In(ns, func() error {
		var err error
		links, err = netlink.LinkList()
		if err != nil {
			return fmt.Errorf("failed to list links: %w", err)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list links in namespace %s: %w", targetNS, err)
	}
	return links, nil
}
