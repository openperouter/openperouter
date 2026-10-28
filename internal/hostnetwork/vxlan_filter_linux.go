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

// The pinned netlink library does not expose VXLAN receive filters. Linux's
// tunnel_msg is eight bytes, followed by nested VXLAN_VNIFILTER_ENTRY attributes.
const (
	netlinkAttributeTypeMask = 0x3fff
	tunnelHeaderSize         = 8
	vniFilterEntry           = 1
	vniFilterStart           = 1
	vniFilterEnd             = 2
	maxVXLANVNI              = 1<<24 - 1
)

type vniFilterRange struct {
	Start uint32
	End   uint32
}

func createFilteredVXLAN(vxlan *netlink.Vxlan) (*netlink.Vxlan, error) {
	if _, err := filteredVXLANRequest(vxlan).Execute(unix.NETLINK_ROUTE, 0); err != nil {
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

func vxlanVNIFilterEnabled(index int) (bool, error) {
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
		return parseVNIFilterFlag(message[unix.SizeofIfInfomsg:])
	}
	return false, fmt.Errorf("missing VXLAN %d link reply", index)
}

func ensureVNIFilter(index int, vni uint32) error {
	req := nl.NewNetlinkRequest(unix.RTM_GETTUNNEL, unix.NLM_F_DUMP)
	req.AddRawData(tunnelHeader(index))
	messages, err := req.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWTUNNEL)
	if err != nil {
		return fmt.Errorf("listing VXLAN %d VNI filters: %w", index, err)
	}
	found := false
	for _, message := range messages {
		entries, err := parseVNIFilters(message, index)
		if err != nil {
			return fmt.Errorf("reading VXLAN %d VNI filters: %w", index, err)
		}
		for _, entry := range entries {
			if entry.Start == vni && entry.End == vni {
				found = true
				continue
			}
			if _, err := vniFilterRequest(unix.RTM_DELTUNNEL, index, entry).Execute(unix.NETLINK_ROUTE, 0); err != nil {
				return fmt.Errorf("deleting VXLAN %d VNI filter %d-%d: %w", index, entry.Start, entry.End, err)
			}
		}
	}
	if found {
		return nil
	}
	entry := vniFilterRange{Start: vni, End: vni}
	if _, err := vniFilterRequest(unix.RTM_NEWTUNNEL, index, entry).Execute(unix.NETLINK_ROUTE, 0); err != nil {
		return fmt.Errorf("adding VXLAN %d VNI filter %d: %w", index, vni, err)
	}
	return nil
}

func vxlanTunnelMappings(index int) ([]nl.TunnelInfo, error) {
	// BridgeVlanTunnelShow discards the interface index; retain it here so another
	// VNI's private VLAN1 mapping can never satisfy this interface's reconciliation.
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

func filteredVXLANRequest(vxlan *netlink.Vxlan) *nl.NetlinkRequest {
	req := nl.NewNetlinkRequest(unix.RTM_NEWLINK, unix.NLM_F_ACK|unix.NLM_F_CREATE|unix.NLM_F_EXCL)
	req.AddData(nl.NewIfInfomsg(unix.AF_UNSPEC))
	req.AddData(nl.NewRtAttr(unix.IFLA_IFNAME, nl.ZeroTerminated(vxlan.Name)))
	req.AddData(nl.NewRtAttr(unix.IFLA_MASTER, nl.Uint32Attr(uint32(vxlan.MasterIndex))))
	if len(vxlan.HardwareAddr) != 0 {
		req.AddData(nl.NewRtAttr(unix.IFLA_ADDRESS, vxlan.HardwareAddr))
	}
	info := nl.NewRtAttr(unix.IFLA_LINKINFO, nil)
	info.AddRtAttr(unix.IFLA_INFO_KIND, nl.ZeroTerminated("vxlan"))
	data := info.AddRtAttr(unix.IFLA_INFO_DATA, nil)
	data.AddRtAttr(nl.IFLA_VXLAN_FLOWBASED, []byte{1})
	data.AddRtAttr(unix.IFLA_VXLAN_VNIFILTER, []byte{1})
	data.AddRtAttr(nl.IFLA_VXLAN_LEARNING, []byte{0})
	data.AddRtAttr(nl.IFLA_VXLAN_LINK, nl.Uint32Attr(uint32(vxlan.VtepDevIndex)))
	if ip := vxlan.SrcAddr.To4(); ip != nil {
		data.AddRtAttr(nl.IFLA_VXLAN_LOCAL, ip)
	} else {
		data.AddRtAttr(nl.IFLA_VXLAN_LOCAL6, vxlan.SrcAddr.To16())
	}
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(vxlan.Port))
	data.AddRtAttr(nl.IFLA_VXLAN_PORT, port)
	req.AddData(info)
	return req
}

func vniFilterRequest(command, index int, entry vniFilterRange) *nl.NetlinkRequest {
	req := nl.NewNetlinkRequest(command, unix.NLM_F_ACK)
	attr := nl.NewRtAttr(vniFilterEntry|unix.NLA_F_NESTED, nil)
	attr.AddRtAttr(vniFilterStart, nl.Uint32Attr(entry.Start))
	if entry.End != entry.Start {
		attr.AddRtAttr(vniFilterEnd, nl.Uint32Attr(entry.End))
	}
	req.AddRawData(append(tunnelHeader(index), attr.Serialize()...))
	return req
}

func tunnelHeader(index int) []byte {
	msg := make([]byte, tunnelHeaderSize)
	msg[0] = unix.AF_BRIDGE
	binary.NativeEndian.PutUint32(msg[4:], uint32(index))
	return msg
}

func parseVNIFilters(message []byte, index int) ([]vniFilterRange, error) {
	if len(message) < tunnelHeaderSize {
		return nil, errors.New("truncated tunnel header")
	}
	if message[0] != unix.AF_BRIDGE {
		return nil, fmt.Errorf("unexpected tunnel family %d", message[0])
	}
	if binary.NativeEndian.Uint32(message[4:8]) != uint32(index) {
		return nil, nil
	}
	attrs, err := checkedAttributes(message[tunnelHeaderSize:])
	if err != nil {
		return nil, err
	}
	var result []vniFilterRange
	for _, attr := range attrs {
		if attr.Attr.Type&netlinkAttributeTypeMask != vniFilterEntry {
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
			case vniFilterStart, vniFilterEnd:
				if len(field.Value) != 4 {
					return nil, errors.New("invalid VNI filter integer length")
				}
				value := binary.NativeEndian.Uint32(field.Value)
				if field.Attr.Type&netlinkAttributeTypeMask == vniFilterStart {
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

func parseVNIFilterFlag(raw []byte) (bool, error) {
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
	if !idFound || !vidFound || mapping.Vid == 0 || mapping.Vid > 4094 || mapping.TunId > maxVXLANVNI {
		return mapping, flags, errors.New("invalid bridge tunnel mapping")
	}
	return mapping, flags, nil
}

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
