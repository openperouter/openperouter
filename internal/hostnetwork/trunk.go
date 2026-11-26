// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"strings"

	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/ovsmodel"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

const (
	trunkHostPrefix   = "host-t-"
	trunkPEPrefix     = "pe-t-"
	trunkBridgePrefix = "br-t-"
	trunkVXLanPrefix  = "vxt-"

	// trunkIDLength keeps every device name within IFNAMSIZ: the longest
	// prefix plus the identifier is host-t-<id>, 15 characters.
	trunkIDLength = 8
	// trunkIDBase is as high as Linux interface names and Kubernetes label
	// values both allow (letters and digits only). It packs ~5.95 bits per
	// character instead of hex's 4, so trunkIDLength characters carry ~48 bits
	// of the digest instead of 32, cutting collision probability by roughly
	// four orders of magnitude within the same IFNAMSIZ budget.
	trunkIDBase = 62
)

// trunkIDModulus bounds a TrunkID to exactly trunkIDLength base-trunkIDBase
// digits.
var trunkIDModulus = new(big.Int).Exp(big.NewInt(trunkIDBase), big.NewInt(trunkIDLength), nil)

// TrunkDeviceNames holds the four devices an L2Trunk owns, all derived from
// the trunk's stable identifier.
type TrunkDeviceNames struct {
	HostSide      string
	NamespaceSide string
	Bridge        string
	VXLan         string
}

// TrunkID returns the stable identifier of a trunk. It hashes namespace/name,
// not metadata.name: the reconciler lists trunks cluster-wide, so two trunks
// named "workloads" in different namespaces must not share device names.
func TrunkID(namespace, name string) string {
	digest := sha256.Sum256([]byte(namespace + "/" + name))
	id := new(big.Int).SetBytes(digest[:])
	id.Mod(id, trunkIDModulus)
	return padTrunkID(id.Text(trunkIDBase))
}

// padTrunkID left-pads a base-trunkIDBase identifier with '0' to the fixed
// width device names rely on: big.Int.Text drops leading zero digits, so a
// small remainder would otherwise produce a shorter string.
func padTrunkID(id string) string {
	if len(id) >= trunkIDLength {
		return id
	}
	return strings.Repeat("0", trunkIDLength-len(id)) + id
}

// TrunkDeviceNamesFor returns the device names of a trunk.
func TrunkDeviceNamesFor(namespace, name string) TrunkDeviceNames {
	return trunkDeviceNamesFromID(TrunkID(namespace, name))
}

func trunkDeviceNamesFromID(id string) TrunkDeviceNames {
	return TrunkDeviceNames{
		HostSide:      trunkHostPrefix + id,
		NamespaceSide: trunkPEPrefix + id,
		Bridge:        trunkBridgePrefix + id,
		VXLan:         trunkVXLanPrefix + id,
	}
}

func trunkIDFromHostName(name string) (string, error) {
	return trunkIDFromName(name, trunkHostPrefix)
}

func trunkIDFromPEName(name string) (string, error) {
	return trunkIDFromName(name, trunkPEPrefix)
}

func trunkIDFromBridgeName(name string) (string, error) {
	return trunkIDFromName(name, trunkBridgePrefix)
}

func trunkIDFromVXLanName(name string) (string, error) {
	return trunkIDFromName(name, trunkVXLanPrefix)
}

func trunkIDFromName(name, prefix string) (string, error) {
	if !strings.HasPrefix(name, prefix) {
		return "", NotRouterInterfaceError{Name: name}
	}
	id := strings.TrimPrefix(name, prefix)
	if len(id) != trunkIDLength {
		return "", fmt.Errorf("interface %s does not use the trunk naming scheme", name)
	}
	return id, nil
}

