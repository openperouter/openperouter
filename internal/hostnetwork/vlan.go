// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

const internalVLANID uint16 = 1

func setupVNIEndpoint(params VNIParams, bridge *netlink.Bridge) (netlink.Link, error) {
	if err := removeGatewayEndpoint(params.VNI, bridge); err != nil {
		return nil, err
	}
	return setupVLANEndpoint(params, bridge, BridgeName(params.VNI))
}

func setupL2Gateway(params L2VNIParams, bridge *netlink.Bridge) (netlink.Link, error) {
	if err := removeGatewaySVI(params.VNI); err != nil {
		return nil, err
	}
	mac, err := BridgeFixedMAC(params.VNI)
	if err != nil {
		return nil, err
	}
	// FRR learns EVPN neighbors on the VLAN SVI. Keeping gateway addresses on
	// that same interface lets local ARP/ND replace remote neighbors after a move.
	link, err := setupVLANInterface(params.VNIParams, bridge, BridgeName(params.VNI), mac)
	if err != nil {
		return nil, err
	}
	if err := ensureGatewayLinkLocal(link, bridge.HardwareAddr); err != nil {
		return nil, err
	}
	for _, ip := range params.L2GatewayIPs {
		if err := AssignIPToInterface(link, ip); err != nil {
			return nil, err
		}
	}
	if err := ensureGatewayFDB(bridge, mac); err != nil {
		return nil, err
	}
	return netlink.LinkByIndex(link.Attrs().Index)
}

func ensureAccessVLAN(link netlink.Link) error {
	return ensureVLANMembership(link, true, false)
}

func removeGatewayEndpoint(vni int32, bridge *netlink.Bridge) error {
	if err := removeGatewaySVI(vni); err != nil {
		return err
	}
	mac, err := BridgeFixedMAC(vni)
	if err != nil {
		return err
	}
	entries, err := netlink.NeighList(bridge.Index, unix.AF_BRIDGE)
	if err != nil {
		return fmt.Errorf("list old gateway FDB on %s: %w", bridge.Name, err)
	}
	for _, entry := range entries {
		if entry.Vlan != int(internalVLANID) || !bytes.Equal(entry.HardwareAddr, mac) {
			continue
		}
		entry.Flags = netlink.NTF_SELF
		if err := netlink.NeighDel(&entry); err != nil {
			return fmt.Errorf("remove old gateway FDB on %s: %w", bridge.Name, err)
		}
	}
	return nil
}

func removeGatewaySVI(vni int32) error {
	name := vlanSVIName(vni)
	svi, err := netlink.LinkByName(name)
	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) {
		return fmt.Errorf("look up gateway SVI %s: %w", name, err)
	}
	if svi != nil {
		// Removing the backing SVI also frees its macvlan child's gateway name.
		if err := netlink.LinkDel(svi); err != nil {
			return fmt.Errorf("remove gateway SVI %s: %w", name, err)
		}
	}
	return nil
}

func setupVLANEndpoint(params VNIParams, bridge *netlink.Bridge, name string) (netlink.Link, error) {
	link, err := setupVLANInterface(params, bridge, name, bridge.HardwareAddr)
	if err != nil {
		return nil, err
	}
	if err := ensureAddressless(link); err != nil {
		return nil, err
	}
	return link, nil
}

func setupVLANInterface(params VNIParams, bridge *netlink.Bridge, name string, mac net.HardwareAddr) (netlink.Link, error) {
	currentBridge, err := netlink.LinkByIndex(bridge.Index)
	if err != nil {
		return nil, fmt.Errorf("look up switching bridge %s: %w", bridge.Name, err)
	}
	attrs := currentBridge.Attrs()
	desired := &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{
			Name: name, ParentIndex: bridge.Index, MTU: attrs.MTU, HardwareAddr: mac,
		},
		VlanId: int(internalVLANID),
	}
	link, err := ownedEndpoint(desired)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(link.Attrs().HardwareAddr, mac) {
		if err := netlink.LinkSetHardwareAddr(link, mac); err != nil {
			return nil, fmt.Errorf("set endpoint MAC on %s: %w", name, err)
		}
	}
	if err := linkSetMTU(link, attrs.MTU); err != nil {
		return nil, fmt.Errorf("set endpoint %s MTU: %w", name, err)
	}
	if err := endpointVRF(link, params.VRF); err != nil {
		return nil, err
	}
	if err := suppressEndpointLinkLocal(link); err != nil {
		return nil, err
	}
	if err := linkSetUp(link); err != nil {
		return nil, fmt.Errorf("set endpoint %s up: %w", name, err)
	}
	return netlink.LinkByIndex(link.Attrs().Index)
}

