// SPDX-License-Identifier:Apache-2.0

package hostnetwork

// This file describes the VLAN-aware device model shared by every VNI.
//
// Each L2VNI and L3VNI is backed by four devices:
//
//   - br-vl-<vni>, a private VLAN-filtering bridge with no default PVID. It
//     switches the VNI's ports and carries no addresses or VRF membership.
//   - vni<vni>, an external (collect-metadata) VXLAN device enslaved to that
//     bridge, with VNI filtering enabled and the internal VLAN mapped to the
//     VNI, so several VNIs can share one UDP port.
//   - br-pe-<vni>, a VLAN subinterface parented on the bridge. It keeps the
//     name downstream code knows and is the SVI zebra discovers; it carries the
//     gateway addresses, anycast MAC and VRF membership.
//   - the router-side veth, an untagged access port on the internal VLAN.
//
// VLAN memberships are converged by value: a vlanMembership carries the VID
// and its pvid/untagged flags, and ensurePortVlans removes every VID that is
// not desired before adding or rewriting the wanted ones.
//
// The kernel-touching helpers are stubs until the follow-up commits implement
// them; each stub names the work it owes.

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

const (
	// internalVLANID is the private VLAN that carries an L2VNI or L3VNI on its
	// dedicated switching bridge. It never leaves the node.
	internalVLANID uint16 = 1
	// maxVLANID is the highest usable 802.1Q VLAN ID.
	maxVLANID = 4094

	switchingBridgePrefix = "br-vl-"
)

// vlanMembership is the VLAN state of one bridge port, either as desired or as
// observed in the kernel. Trunk ports only ever desire tagged memberships;
// access ports set PVID and Untagged.
type vlanMembership struct {
	VID      uint16
	PVID     bool
	Untagged bool
}

// svdParams describes an external vnifilter VXLAN device servicing one bridge.
type svdParams struct {
	Name            string
	Bridge          *netlink.Bridge
	VTEPIP          string
	Port            int32
	VtepDeviceIndex int
	HardwareAddr    net.HardwareAddr
}

// ensureSwitchingBridge creates or converges the private VLAN-filtering bridge
// that switches a VNI or a trunk. Filtering and a zero default PVID are set
// before any port is enslaved, so no port ever inherits an implicit VLAN.
func ensureSwitchingBridge(name string) (*netlink.Bridge, error) {
	// TODO(vlan-aware): create br-vl-<vni> if missing, enable VLAN filtering,
	// disable the default PVID, clear its addresses and bring it up.
	return nil, fmt.Errorf("ensureSwitchingBridge(%s): not implemented", name)
}

// newAccessPort returns the desired membership of an access port: vid is the
// untagged PVID, so untagged frames are admitted and tagged frames dropped.
func newAccessPort(vid uint16) []vlanMembership {
	return []vlanMembership{{VID: vid, PVID: true, Untagged: true}}
}

// newTrunk returns the desired memberships of a trunk port: every vid is
// tagged and no PVID is configured, so untagged frames are dropped.
func newTrunk(vids []uint16) []vlanMembership {
	memberships := make([]vlanMembership, 0, len(vids))
	for _, vid := range vids {
		memberships = append(memberships, vlanMembership{VID: vid})
	}
	return memberships
}

// ensurePortVlans converges one port to exactly desired. It removes VIDs that
// are not desired and lets applyVlanMembership rewrite entries whose
// pvid/untagged flags differ, so a stale VID 1 inherited from a bridge created
// with the default PVID cannot admit untagged traffic into a tenant domain.
// options are forwarded to the membership helpers.
func ensurePortVlans(link netlink.Link, desired []vlanMembership, options ...vlanMembershipOption) error {
	current, err := portVlanMemberships(link)
	if err != nil {
		return err
	}
	desiredByVID := make(map[uint16]vlanMembership, len(desired))
	for _, membership := range desired {
		desiredByVID[membership.VID] = membership
	}

	target := resolveVlanMembershipOptions(options...)
	for _, observed := range current {
		if _, ok := desiredByVID[observed.VID]; ok {
			continue
		}
		if err := deleteVlanMembership(link, observed.VID, &current, target); err != nil {
			return err
		}
	}
	for _, wanted := range desired {
		if err := applyVlanMembership(link, wanted, &current, options...); err != nil {
			return err
		}
	}
	return nil
}

// vlanPortOptions selects which VLAN table on a link is written.
type vlanPortOptions struct {
	bridgeInterface bool
}