// SetupL2Trunk creates or converges the devices an L2Trunk owns eagerly: the
// veth pair, the shared VLAN-aware bridge and the shared external vnifilter
// VXLAN. Per-L2VNI tagged membership and mappings are added by SetupL2TrunkVNI.
func (c *VNIConfigurator) SetupL2Trunk(ctx context.Context, params L2TrunkParams) error {
	names := TrunkDeviceNamesFor(params.Namespace, params.Name)

	if err := ensureTrunkVeth(ctx, names, c.ns); err != nil {
		return fmt.Errorf("SetupL2Trunk: %w", err)
	}

	hostVeth, err := netlink.LinkByName(names.HostSide)
	if err != nil {
		return fmt.Errorf("SetupL2Trunk: failed to get host veth %s: %w", names.HostSide, err)
	}
	if err := SetVethMTUForTunnelOverhead(hostVeth, c.underlayMTU, params.TunnelOverhead); err != nil {
		return fmt.Errorf("SetupL2Trunk: failed to set MTU on host veth %s: %w", names.HostSide, err)
	}
	if err := sysctl.Ensure(sysctl.AddrGenMode(hostVeth.Attrs().Name, "1")); err != nil {
		return err
	}
	if params.HostMaster != nil {
		if err := SetupHostMaster(ctx, c.ovsClient, params.HostMaster, 0, hostVeth); err != nil {
			return err
		}
	}

	if err := netnamespace.In(c.ns, func() error {
		bridge, err := ensureSwitchingBridge(names.Bridge)
		if err != nil {
			return err
		}
		routerVeth, err := netlink.LinkByName(names.NamespaceSide)
		if err != nil {
			return fmt.Errorf("could not find peer veth %s in the router namespace: %w", names.NamespaceSide, err)
		}
		if err := SetVethMTUForTunnelOverhead(routerVeth, c.underlayMTU, params.TunnelOverhead); err != nil {
			return fmt.Errorf("failed to set MTU on pe veth %s: %w", names.NamespaceSide, err)
		}
		if err := sysctl.Ensure(sysctl.AddrGenMode(routerVeth.Attrs().Name, "1")); err != nil {
			return err
		}
		if err := linkSetMaster(routerVeth, bridge); err != nil {
			return fmt.Errorf("failed to set bridge %s as master of pe veth %s: %w",
				names.Bridge, names.NamespaceSide, err)
		}
		if err := ensureLinkAlias(routerVeth, params.Namespace, params.Name); err != nil {
			return err
		}
		svd, err := ensureSVD(svdParams{
			Name:            names.VXLan,
			Bridge:          bridge,
			VTEPIP:          params.VTEPIP,
			Port:            params.VXLanPort,
			VtepDeviceIndex: c.vtepDeviceIndex,
			HardwareAddr:    bridge.HardwareAddr,
		})
		if err != nil {
			return err
		}
		if err := ensureLinkAlias(bridge, params.Namespace, params.Name); err != nil {
			return err
		}
		return ensureLinkAlias(svd, params.Namespace, params.Name)
	}); err != nil {
		return fmt.Errorf("SetupL2Trunk: failed to set up trunk devices: %w", err)
	}

	if err := ensureLinkAlias(hostVeth, params.Namespace, params.Name); err != nil {
		return err
	}
	return nil
}

// trunkPortSnapshot caches one trunk's VLAN membership, tunnel mappings and
// VNI filter entries across the several SetupL2TrunkVNI calls needed to bring
// up every L2VNI on the trunk, so a trunk with many tags reads each device's
// state once per reconcile instead of once per tag.
type trunkPortSnapshot struct {
	routerVethVlans  []vlanMembership
	bridgeVlans      []vlanMembership
	svdVlans         []vlanMembership
	svdMappings      []nl.TunnelInfo
	svdFilterEntries []vniFilterRange
}

