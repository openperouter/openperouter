// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// This file is the single place that speaks raw netlink. The pinned netlink
// release (v1.3.1) is missing exactly two things the VLAN-aware dataplane
// needs: Vxlan.VniFilter on create, and the RTM_*TUNNEL API that backs
// `bridge vni add|del|show` and the per-device VNI filter entries. Everything
// else in the package uses the typed API.
//
// Linux's tunnel_msg is eight bytes (family, flags, reserved, ifindex),
// followed by nested VXLAN_VNIFILTER_ENTRY attributes. The kernel enforces
// that VNI filtering is only valid on a collect-metadata (external) device.
const (
	netlinkAttributeTypeMask = 0x3fff
	tunnelMessageHeaderSize  = 8

	// VXLAN_VNIFILTER_ENTRY and its fields are not exported by x/sys.
	vniFilterEntryAttr = 1
	vniFilterStartAttr = 1
	vniFilterEndAttr   = 2

	maxVXLANVNI = 1<<24 - 1
)

type vniFilterRange struct {
	Start uint32
	End   uint32
}

func (r vniFilterRange) contains(vni uint32) bool {
	return vni >= r.Start && vni <= r.End
}

// createVniFilterVxlan creates an external vnifilter VXLAN device. The typed
// LinkAdd path cannot express IFLA_VXLAN_VNIFILTER, so the request is
// serialized here.
func createVniFilterVxlan(vxlan *netlink.Vxlan) (*netlink.Vxlan, error) {
	if !vxlan.FlowBased {
		return nil, fmt.Errorf("creating VXLAN %s with VNI filtering: VNI filtering requires collect-metadata (external) mode",
			vxlan.Name)
	}
	if _, err := vniFilterVxlanRequest(vxlan).Execute(unix.NETLINK_ROUTE, 0); err != nil {
		return nil, fmt.Errorf("creating external VXLAN %s with VNI filtering: %w", vxlan.Name, err)
	}
	link, err := netlink.LinkByName(vxlan.Name)
	if err != nil {
		return nil, fmt.Errorf("looking up created VXLAN %s: %w", vxlan.Name, err)
	}
	result, ok := link.(*netlink.Vxlan)
	if !ok {
		return nil, fmt.Errorf("created link %s is %s, expected VXLAN", vxlan.Name, link.Type())
	}
	return result, nil
}

// vniFilterEnabled reports whether the VXLAN device has VNI filtering enabled.
func vniFilterEnabled(index int) (bool, error) {
	req := nl.NewNetlinkRequest(unix.RTM_GETLINK, 0)
	msg := nl.NewIfInfomsg(unix.AF_UNSPEC)
	msg.Index = int32(index)
	req.AddData(msg)
	messages, err := req.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWLINK)
	if err != nil {
		return false, fmt.Errorf("querying VXLAN %d VNI filter flag: %w", index, err)
	}
	for _, message := range messages {
		if len(message) < unix.SizeofIfInfomsg {
			return false, errors.New("truncated VXLAN link reply")
		}
		if nl.DeserializeIfInfomsg(message).Index != int32(index) {
			continue
		}
		return parseVniFilterFlag(message[unix.SizeofIfInfomsg:])
	}
	return false, fmt.Errorf("missing VXLAN %d link reply", index)
}

// vniFilterEntries returns the VNI filter entries configured on the device.
func vniFilterEntries(index int) ([]vniFilterRange, error) {
	req := nl.NewNetlinkRequest(unix.RTM_GETTUNNEL, unix.NLM_F_DUMP)
	req.AddRawData(tunnelMessageHeader(index))
	messages, err := req.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWTUNNEL)
	if err != nil {
		return nil, fmt.Errorf("listing VXLAN %d VNI filters: %w", index, err)
	}
	var entries []vniFilterRange
	for _, message := range messages {
		parsed, err := parseVniFilterEntries(message, index)
		if err != nil {
			return nil, fmt.Errorf("reading VXLAN %d VNI filters: %w", index, err)
		}
		entries = append(entries, parsed...)
	}
	return entries, nil
}

