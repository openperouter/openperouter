// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

// VRFRemovalError distinguishes leftover routes from a failure to remove a live VRF.
type VRFRemovalError struct {
	OnlyOrphanGuards bool
	Err              error
}

func (e *VRFRemovalError) Error() string { return e.Err.Error() }
func (e *VRFRemovalError) Unwrap() error { return e.Err }

var deleteVRFLink = netlink.LinkDel
var deleteVRFAttachment = netlink.LinkDel

// CleanupOrphanVRFDefaultGuards retries owned route cleanup after the VRF is gone.
func CleanupOrphanVRFDefaultGuards(targetNS string) error {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("open orphan guard namespace: %w", err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("close orphan guard namespace", "err", err)
		}
	}()
	return netnamespace.In(ns, cleanupOrphanVRFDefaultGuards)
}

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
		failures := removeNamespaceSideVRFs(vrfs)
		if len(failures) == 0 {
			return nil
		}
		orphanOnly := true
		for _, failure := range failures {
			var cleanup *VRFRemovalError
			if !errors.As(failure, &cleanup) || !cleanup.OnlyOrphanGuards {
				orphanOnly = false
			}
		}
		return &VRFRemovalError{OnlyOrphanGuards: orphanOnly, Err: errors.Join(failures...)}
	})
}

// CleanupObsoleteIsolationVRF removes only the confirmed managed table owner.
func CleanupObsoleteIsolationVRF(targetNS string, desired map[string]bool, conflict *VRFTableOwnershipError) error {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("open obsolete VRF namespace: %w", err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("close obsolete VRF namespace", "err", err)
		}
	}()
	return netnamespace.In(ns, func() error {
		return cleanupObsoleteIsolationVRF(desired, conflict)
	})
}

// CheckObsoleteIsolationVRF validates a withdrawal before FRR changes and returns
// the source which must remain in use until the old table owner has been removed.
func CheckObsoleteIsolationVRF(targetNS string, desired map[string]bool, conflict *VRFTableOwnershipError) (net.IP, error) {
	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return nil, fmt.Errorf("open obsolete VRF namespace: %w", err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("close obsolete VRF namespace", "err", err)
		}
	}()
	var source net.IP
	err = netnamespace.In(ns, func() error {
		if _, _, err := inspectObsoleteIsolationVRF(desired, conflict); err != nil {
			return err
		}
		var err error
		source, err = getSRv6TunnelSource()
		return err
	})
	return source, err
}

// setupHostVeth configures the veth pair that connects the host to the router
// namespace the configurator was built for, for L3VNI and L3VPN.
func (c *VNIConfigurator) setupHostVeth(ctx context.Context, vethNames VethNames, linkIPs *LinkIPs,
	vrfName string, tunnelOverhead int) error {
	if err := setupNamespacedVethInNamespace(ctx, vethNames, c.ns); err != nil {
		return fmt.Errorf("failed to setup veth: %w", err)
	}

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

	if err := SetVethMTUForTunnelOverhead(hostVethLink, c.underlayMTU, tunnelOverhead); err != nil {
		return fmt.Errorf("failed to set MTU on host veth %s: %w", vethNames.HostSide, err)
	}

	return netnamespace.In(c.ns, func() error {
		peVethLink, err := netlink.LinkByName(vethNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in the router namespace: %w", vethNames.NamespaceSide, err)
		}

		if err := SetVethMTUForTunnelOverhead(peVethLink, c.underlayMTU, tunnelOverhead); err != nil {
			return fmt.Errorf("failed to set MTU on pe veth %s: %w", vethNames.NamespaceSide, err)
		}

		vrfLink, err := netlink.LinkByName(vrfName)
		if err != nil {
			return fmt.Errorf("could not find vrf %s in the router namespace: %w", vrfName, err)
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
		if err := deleteObsoleteVRF(l.(*netlink.Vrf)); err != nil {
			failedDeletes = append(failedDeletes, err)
		}
	}
	if len(failedDeletes) == 0 {
		if err := cleanupOrphanVRFDefaultGuards(); err != nil {
			failedDeletes = append(failedDeletes, &VRFRemovalError{OnlyOrphanGuards: true, Err: err})
		}
	}
	return failedDeletes
}

func deleteObsoleteVRF(vrf *netlink.Vrf) error {
	vrfGuardMu.Lock()
	defer vrfGuardMu.Unlock()
	stale, err := vrfDefaultGuardsToRemove(vrf)
	if err != nil {
		return err
	}
	if err := deleteVRFLink(vrf); err != nil {
		return fmt.Errorf("remove obsolete VRF %s: %w", vrf.Name, err)
	}
	// An obsolete owner whose deletion failed still needs the table's guards.
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("inspect live VRF owners after deleting %s: %w", vrf.Name, err)
	}
	for _, link := range links {
		if live, ok := link.(*netlink.Vrf); ok && live.Table == vrf.Table {
			return nil
		}
	}
	if err := deleteVRFDefaultGuards(stale); err != nil {
		return &VRFRemovalError{OnlyOrphanGuards: true, Err: err}
	}
	return nil
}

func cleanupOrphanVRFDefaultGuards() error {
	vrfGuardMu.Lock()
	defer vrfGuardMu.Unlock()
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("list VRF tables before orphan guard cleanup: %w", err)
	}
	tables := map[uint32]bool{}
	for _, link := range links {
		if vrf, ok := link.(*netlink.Vrf); ok {
			tables[vrf.Table] = true
		}
	}
	stale, err := vrfDefaultGuardsToRemove(&netlink.Vrf{Table: 0})
	if err != nil {
		return err
	}
	var orphaned []netlink.Route
	for _, route := range stale {
		if !tables[uint32(route.Table)] && !reservedVRFTable(uint32(route.Table)) {
			orphaned = append(orphaned, route)
		}
	}
	return deleteVRFDefaultGuards(orphaned)
}