// trunkSnapshotFor returns the cached snapshot for trunkID, building it from
// the current kernel state the first time it's needed. SetupL2Trunk runs
// before any SetupL2TrunkVNI call in a reconcile and adds no VLAN membership
// of its own, so taking the snapshot lazily on first use is safe.
func (c *VNIConfigurator) trunkSnapshotFor(routerVeth, bridge, svd netlink.Link, trunkID string) (*trunkPortSnapshot, error) {
	if snapshot, ok := c.trunkSnapshots[trunkID]; ok {
		return snapshot, nil
	}
	routerVethVlans, err := portVlanMemberships(routerVeth)
	if err != nil {
		return nil, err
	}
	bridgeVlans, err := portVlanMemberships(bridge)
	if err != nil {
		return nil, err
	}
	svdVlans, err := portVlanMemberships(svd)
	if err != nil {
		return nil, err
	}
	svdMappings, err := vxlanTunnelMappings(svd.Attrs().Index)
	if err != nil {
		return nil, err
	}
	svdFilterEntries, err := vniFilterEntries(svd.Attrs().Index)
	if err != nil {
		return nil, err
	}
	snapshot := &trunkPortSnapshot{
		routerVethVlans:  routerVethVlans,
		bridgeVlans:      bridgeVlans,
		svdVlans:         svdVlans,
		svdMappings:      svdMappings,
		svdFilterEntries: svdFilterEntries,
	}
	c.trunkSnapshots[trunkID] = snapshot
	return snapshot, nil
}

// SetupL2TrunkVNI maps one L2VNI's VLAN tag to its VNI on the shared trunk
// devices and creates the br-pe-<vni> endpoint that plays the same SVI role it
// plays for a standalone L2VNI.
func (c *VNIConfigurator) SetupL2TrunkVNI(ctx context.Context, params L2VNIParams) error { // nolint:gocognit
	if params.Trunk == nil || params.Trunk.TrunkID == "" {
		return errors.New("SetupL2TrunkVNI requires a trunk id")
	}
	names := trunkDeviceNamesFromID(params.Trunk.TrunkID)
	vid, err := vlanIDForTag(params.Trunk.VLAN)
	if err != nil {
		return fmt.Errorf("SetupL2TrunkVNI: %w", err)
	}

	var endpointOptions []NetlinkOption
	if len(params.L2GatewayIPs) > 0 {
		mac, err := BridgeFixedMAC(params.VNI)
		if err != nil {
			return fmt.Errorf("SetupL2TrunkVNI: failed to compute gateway MAC: %w", err)
		}
		endpointOptions = append(endpointOptions, bridgeMACOption(mac))
	}

	if err := netnamespace.In(c.ns, func() error {
		bridge, err := c.mapTrunkVIDToVNI(names, params, vid)
		if err != nil {
			return err
		}
		return setupTrunkVNIEndpoint(bridge, params, vid, endpointOptions)
	}); err != nil {
		return fmt.Errorf("SetupL2TrunkVNI: %w", err)
	}
	return nil
}

// mapTrunkVIDToVNI resolves the trunk's shared devices and converges params'
// VLAN tag onto them, returning the trunk bridge so the caller can attach
// params' br-pe-<vni> endpoint to it.
func (c *VNIConfigurator) mapTrunkVIDToVNI(names TrunkDeviceNames, params L2VNIParams, vid uint16) (*netlink.Bridge, error) {
	bridgeLink, err := netlink.LinkByName(names.Bridge)
	if err != nil {
		return nil, fmt.Errorf("could not find trunk bridge %s: %w", names.Bridge, err)
	}
	bridge, ok := bridgeLink.(*netlink.Bridge)
	if !ok {
		return nil, fmt.Errorf("trunk switching device %s is not a bridge", names.Bridge)
	}
	svd, err := netlink.LinkByName(names.VXLan)
	if err != nil {
		return nil, fmt.Errorf("could not find trunk VXLAN %s: %w", names.VXLan, err)
	}
	routerVeth, err := netlink.LinkByName(names.NamespaceSide)
	if err != nil {
		return nil, fmt.Errorf("could not find trunk veth %s: %w", names.NamespaceSide, err)
	}

	snapshot, err := c.trunkSnapshotFor(routerVeth, bridge, svd, params.Trunk.TrunkID)
	if err != nil {
		return nil, err
	}
	if err := applyVlanMembership(routerVeth, vlanMembership{VID: vid},
		&snapshot.routerVethVlans); err != nil {
		return nil, err
	}
	if err := applyVlanMembership(bridge, vlanMembership{VID: vid},
		&snapshot.bridgeVlans, withBridgeInterface()); err != nil {
		return nil, err
	}
	if err := requireVlanTunnelReady(svd, vid, uint32(params.VNI)); err != nil {
		return nil, err
	}
	if err := applyVlanToVNI(svd, vid, uint32(params.VNI),
		&snapshot.svdVlans, &snapshot.svdMappings, &snapshot.svdFilterEntries); err != nil {
		return nil, err
	}
	return bridge, nil
}