func createVLANBridge(name string) (*netlink.Bridge, error) {
	link, err := netlink.LinkByName(name)
	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil, fmt.Errorf("look up switching bridge %s: %w", name, err)
	}
	if bridge, ok := link.(*netlink.Bridge); ok {
		return bridge, nil
	}
	if link != nil {
		if err := netlink.LinkDel(link); err != nil {
			return nil, fmt.Errorf("delete mismatched switching bridge %s: %w", name, err)
		}
	}
	mac := make(net.HardwareAddr, macSize)
	if _, err := rand.Read(mac); err != nil {
		return nil, fmt.Errorf("generate router MAC for %s: %w", name, err)
	}
	mac[0] = (mac[0] | 2) & 0xfe
	// An explicit MAC prevents bridge port additions from changing the router identity.
	bridge := &netlink.Bridge{
		LinkAttrs:     netlink.LinkAttrs{Name: name, HardwareAddr: mac, TxQLen: bridgeQueueLength},
		VlanFiltering: new(true), VlanDefaultPVID: new(uint16(0)),
	}
	if err := netlink.LinkAdd(bridge); err != nil {
		return nil, fmt.Errorf("create switching bridge %s: %w", name, err)
	}
	current, err := netlink.LinkByIndex(bridge.Index)
	if err != nil {
		return nil, fmt.Errorf("look up new switching bridge %s: %w", name, err)
	}
	return current.(*netlink.Bridge), nil
}

func ownedEndpoint(desired netlink.Link) (netlink.Link, error) {
	name := desired.Attrs().Name
	link, err := netlink.LinkByName(name)
	if err != nil && !errors.As(err, &netlink.LinkNotFoundError{}) {
		return nil, fmt.Errorf("look up endpoint %s: %w", name, err)
	}
	if link != nil && endpointMatches(link, desired) {
		return link, nil
	}
	if link != nil {
		if err := netlink.LinkDel(link); err != nil {
			return nil, fmt.Errorf("delete mismatched endpoint %s: %w", name, err)
		}
	}
	if err := netlink.LinkAdd(desired); err != nil {
		return nil, fmt.Errorf("create endpoint %s: %w", name, err)
	}
	return desired, nil
}

func endpointMatches(link, desired netlink.Link) bool {
	if link.Attrs().ParentIndex != desired.Attrs().ParentIndex {
		return false
	}
	switch want := desired.(type) {
	case *netlink.Vlan:
		observed, ok := link.(*netlink.Vlan)
		return ok && observed.VlanId == want.VlanId
	case *netlink.Macvlan:
		observed, ok := link.(*netlink.Macvlan)
		return ok && observed.Mode == want.Mode
	}
	return false
}

func endpointVRF(link netlink.Link, name string) error {
	if name == "" {
		if link.Attrs().MasterIndex == 0 {
			return nil
		}
		if err := netlink.LinkSetNoMaster(link); err != nil {
			return fmt.Errorf("detach endpoint %s from VRF: %w", link.Attrs().Name, err)
		}
		return nil
	}
	vrf, err := lookupVRF(name)
	if err != nil {
		return fmt.Errorf("find endpoint %s VRF %s: %w", link.Attrs().Name, name, err)
	}
	if err := linkSetMaster(link, vrf); err != nil {
		return fmt.Errorf("bind endpoint %s to VRF %s: %w", link.Attrs().Name, name, err)
	}
	return nil
}

func ensureAddressless(link netlink.Link) error {
	if err := suppressEndpointLinkLocal(link); err != nil {
		return err
	}
	addresses, err := listAddresses(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("list addressless interface %s addresses: %w", link.Attrs().Name, err)
	}
	for _, address := range addresses {
		if err := netlink.AddrDel(link, &address); err != nil {
			return fmt.Errorf("remove address from %s: %w", link.Attrs().Name, err)
		}
	}
	return nil
}

func suppressEndpointLinkLocal(link netlink.Link) error {
	// Gateway SVIs use a shared anycast MAC, so their link-local address must be
	// assigned explicitly from a node-specific identity.
	path := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/addr_gen_mode", link.Attrs().Name)
	mode, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read IPv6 address generation mode for %s: %w", link.Attrs().Name, err)
	}
	if strings.TrimSpace(string(mode)) == "1" {
		return nil
	}
	if err := SuppressLinkLocal(link.Attrs().Name); err != nil {
		return err
	}
	return nil
}

