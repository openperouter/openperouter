// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

var guardRouteDump = dumpGuardRoutes

// netlink.Route discards rtm_src_len. Read guard candidates directly so a
// source-specific IPv6 default cannot stand in for an unconditional guard.
func listGuardRouteCandidates(family int, filter *netlink.Route, _ uint64) ([]netlink.Route, error) {
	strict := filter.Table != unix.RT_TABLE_UNSPEC
	messages, err := guardRouteDump(family, filter.Table, strict)
	if strict && (errors.Is(err, unix.ENOPROTOOPT) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EINVAL)) {
		messages, err = guardRouteDump(family, unix.RT_TABLE_UNSPEC, false)
	}
	if err != nil {
		return nil, fmt.Errorf("dump default guards: %w", err)
	}
	var routes []netlink.Route
	for _, data := range messages {
		route, err := decodeGuardRoute(data, family, filter)
		if err != nil {
			return nil, err
		}
		if route != nil {
			routes = append(routes, *route)
		}
	}
	return routes, nil
}

func decodeGuardRoute(data []byte, family int, filter *netlink.Route) (*netlink.Route, error) {
	if len(data) < unix.SizeofRtMsg {
		return nil, fmt.Errorf("short default guard route message")
	}
	header := nl.DeserializeRtMsg(data)
	if int(header.Family) != family || header.Dst_len != 0 || header.Src_len != 0 || header.Tos != 0 {
		return nil, nil
	}
	if header.Type != unix.RTN_UNREACHABLE || header.Protocol != uint8(vrfGuardProtocol) {
		return nil, nil
	}
	if header.Flags&unix.RTM_F_CLONED != 0 {
		return nil, nil
	}
	attrs, err := nl.ParseRouteAttr(data[header.Len():])
	if err != nil {
		return nil, fmt.Errorf("decode default guard attributes: %w", err)
	}
	table, priority := uint32(header.Table), uint32(0)
	for _, attr := range attrs {
		switch attr.Attr.Type {
		case unix.RTA_TABLE, unix.RTA_PRIORITY:
			if len(attr.Value) != 4 {
				return nil, fmt.Errorf("invalid default guard attribute %d", attr.Attr.Type)
			}
			value := binary.NativeEndian.Uint32(attr.Value)
			if attr.Attr.Type == unix.RTA_TABLE {
				table = value
			} else {
				priority = value
			}
		}
	}
	if filter.Table != unix.RT_TABLE_UNSPEC && filter.Table != int(table) {
		return nil, nil
	}
	if priority != uint32(filter.Priority) {
		return nil, nil
	}
	route := vrfDefaultGuard(table, family)
	route.Scope = netlink.Scope(header.Scope)
	return &route, nil
}

// Strict checking enables kernel-side table filtering. Older kernels either
// reject the socket option/request or ignore filters; the decoder also filters.
func dumpGuardRoutes(family, table int, strict bool) ([][]byte, error) {
	socket, err := nl.GetNetlinkSocketAt(netns.None(), netns.None(), unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	defer socket.Close()
	if strict {
		if err := unix.SetsockoptInt(socket.GetFd(), unix.SOL_NETLINK, unix.NETLINK_GET_STRICT_CHK, 1); err != nil {
			return nil, err
		}
	}
	req := nl.NewNetlinkRequest(unix.RTM_GETROUTE, unix.NLM_F_DUMP)
	req.Sockets = map[int]*nl.SocketHandle{unix.NETLINK_ROUTE: {Socket: socket}}
	req.AddData(&nl.RtMsg{RtMsg: unix.RtMsg{Family: uint8(family)}})
	if strict {
		req.AddData(nl.NewRtAttr(unix.RTA_TABLE, nl.Uint32Attr(uint32(table))))
	}
	messages, err := req.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWROUTE)
	if strict && errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	return messages, err
}