// setupTrunkVNIEndpoint creates params' br-pe-<vni> endpoint on bridge and
// configures it as a gateway or an addressless endpoint, the same role the
// endpoint plays for a standalone L2VNI.
func setupTrunkVNIEndpoint(bridge *netlink.Bridge, params L2VNIParams, vid uint16, endpointOptions []NetlinkOption) error {
	endpoint, err := ensureVLANEndpoint(BridgeName(params.VNI), bridge, vid, endpointOptions...)
	if err != nil {
		return err
	}
	if len(params.L2GatewayIPs) == 0 {
		if err := removeAddresses(endpoint, netlink.FAMILY_ALL); err != nil {
			return err
		}
		if err := sysctl.Ensure(
			sysctl.DisableIPv4Forwarding(endpoint.Attrs().Name),
			sysctl.DisableIPv6Forwarding(endpoint.Attrs().Name),
		); err != nil {
			return fmt.Errorf("disabling forwarding on gateway-less endpoint %s: %w", endpoint.Attrs().Name, err)
		}
		return nil
	}
	return configureL2GatewayEndpoint(endpoint, bridge, vid, params.VRF, params.L2GatewayIPs, params.VNI)
}

func vlanIDForTag(tag int32) (uint16, error) {
	if tag < 1 || tag > int32(maxVLANID) {
		return 0, fmt.Errorf("invalid VLAN tag %d", tag)
	}
	return uint16(tag), nil
}

// RemoveAllTrunks removes every trunk device from the target namespace.
func RemoveAllTrunks(targetNS string) error {
	return RemoveNonConfiguredTrunks(targetNS, nil, nil)
}

// RemoveNonConfiguredTrunks removes trunk devices that no longer correspond to
// a configured trunk, and converges every surviving trunk to the L2VNIs that
// reference it. It never removes a trunk that still exists as a resource.
func RemoveNonConfiguredTrunks(targetNS string, trunks []L2TrunkParams, l2vnis []L2VNIParams) error {
	keepTrunkIDs := make(map[string]struct{}, len(trunks))
	for _, trunk := range trunks {
		keepTrunkIDs[TrunkID(trunk.Namespace, trunk.Name)] = struct{}{}
	}

	desired := desiredTrunkVNIs(keepTrunkIDs, l2vnis)

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("RemoveNonConfiguredTrunks: failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	var errs []error
	if err := netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("listing links: %w", err)
		}
		if err := removeTrunkDevices(keepTrunkIDs, links); err != nil {
			return err
		}
		return convergeSurvivingTrunks(desired, links)
	}); err != nil {
		errs = append(errs, err)
	}
	detachIDs, err := trunkHostPortsToDetach(keepTrunkIDs)
	if err != nil {
		errs = append(errs, err)
	}
	if len(detachIDs) > 0 {
		ovsClient := &OVSClient{}
		defer ovsClient.Close()
		if err := detachTrunkPortsFromOVSBridges(context.Background(), ovsClient, detachIDs); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("RemoveNonConfiguredTrunks: %w", err)
	}
	return nil
}