// vlanMembershipOption configures which VLAN table a membership is written to.
type vlanMembershipOption func(*vlanPortOptions)

// withBridgeInterface targets the bridge interface itself (BRIDGE_FLAGS_SELF)
// instead of an enslaved port. Without an entry on the bridge interface, the
// bridge does not pass the VLAN to it.
func withBridgeInterface() vlanMembershipOption {
	return func(target *vlanPortOptions) {
		target.bridgeInterface = true
	}
}

func resolveVlanMembershipOptions(options ...vlanMembershipOption) vlanPortOptions {
	target := vlanPortOptions{}
	for _, option := range options {
		option(&target)
	}
	return target
}

// applyVlanMembership converges one VID on a port to desired without touching
// other VIDs, and records the result in current. Shared trunk devices use it to
// add a single L2VNI's tag without removing other tenants' tags. It converges
// against current, the caller's own snapshot of the port's memberships, and
// updates it in place instead of re-reading it: SetupL2TrunkVNI reuses one
// snapshot across every L2VNI on a shared trunk so a 20-tag trunk reads the
// port's VLAN table once per reconcile instead of once per tag.
func applyVlanMembership(link netlink.Link, desired vlanMembership, current *[]vlanMembership,
	options ...vlanMembershipOption) error {
	// TODO(vlan-aware): rewrite an existing membership whose flags differ, then
	// add the desired membership with BridgeVlanAdd and record it in current.
	return fmt.Errorf("applyVlanMembership(%s, %d): not implemented", link.Attrs().Name, desired.VID)
}

// deleteVlanMembership removes vid from the port's VLAN table, if present, and
// records the removal in current.
func deleteVlanMembership(link netlink.Link, vid uint16, current *[]vlanMembership, target vlanPortOptions) error {
	// TODO(vlan-aware): delete the membership with BridgeVlanDel and drop it
	// from current.
	return fmt.Errorf("deleteVlanMembership(%s, %d): not implemented", link.Attrs().Name, vid)
}

func indexOfVlanMembership(memberships []vlanMembership, vid uint16) (vlanMembership, bool) {
	for _, membership := range memberships {
		if membership.VID == vid {
			return membership, true
		}
	}
	return vlanMembership{}, false
}

func withoutVlanMembership(memberships []vlanMembership, vid uint16) []vlanMembership {
	result := make([]vlanMembership, 0, len(memberships))
	for _, membership := range memberships {
		if membership.VID != vid {
			result = append(result, membership)
		}
	}
	return result
}

func portVlanMemberships(link netlink.Link) ([]vlanMembership, error) {
	// TODO(vlan-aware): list the bridge VLAN table and expand the ranges.
	return nil, fmt.Errorf("portVlanMemberships(%s): not implemented", link.Attrs().Name)
}

func expandVlanMemberships(entries []*nl.BridgeVlanInfo) []vlanMembership {
	var result []vlanMembership
	var rangeStart uint16
	for _, entry := range entries {
		if entry.Flags&nl.BRIDGE_VLAN_INFO_RANGE_BEGIN != 0 {
			rangeStart = entry.Vid
			continue
		}
		if entry.Flags&nl.BRIDGE_VLAN_INFO_RANGE_END != 0 {
			for vid := rangeStart; vid <= entry.Vid; vid++ {
				result = append(result, vlanMembership{VID: vid, PVID: entry.PortVID(), Untagged: entry.EngressUntag()})
			}
			continue
		}
		result = append(result, vlanMembership{VID: entry.Vid, PVID: entry.PortVID(), Untagged: entry.EngressUntag()})
	}
	return result
}

// ensureVLANEndpoint creates or converges the VLAN subinterface that acts as
// the L2VNI or L3VNI SVI. zebra only discovers a VLAN device parented on the
// bridge as the VNI's SVI, so the endpoint must always be a VLAN device. A new
// endpoint starts with the parent bridge's MAC, and options converge the MAC or
// any other attribute zebra cares about before the endpoint is brought up.
func ensureVLANEndpoint(name string, parent *netlink.Bridge, vid uint16, options ...NetlinkOption) (netlink.Link, error) {
	// TODO(vlan-aware): create or converge the VLAN device, apply the options
	// before bringing it up, then return the live link.
	return nil, fmt.Errorf("ensureVLANEndpoint(%s): not implemented", name)
}

func matchesVLANEndpoint(link netlink.Link, parentIndex int, vid uint16) bool {
	existing, ok := link.(*netlink.Vlan)
	return ok && existing.ParentIndex == parentIndex && existing.VlanId == int(vid)
}