// setVniFilterEntries converges the device's VNI filter entries to desired.
// Extra entries are deleted and missing ones added, so a device never keeps a
// VNI that no configured resource owns.
func setVniFilterEntries(index int, desired []vniFilterRange) error {
	existing, err := vniFilterEntries(index)
	if err != nil {
		return err
	}

	for _, entry := range existing {
		if containsVniFilterRange(desired, entry) {
			continue
		}
		if _, err := vniFilterRequest(unix.RTM_DELTUNNEL, index, entry).Execute(unix.NETLINK_ROUTE, 0); err != nil {
			return fmt.Errorf("deleting VXLAN %d VNI filter %d-%d: %w", index, entry.Start, entry.End, err)
		}
	}

	for _, entry := range desired {
		if containsVniFilterRange(existing, entry) {
			continue
		}
		if _, err := vniFilterRequest(unix.RTM_NEWTUNNEL, index, entry).Execute(unix.NETLINK_ROUTE, 0); err != nil {
			return fmt.Errorf("adding VXLAN %d VNI filter %d-%d: %w", index, entry.Start, entry.End, err)
		}
	}
	return nil
}

// addVniFilterEntry adds a single VNI to the device's filter table, the
// equivalent of `bridge vni add dev <device> vni <vni>`.
func addVniFilterEntry(index int, vni uint32) error {
	entry := vniFilterRange{Start: vni, End: vni}
	existing, err := vniFilterEntries(index)
	if err != nil {
		return err
	}
	if containsVniFilterRange(existing, entry) {
		return nil
	}
	if _, err := vniFilterRequest(unix.RTM_NEWTUNNEL, index, entry).Execute(unix.NETLINK_ROUTE, 0); err != nil {
		return fmt.Errorf("adding VNI %d to VXLAN %d VNI filters: %w", vni, index, err)
	}
	return nil
}

// removeVniFilterEntry removes a single VNI from the device's filter table,
// the equivalent of `bridge vni del dev <device> vni <vni>`.
func removeVniFilterEntry(index int, vni uint32) error {
	entry := vniFilterRange{Start: vni, End: vni}
	existing, err := vniFilterEntries(index)
	if err != nil {
		return err
	}
	if !containsVniFilterRange(existing, entry) {
		return nil
	}
	if _, err := vniFilterRequest(unix.RTM_DELTUNNEL, index, entry).Execute(unix.NETLINK_ROUTE, 0); err != nil {
		return fmt.Errorf("removing VNI %d from VXLAN %d VNI filters: %w", vni, index, err)
	}
	return nil
}

func containsVniFilterRange(rangesToSearch []vniFilterRange, target vniFilterRange) bool {
	for _, entry := range rangesToSearch {
		if entry == target {
			return true
		}
	}
	return false
}

// vxlanTunnelMappings returns the VLAN-to-VNI tunnel mappings of a single
// device. BridgeVlanTunnelShow discards the interface index, so another
// device's private VLAN 1 mapping could otherwise satisfy this device's
// reconciliation.
func vxlanTunnelMappings(index int) ([]nl.TunnelInfo, error) {
	req := nl.NewNetlinkRequest(unix.RTM_GETLINK, unix.NLM_F_DUMP)
	req.AddData(nl.NewIfInfomsg(unix.AF_BRIDGE))
	req.AddData(nl.NewRtAttr(unix.IFLA_EXT_MASK, nl.Uint32Attr(nl.RTEXT_FILTER_BRVLAN)))
	messages, err := req.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWLINK)
	if err != nil {
		return nil, fmt.Errorf("listing VXLAN %d tunnel mappings: %w", index, err)
	}
	var result []nl.TunnelInfo
	for _, message := range messages {
		mappings, err := parseTunnelMappings(message, index)
		if err != nil {
			return nil, fmt.Errorf("reading VXLAN %d tunnel mappings: %w", index, err)
		}
		result = append(result, mappings...)
	}
	return result, nil
}