// ReclaimVNIs releases every configured VNI from any device that should not
// own it before setup. Converting an L2VNI between standalone and
// trunk-attached is a delete-and-recreate (spec.trunk is immutable), so on the
// reconcile where the new form appears the old device still holds the VNI and
// the new owner's VNI add would fail.
func ReclaimVNIs(targetNS string, l2vnis []L2VNIParams) error {
	owner, trunkVNI := vniOwners(l2vnis)
	if len(owner) == 0 {
		return nil
	}

	ns, err := netns.GetFromPath(targetNS)
	if err != nil {
		return fmt.Errorf("ReclaimVNIs: failed to get network namespace %s: %w", targetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", targetNS, "error", err)
		}
	}()

	return netnamespace.In(ns, func() error {
		links, err := netlink.LinkList()
		if err != nil {
			return fmt.Errorf("listing links in %s: %w", targetNS, err)
		}
		// Only a trunk device can own a VNI that a configured VNI needs while
		// being the wrong owner, so a node without trunks skips the per-device
		// VNI filter dumps on every reconcile.
		if !trunkVNI && !hasTrunkVXLan(links) {
			return nil
		}
		var errs []error
		for _, link := range links {
			vxlan, ok := link.(*netlink.Vxlan)
			if !ok || !vxlan.FlowBased || vxlan.VxlanId != 0 {
				continue
			}
			if err := reclaimVNIFromDevice(vxlan, owner); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	})
}

// vniOwners maps every configured VNI to the device that must own it, and
// reports whether any of them is carried by a trunk.
func vniOwners(l2vnis []L2VNIParams) (map[uint32]string, bool) {
	owner := make(map[uint32]string, len(l2vnis))
	trunkVNI := false
	for _, l2vni := range l2vnis {
		if l2vni.Trunk != nil && l2vni.Trunk.TrunkID != "" {
			owner[uint32(l2vni.VNI)] = trunkDeviceNamesFromID(l2vni.Trunk.TrunkID).VXLan
			trunkVNI = true
			continue
		}
		owner[uint32(l2vni.VNI)] = vxLanNameFromVNI(l2vni.VNI)
	}
	return owner, trunkVNI
}

// reclaimVNIFromDevice removes the VNIs the device owns that a configured VNI
// claims for another device.
func reclaimVNIFromDevice(vxlan *netlink.Vxlan, owner map[uint32]string) error {
	enabled, err := vniFilterEnabled(vxlan.Index)
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}
	entries, err := vniFilterEntries(vxlan.Index)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		expectedOwner, configured := owner[entry.Start]
		if !configured || vxlan.Name == expectedOwner {
			continue
		}
		if err := deleteVniFilterEntry(vxlan.Index, entry); err != nil {
			errs = append(errs, err)
			continue
		}
		slog.Info("reclaimed VNI from a device that no longer owns it",
			"device", vxlan.Name, "vni", entry.Start, "owner", expectedOwner)
	}
	return errors.Join(errs...)
}

// desiredTrunkVNIs returns, per surviving trunk ID, the vid -> VNI mapping the
// configured L2VNIs require.
func desiredTrunkVNIs(keepTrunkIDs map[string]struct{}, l2vnis []L2VNIParams) map[string]map[uint16]uint32 {
	desired := make(map[string]map[uint16]uint32, len(keepTrunkIDs))
	for trunkID := range keepTrunkIDs {
		desired[trunkID] = map[uint16]uint32{}
	}
	for _, l2vni := range l2vnis {
		if l2vni.Trunk == nil || l2vni.Trunk.TrunkID == "" {
			continue
		}
		if _, keep := keepTrunkIDs[l2vni.Trunk.TrunkID]; !keep {
			continue
		}
		vid, err := vlanIDForTag(l2vni.Trunk.VLAN)
		if err != nil {
			continue
		}
		desired[l2vni.Trunk.TrunkID][vid] = uint32(l2vni.VNI)
	}
	return desired
}

func removeTrunkDevices(keepTrunkIDs map[string]struct{}, links []netlink.Link) error {
	var errs []error
	for _, link := range links {
		id, owned := trunkIDOfLink(link)
		if !owned {
			continue
		}
		if _, keep := keepTrunkIDs[id]; keep {
			continue
		}
		slog.Info("deleting trunk device", "device", link.Attrs().Name)
		if err := netlink.LinkDel(link); err != nil && !errors.Is(err, unix.ENODEV) {
			errs = append(errs, fmt.Errorf("deleting trunk device %s: %w", link.Attrs().Name, err))
		}
	}
	return errors.Join(errs...)
}

func convergeSurvivingTrunks(desired map[string]map[uint16]uint32, links []netlink.Link) error {
	var errs []error
	for trunkID, mapping := range desired {
		if err := convergeTrunk(trunkID, mapping, links); err != nil {
			errs = append(errs, fmt.Errorf("converging trunk %s: %w", trunkID, err))
		}
	}
	return errors.Join(errs...)
}