func cleanupObsoleteIsolationVRF(desired map[string]bool, conflict *VRFTableOwnershipError) error {
	owner, attachments, err := inspectObsoleteIsolationVRF(desired, conflict)
	if err != nil {
		return err
	}
	if err := recheckObsoleteVRF(owner); err != nil {
		return err
	}
	for _, attachment := range attachments {
		if err := removeObsoleteVRFAttachment(attachment); err != nil {
			return err
		}
	}
	if err := recheckObsoleteVRF(owner); err != nil {
		return err
	}
	if err := requireNoAttachedLinks(owner); err != nil {
		return err
	}
	return deleteObsoleteVRF(owner)
}

func recheckObsoleteVRF(owner *netlink.Vrf) error {
	link, err := netlink.LinkByName(owner.Name)
	if err != nil {
		return fmt.Errorf("recheck obsolete VRF %s: %w", owner.Name, err)
	}
	current, ok := link.(*netlink.Vrf)
	if !ok || current.Index != owner.Index || current.Table != owner.Table || current.Alias != managedVRFAlias {
		return fmt.Errorf("obsolete VRF %s changed before deletion", owner.Name)
	}
	return nil
}

// Validate the whole attachment set before deleting anything. Managed bridges
// must lose their ports first so deletion cannot move tenant traffic into main.
func obsoleteVRFAttachments(owner *netlink.Vrf, links []netlink.Link) ([]netlink.Link, error) {
	var attachments []netlink.Link
	for _, link := range links {
		if link.Attrs().MasterIndex != owner.Index {
			continue
		}
		if managedVRFVeth(link) {
			attachments = append(attachments, link)
			continue
		}
		bridge, ok := link.(*netlink.Bridge)
		if !ok {
			return nil, fmt.Errorf("cannot remove obsolete VRF %s while interface %s is attached", owner.Name, link.Attrs().Name)
		}
		ports, err := obsoleteVNIBridgePorts(bridge, links)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, ports...)
		attachments = append(attachments, bridge)
	}
	return attachments, nil
}

func managedVRFVeth(link netlink.Link) bool {
	if link.Type() != VethLinkType {
		return false
	}
	for _, prefix := range []string{PESRv6Prefix, PEEVPNPrefix} {
		id, err := interfaceIDFromPrefix(link.Attrs().Name, prefix)
		if err == nil && id > 0 && link.Attrs().Name == fmt.Sprintf("%s%d", prefix, id) {
			return true
		}
	}
	return false
}

func obsoleteVNIBridgePorts(bridge *netlink.Bridge, links []netlink.Link) ([]netlink.Link, error) {
	vni, err := vniFromBridgeName(bridge.Name)
	if err != nil || vni <= 0 || bridge.Name != BridgeName(vni) {
		return nil, fmt.Errorf("cannot remove unmanaged bridge %s from obsolete VRF", bridge.Name)
	}
	var ports []netlink.Link
	for _, link := range links {
		if link.Attrs().MasterIndex != bridge.Index {
			continue
		}
		if link.Type() == VethLinkType && link.Attrs().Name == vethNamesFromVNI(vni).NamespaceSide {
			ports = append(ports, link)
			continue
		}
		vxlan, ok := link.(*netlink.Vxlan)
		if ok && vxlan.VxlanId == int(vni) && vxlan.Name == vxLanNameFromVNI(vni) {
			ports = append(ports, link)
			continue
		}
		return nil, fmt.Errorf("cannot remove obsolete bridge %s while interface %s is attached", bridge.Name, link.Attrs().Name)
	}
	return ports, nil
}

func removeObsoleteVRFAttachment(link netlink.Link) error {
	current, err := netlink.LinkByName(link.Attrs().Name)
	if err != nil {
		return fmt.Errorf("recheck obsolete attachment %s: %w", link.Attrs().Name, err)
	}
	if current.Attrs().Index != link.Attrs().Index || current.Type() != link.Type() ||
		current.Attrs().MasterIndex != link.Attrs().MasterIndex {
		return fmt.Errorf("obsolete attachment %s changed before deletion", link.Attrs().Name)
	}
	if err := requireNoAttachedLinks(current); err != nil {
		return err
	}
	if err := deleteVRFAttachment(current); err != nil {
		return fmt.Errorf("remove obsolete attachment %s: %w", current.Attrs().Name, err)
	}
	return nil
}

func requireNoAttachedLinks(master netlink.Link) error {
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("list attachments of %s: %w", master.Attrs().Name, err)
	}
	for _, link := range links {
		if link.Attrs().MasterIndex == master.Attrs().Index {
			return fmt.Errorf("cannot remove %s while interface %s is attached", master.Attrs().Name, link.Attrs().Name)
		}
	}
	return nil
}

func inspectObsoleteIsolationVRF(desired map[string]bool, conflict *VRFTableOwnershipError) (*netlink.Vrf, []netlink.Link, error) {
	link, err := netlink.LinkByName(conflict.VRF)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect obsolete VRF %s: %w", conflict.VRF, err)
	}
	owner, ok := link.(*netlink.Vrf)
	if !ok || desired[conflict.VRF] || owner.Alias != managedVRFAlias || owner.Table != conflict.Table {
		return nil, nil, conflict
	}
	links, err := netlink.LinkList()
	if err != nil {
		return nil, nil, fmt.Errorf("list retained VRF tables: %w", err)
	}
	attachments, err := obsoleteVRFAttachments(owner, links)
	if err != nil {
		return nil, nil, err
	}
	return owner, attachments, nil
}