func vniFilterVxlanRequest(vxlan *netlink.Vxlan) *nl.NetlinkRequest {
	req := nl.NewNetlinkRequest(unix.RTM_NEWLINK, unix.NLM_F_ACK|unix.NLM_F_CREATE|unix.NLM_F_EXCL)
	req.AddData(nl.NewIfInfomsg(unix.AF_UNSPEC))
	req.AddData(nl.NewRtAttr(unix.IFLA_IFNAME, nl.ZeroTerminated(vxlan.Name)))
	if vxlan.MasterIndex != 0 {
		req.AddData(nl.NewRtAttr(unix.IFLA_MASTER, nl.Uint32Attr(uint32(vxlan.MasterIndex))))
	}
	if len(vxlan.HardwareAddr) != 0 {
		req.AddData(nl.NewRtAttr(unix.IFLA_ADDRESS, vxlan.HardwareAddr))
	}
	info := nl.NewRtAttr(unix.IFLA_LINKINFO, nil)
	info.AddRtAttr(unix.IFLA_INFO_KIND, nl.ZeroTerminated("vxlan"))
	data := info.AddRtAttr(unix.IFLA_INFO_DATA, nil)
	data.AddRtAttr(nl.IFLA_VXLAN_FLOWBASED, []byte{1})
	data.AddRtAttr(unix.IFLA_VXLAN_VNIFILTER, []byte{1})
	data.AddRtAttr(nl.IFLA_VXLAN_LEARNING, []byte{0})
	if vxlan.VtepDevIndex != 0 {
		data.AddRtAttr(nl.IFLA_VXLAN_LINK, nl.Uint32Attr(uint32(vxlan.VtepDevIndex)))
	}
	if vxlan.SrcAddr != nil {
		if ip := vxlan.SrcAddr.To4(); ip != nil {
			data.AddRtAttr(nl.IFLA_VXLAN_LOCAL, ip)
		} else {
			data.AddRtAttr(nl.IFLA_VXLAN_LOCAL6, vxlan.SrcAddr.To16())
		}
	}
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(vxlan.Port))
	data.AddRtAttr(nl.IFLA_VXLAN_PORT, port)
	req.AddData(info)
	return req
}

func vniFilterRequest(command, index int, entry vniFilterRange) *nl.NetlinkRequest {
	req := nl.NewNetlinkRequest(command, unix.NLM_F_ACK)
	attr := nl.NewRtAttr(vniFilterEntryAttr|unix.NLA_F_NESTED, nil)
	attr.AddRtAttr(vniFilterStartAttr, nl.Uint32Attr(entry.Start))
	if entry.End != entry.Start {
		attr.AddRtAttr(vniFilterEndAttr, nl.Uint32Attr(entry.End))
	}
	req.AddRawData(append(tunnelMessageHeader(index), attr.Serialize()...))
	return req
}

func tunnelMessageHeader(index int) []byte {
	msg := make([]byte, tunnelMessageHeaderSize)
	msg[0] = unix.AF_BRIDGE
	binary.NativeEndian.PutUint32(msg[4:], uint32(index))
	return msg
}

func parseVniFilterEntries(message []byte, index int) ([]vniFilterRange, error) {
	if len(message) < tunnelMessageHeaderSize {
		return nil, errors.New("truncated tunnel header")
	}
	if message[0] != unix.AF_BRIDGE {
		return nil, fmt.Errorf("unexpected tunnel family %d", message[0])
	}
	if binary.NativeEndian.Uint32(message[4:8]) != uint32(index) {
		return nil, nil
	}
	attrs, err := checkedAttributes(message[tunnelMessageHeaderSize:])
	if err != nil {
		return nil, err
	}
	var result []vniFilterRange
	for _, attr := range attrs {
		if attr.Attr.Type&netlinkAttributeTypeMask != vniFilterEntryAttr {
			continue
		}
		fields, err := checkedAttributes(attr.Value)
		if err != nil {
			return nil, fmt.Errorf("parsing VNI filter entry: %w", err)
		}
		var entry vniFilterRange
		startFound := false
		endFound := false
		for _, field := range fields {
			switch field.Attr.Type & netlinkAttributeTypeMask {
			case vniFilterStartAttr, vniFilterEndAttr:
				if len(field.Value) != 4 {
					return nil, errors.New("invalid VNI filter integer length")
				}
				value := binary.NativeEndian.Uint32(field.Value)
				if field.Attr.Type&netlinkAttributeTypeMask == vniFilterStartAttr {
					entry.Start, startFound = value, true
				} else {
					entry.End, endFound = value, true
				}
			}
		}
		if !startFound {
			return nil, errors.New("VNI filter entry has no start")
		}
		if !endFound {
			entry.End = entry.Start
		}
		if entry.Start > entry.End || entry.End > maxVXLANVNI {
			return nil, fmt.Errorf("invalid VNI filter range %d-%d", entry.Start, entry.End)
		}
		result = append(result, entry)
	}
	return result, nil
}