func convergeTrunk(trunkID string, desired map[uint16]uint32, links []netlink.Link) error {
	names := trunkDeviceNamesFromID(trunkID)
	bridgeLink, err := netlink.LinkByName(names.Bridge)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("looking up trunk bridge %s: %w", names.Bridge, err)
	}
	bridge, ok := bridgeLink.(*netlink.Bridge)
	if !ok {
		return nil
	}
	svd, err := netlink.LinkByName(names.VXLan)
	if err != nil {
		return fmt.Errorf("looking up trunk VXLAN %s: %w", names.VXLan, err)
	}
	routerVeth, err := netlink.LinkByName(names.NamespaceSide)
	if err != nil {
		return fmt.Errorf("looking up trunk veth %s: %w", names.NamespaceSide, err)
	}

	if err := removeTrunkEndpoints(bridge, desired, links); err != nil {
		return err
	}
	if err := convergeTrunkMappings(svd, desired); err != nil {
		return err
	}

	vids := make([]uint16, 0, len(desired))
	for vid := range desired {
		vids = append(vids, vid)
	}
	slices.Sort(vids)
	if err := ensurePortVlans(routerVeth, newTrunk(vids)); err != nil {
		return err
	}
	return ensurePortVlans(bridge, newTrunk(vids), withBridgeInterface())
}

func convergeTrunkMappings(svd netlink.Link, desired map[uint16]uint32) error {
	mappings, err := vxlanTunnelMappings(svd.Attrs().Index)
	if err != nil {
		return err
	}
	for _, mapping := range mappings {
		if expected, ok := desired[mapping.Vid]; ok && expected == mapping.TunId {
			continue
		}
		if err := netlink.BridgeVlanDelTunnelInfo(svd, mapping.Vid, mapping.TunId, false, false); err != nil {
			return fmt.Errorf("removing stale VLAN %d tunnel mapping from %s: %w", mapping.Vid, svd.Attrs().Name, err)
		}
	}

	desiredVNIs := make(map[uint32]struct{}, len(desired))
	for _, vni := range desired {
		desiredVNIs[vni] = struct{}{}
	}
	entries, err := vniFilterEntries(svd.Attrs().Index)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, keep := desiredVNIs[entry.Start]; keep {
			continue
		}
		if err := deleteVniFilterEntry(svd.Attrs().Index, entry); err != nil {
			return err
		}
	}
	return nil
}

// removeTrunkEndpoints deletes br-pe-<vni> endpoints parented on the trunk
// bridge whose VNI no longer belongs to this trunk, removing their local FDB
// entries first.
func removeTrunkEndpoints(bridge *netlink.Bridge, desired map[uint16]uint32, links []netlink.Link) error {
	desiredVNIs := map[uint32]struct{}{}
	for _, vni := range desired {
		desiredVNIs[vni] = struct{}{}
	}

	var errs []error
	for _, link := range links {
		endpoint, ok := link.(*netlink.Vlan)
		if !ok || endpoint.ParentIndex != bridge.Index {
			continue
		}
		vni, err := vniFromBridgeName(endpoint.Name)
		if err != nil {
			continue
		}
		if _, keep := desiredVNIs[uint32(vni)]; keep {
			continue
		}
		mac, err := BridgeFixedMAC(vni)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := removeLocalFDBEntry(bridge, uint16(endpoint.VlanId), mac); err != nil {
			errs = append(errs, err)
		}
		if err := netlink.LinkDel(endpoint); err != nil && !errors.Is(err, unix.ENODEV) {
			errs = append(errs, fmt.Errorf("deleting trunk endpoint %s: %w", endpoint.Name, err))
		}
	}
	return errors.Join(errs...)
}