func ensureVLANMembership(link netlink.Link, access, self bool) error {
	memberships, err := netlink.BridgeVlanList()
	if err != nil {
		return fmt.Errorf("list VLAN memberships for %s: %w", link.Attrs().Name, err)
	}
	desiredPresent := false
	for _, entry := range expandedVLANMemberships(memberships[int32(link.Attrs().Index)]) {
		if entry.Vid != internalVLANID {
			if err := netlink.BridgeVlanDel(link, entry.Vid, false, false, self, false); err != nil {
				return fmt.Errorf("remove unexpected VLAN %d from %s: %w", entry.Vid, link.Attrs().Name, err)
			}
			continue
		}
		desiredPresent = entry.PortVID() == access && entry.EngressUntag() == access
	}
	if desiredPresent {
		return nil
	}
	if err := netlink.BridgeVlanAdd(link, internalVLANID, access, access, self, false); err != nil {
		return fmt.Errorf("set VLAN %d membership on %s: %w", internalVLANID, link.Attrs().Name, err)
	}
	return nil
}

func expandedVLANMemberships(entries []*nl.BridgeVlanInfo) []nl.BridgeVlanInfo {
	var result []nl.BridgeVlanInfo
	var rangeStart uint16
	for _, entry := range entries {
		if entry.Flags&nl.BRIDGE_VLAN_INFO_RANGE_BEGIN != 0 {
			rangeStart = entry.Vid
			continue
		}
		if entry.Flags&nl.BRIDGE_VLAN_INFO_RANGE_END != 0 {
			for vid := rangeStart; vid <= entry.Vid; vid++ {
				result = append(result, nl.BridgeVlanInfo{Vid: vid, Flags: entry.Flags})
			}
			continue
		}
		result = append(result, *entry)
	}
	return result
}

func ensureGatewayFDB(bridge *netlink.Bridge, mac net.HardwareAddr) error {
	entries, err := netlink.NeighList(bridge.Index, unix.AF_BRIDGE)
	if err != nil {
		return fmt.Errorf("list gateway FDB on %s: %w", bridge.Name, err)
	}
	for _, entry := range entries {
		if entry.Vlan == int(internalVLANID) && bytes.Equal(entry.HardwareAddr, mac) &&
			entry.State&netlink.NUD_PERMANENT != 0 {
			return nil
		}
	}
	entry := &netlink.Neigh{
		LinkIndex: bridge.Index, Family: unix.AF_BRIDGE, HardwareAddr: mac,
		Vlan: int(internalVLANID), State: netlink.NUD_PERMANENT, Flags: netlink.NTF_SELF,
	}
	if err := netlink.NeighSet(entry); err != nil {
		return fmt.Errorf("set local gateway FDB on %s: %w", bridge.Name, err)
	}
	return nil
}

func ensureGatewayLinkLocal(link netlink.Link, mac net.HardwareAddr) error {
	if len(mac) != macSize {
		return fmt.Errorf("derive gateway %s link-local address: invalid MAC %s", link.Attrs().Name, mac)
	}
	// Timer-driven IPv6 neighbor probes require a usable link-local source. Use
	// the private bridge's node-specific MAC rather than the shared gateway MAC.
	ip := net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, mac[0] ^ 2, mac[1], mac[2], 0xff, 0xfe, mac[3], mac[4], mac[5]}
	desired := netlink.Addr{
		IPNet: &net.IPNet{IP: ip, Mask: net.CIDRMask(64, 128)},
		Scope: unix.RT_SCOPE_LINK, Flags: unix.IFA_F_NODAD,
	}
	addresses, err := listAddresses(link, netlink.FAMILY_V6)
	if err != nil {
		return fmt.Errorf("list gateway %s IPv6 addresses: %w", link.Attrs().Name, err)
	}
	for _, address := range addresses {
		if !address.IP.Equal(ip) {
			continue
		}
		if bytes.Equal(address.Mask, desired.Mask) && address.Scope == desired.Scope &&
			address.Flags&(unix.IFA_F_TENTATIVE|unix.IFA_F_DADFAILED|unix.IFA_F_OPTIMISTIC) == 0 {
			return nil
		}
		if err := netlink.AddrDel(link, &address); err != nil {
			return fmt.Errorf("remove unusable gateway %s link-local address: %w", link.Attrs().Name, err)
		}
	}
	if err := netlink.AddrAdd(link, &desired); err != nil {
		return fmt.Errorf("assign gateway %s link-local address: %w", link.Attrs().Name, err)
	}
	return nil
}

func vlanBridgeName(vni int32) string {
	return fmt.Sprintf("br-vl-%d", vni)
}

func vlanSVIName(vni int32) string {
	return fmt.Sprintf("svi%d", vni)
}