// removeVLANEndpoint deletes the named VLAN endpoint if it exists.
func removeVLANEndpoint(name string) error {
	// TODO(vlan-aware): look the endpoint up and delete it, tolerating absence.
	return fmt.Errorf("removeVLANEndpoint(%s): not implemented", name)
}

// ensureSVD creates or converges the external vnifilter VXLAN device for a
// bridge. VNI filtering is mandatory: a plain external device permits only one
// device per UDP port per network namespace, so several VNIs could not coexist.
func ensureSVD(params svdParams) (*netlink.Vxlan, error) {
	// TODO(vlan-aware): create the external device through the raw netlink
	// helper and converge its bridge-port properties.
	return nil, fmt.Errorf("ensureSVD(%s): not implemented", params.Name)
}

// reconcileSVDParams applies the bridge-port properties the VXLAN needs and
// brings it up. It reads the current state first so an unchanged device is not
// rewritten and FRR does not flush its neighbors.
func reconcileSVDParams(vxlan *netlink.Vxlan, bridge *netlink.Bridge, mac net.HardwareAddr) (*netlink.Vxlan, error) {
	// TODO(vlan-aware): enslave, enable VLAN tunnel mode, neighbor suppression
	// and MAC convergence before bringing the device up.
	return nil, fmt.Errorf("reconcileSVDParams(%s): not implemented", vxlan.Name)
}

// ensureVlanTunnelPort enslaves the VXLAN to its bridge and enables VLAN
// tunnel mode, which the VLAN-to-VNI mapping requires.
func ensureVlanTunnelPort(vxlan netlink.Link) error {
	// TODO(vlan-aware): turn vlan_tunnel on when the bridge port lacks it.
	return fmt.Errorf("ensureVlanTunnelPort(%s): not implemented", vxlan.Attrs().Name)
}

// ensureVlanToVNI maps vid to vni on an enslaved VLAN-tunnel VXLAN. The
// programming order is enforced by the kernel: enslavement, vlan_tunnel on,
// plain VLAN membership, tunnel_info, then the VNI filter entry. Steps 2-4 fail
// EINVAL out of order.
func ensureVlanToVNI(vxlan netlink.Link, vid uint16, vni uint32) error {
	// TODO(vlan-aware): converge the VLAN membership, the tunnel_info mapping
	// and the VNI filter entry in kernel order.
	return fmt.Errorf("ensureVlanToVNI(%s, %d, %d): not implemented", vxlan.Attrs().Name, vid, vni)
}

// removeVlanToVNI removes a VLAN-to-VNI mapping and its VNI filter entry. The
// VLAN membership itself is left to the port-convergence helpers, which know
// the full desired set.
func removeVlanToVNI(vxlan netlink.Link, vid uint16, vni uint32) error {
	// TODO(vlan-aware): delete the tunnel_info mapping and, once no mapping
	// uses the VNI, its VNI filter entry.
	return fmt.Errorf("removeVlanToVNI(%s, %d, %d): not implemented", vxlan.Attrs().Name, vid, vni)
}

// ensureLocalFDBEntry installs the anycast MAC as a local FDB entry on the
// switching bridge's CPU port, so gateway traffic is not flooded onto the
// overlay and back.
func ensureLocalFDBEntry(bridge *netlink.Bridge, vid uint16, mac net.HardwareAddr) error {
	// TODO(vlan-aware): install the permanent NTF_SELF entry unless present.
	return fmt.Errorf("ensureLocalFDBEntry(%s, %d): not implemented", bridge.Name, vid)
}

// removeLocalFDBEntry deletes the local FDB entry installed for an anycast MAC.
func removeLocalFDBEntry(bridge *netlink.Bridge, vid uint16, mac net.HardwareAddr) error {
	// TODO(vlan-aware): delete every matching permanent local entry.
	return fmt.Errorf("removeLocalFDBEntry(%s, %d): not implemented", bridge.Name, vid)
}

// switchingBridgeName returns the private switching bridge backing a VNI.
func switchingBridgeName(vni int32) string {
	return fmt.Sprintf("%s%d", switchingBridgePrefix, vni)
}

func vniFromSwitchingBridgeName(name string) (int32, error) {
	if !strings.HasPrefix(name, switchingBridgePrefix) {
		return 0, NotRouterInterfaceError{Name: name}
	}
	vni, err := strconv.ParseInt(strings.TrimPrefix(name, switchingBridgePrefix), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for bridge %s", name)
	}
	return int32(vni), nil
}