func trunkIDOfLink(link netlink.Link) (string, bool) {
	name := link.Attrs().Name
	switch link.Type() {
	case VethLinkType:
		if id, err := trunkIDFromHostName(name); err == nil {
			return id, true
		}
		if id, err := trunkIDFromPEName(name); err == nil {
			return id, true
		}
	case netlinkTypeFor(BridgeLinkType):
		if id, err := trunkIDFromBridgeName(name); err == nil {
			return id, true
		}
	case VXLanLinkType:
		if id, err := trunkIDFromVXLanName(name); err == nil {
			return id, true
		}
	}
	return "", false
}

// hasTrunkVXLan reports whether any trunk VXLAN device exists.
func hasTrunkVXLan(links []netlink.Link) bool {
	for _, link := range links {
		if _, err := trunkIDFromVXLanName(link.Attrs().Name); err == nil {
			return true
		}
	}
	return false
}

// trunkHostPortsToDetach returns the trunk ids whose host leg exists and is no
// longer configured. It lets the OVS cleanup run only when a trunk was actually
// removed: a node without trunks pays no OVS round trip on every reconcile.
func trunkHostPortsToDetach(keepTrunkIDs map[string]struct{}) (map[string]struct{}, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("listing host links: %w", err)
	}
	ids := map[string]struct{}{}
	for _, link := range links {
		id, err := trunkIDFromHostName(link.Attrs().Name)
		if err != nil {
			continue
		}
		if _, keep := keepTrunkIDs[id]; keep {
			continue
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}

// detachTrunkPortsFromOVSBridges detaches the host leg of a removed trunk from
// an External OVS bridge, leaving the bridge itself in place.
func detachTrunkPortsFromOVSBridges(ctx context.Context, ovsClient *OVSClient, trunkIDs map[string]struct{}) error {
	ovs, err := ovsClient.Get(ctx)
	if err != nil {
		return nil
	}
	var bridges []ovsmodel.Bridge
	if err := ovs.List(ctx, &bridges); err != nil {
		return fmt.Errorf("failed to list OVS bridges: %w", err)
	}
	var errs []error
	for _, bridge := range bridges {
		if bridge.ExternalIDs["created-by"] == "openperouter" {
			continue
		}
		filter := func(port *ovsmodel.Port) bool {
			id, err := trunkIDFromHostName(port.Name)
			if err != nil {
				return false
			}
			_, detach := trunkIDs[id]
			return detach
		}
		detached, err := detachPortsFromBridge(ctx, ovs, bridge, filter)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if detached {
			slog.Info("detaching trunk host leg from OVS bridge", "bridge", bridge.Name)
		}
	}
	return errors.Join(errs...)
}

// ensureTrunkVeth creates the trunk veth pair. Existence is keyed on the
// router-side leg: the host-side leg can survive a namespace rebuild as an
// orphan, and checking it would skip recreation.
func ensureTrunkVeth(ctx context.Context, names TrunkDeviceNames, ns netns.NsHandle) error {
	routerSideExists := false
	err := netnamespace.In(ns, func() error {
		_, err := netlink.LinkByName(names.NamespaceSide)
		if err == nil {
			routerSideExists = true
			return nil
		}
		if errors.As(err, &netlink.LinkNotFoundError{}) {
			return nil
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("looking up trunk veth %s: %w", names.NamespaceSide, err)
	}
	if routerSideExists {
		return nil
	}

	if hostLeg, err := netlink.LinkByName(names.HostSide); err == nil {
		if err := netlink.LinkDel(hostLeg); err != nil && !errors.Is(err, unix.ENODEV) {
			return fmt.Errorf("deleting stale host leg %s: %w", names.HostSide, err)
		}
	}

	return setupNamespacedVethInNamespace(ctx, VethNames{
		HostSide:      names.HostSide,
		NamespaceSide: names.NamespaceSide,
	}, ns)
}

// ensureLinkAlias records <namespace>/<name> on a device so an operator can map
// a hashed device name back to its trunk during triage.
func ensureLinkAlias(link netlink.Link, namespace, name string) error {
	alias := namespace + "/" + name
	if link.Attrs().Alias == alias {
		return nil
	}
	if err := netlink.LinkSetAlias(link, alias); err != nil {
		return fmt.Errorf("setting alias %q on %s: %w", alias, link.Attrs().Name, err)
	}
	return nil
}