func parseVniFilterFlag(raw []byte) (bool, error) {
	for _, kind := range []uint16{unix.IFLA_LINKINFO, unix.IFLA_INFO_DATA, unix.IFLA_VXLAN_VNIFILTER} {
		attrs, err := checkedAttributes(raw)
		if err != nil {
			return false, fmt.Errorf("parsing VXLAN VNI filter flag: %w", err)
		}
		found := false
		for _, attr := range attrs {
			if attr.Attr.Type&netlinkAttributeTypeMask == kind {
				raw, found = attr.Value, true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	if len(raw) != 1 {
		return false, errors.New("invalid VXLAN VNI filter flag length")
	}
	return raw[0] != 0, nil
}

func parseTunnelMappings(message []byte, index int) ([]nl.TunnelInfo, error) {
	if len(message) < unix.SizeofIfInfomsg {
		return nil, errors.New("truncated bridge link reply")
	}
	if nl.DeserializeIfInfomsg(message).Index != int32(index) {
		return nil, nil
	}
	attrs, err := checkedAttributes(message[unix.SizeofIfInfomsg:])
	if err != nil {
		return nil, err
	}
	var result []nl.TunnelInfo
	for _, attr := range attrs {
		if attr.Attr.Type&netlinkAttributeTypeMask != unix.IFLA_AF_SPEC {
			continue
		}
		bridgeAttrs, err := checkedAttributes(attr.Value)
		if err != nil {
			return nil, err
		}
		for _, bridgeAttr := range bridgeAttrs {
			if bridgeAttr.Attr.Type&netlinkAttributeTypeMask != nl.IFLA_BRIDGE_VLAN_TUNNEL_INFO {
				continue
			}
			mapping, flags, err := parseTunnelMapping(bridgeAttr.Value)
			if err != nil {
				return nil, err
			}
			if flags&nl.BRIDGE_VLAN_INFO_RANGE_END == 0 {
				result = append(result, mapping)
				continue
			}
			if len(result) == 0 || mapping.Vid < result[len(result)-1].Vid {
				return nil, errors.New("invalid bridge tunnel mapping range")
			}
			start := result[len(result)-1]
			for vid := start.Vid + 1; vid <= mapping.Vid; vid++ {
				result = append(result, nl.TunnelInfo{Vid: vid, TunId: start.TunId + uint32(vid-start.Vid)})
			}
		}
	}
	return result, nil
}

func parseTunnelMapping(raw []byte) (nl.TunnelInfo, uint16, error) {
	attrs, err := checkedAttributes(raw)
	if err != nil {
		return nl.TunnelInfo{}, 0, err
	}
	var mapping nl.TunnelInfo
	var flags uint16
	idFound, vidFound := false, false
	for _, attr := range attrs {
		switch attr.Attr.Type & netlinkAttributeTypeMask {
		case nl.IFLA_BRIDGE_VLAN_TUNNEL_ID:
			if len(attr.Value) != 4 {
				return mapping, flags, errors.New("invalid bridge tunnel ID length")
			}
			mapping.TunId, idFound = binary.NativeEndian.Uint32(attr.Value), true
		case nl.IFLA_BRIDGE_VLAN_TUNNEL_VID, nl.IFLA_BRIDGE_VLAN_TUNNEL_FLAGS:
			if len(attr.Value) != 2 {
				return mapping, flags, errors.New("invalid bridge tunnel VLAN/flags length")
			}
			value := binary.NativeEndian.Uint16(attr.Value)
			if attr.Attr.Type&netlinkAttributeTypeMask == nl.IFLA_BRIDGE_VLAN_TUNNEL_VID {
				mapping.Vid, vidFound = value, true
			} else {
				flags = value
			}
		}
	}
	if !idFound || !vidFound || mapping.Vid == 0 || mapping.Vid > maxVLANID || mapping.TunId > maxVXLANVNI {
		return mapping, flags, errors.New("invalid bridge tunnel mapping")
	}
	return mapping, flags, nil
}

// checkedAttributes verifies the attribute alignment before handing the
// buffer to nl.ParseRouteAttr, which trusts the lengths it reads.
func checkedAttributes(raw []byte) ([]syscall.NetlinkRouteAttr, error) {
	for remaining := raw; len(remaining) != 0; {
		if len(remaining) < unix.SizeofRtAttr {
			return nil, errors.New("truncated netlink attribute header")
		}
		length := int(binary.NativeEndian.Uint16(remaining[:2]))
		aligned := (length + unix.RTA_ALIGNTO - 1) & ^(unix.RTA_ALIGNTO - 1)
		if length < unix.SizeofRtAttr || aligned > len(remaining) {
			return nil, errors.New("invalid netlink attribute length")
		}
		remaining = remaining[aligned:]
	}
	return nl.ParseRouteAttr(raw)
}
