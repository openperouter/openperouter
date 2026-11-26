// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"encoding/binary"
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func TestVniFilterVxlanRequest(t *testing.T) {
	for _, source := range []string{"192.0.2.1", "2001:db8::1"} {
		t.Run(source, func(t *testing.T) {
			vxlan := &netlink.Vxlan{
				LinkAttrs:    netlink.LinkAttrs{Name: "vni100", MasterIndex: 42},
				VtepDevIndex: 9, SrcAddr: net.ParseIP(source), Port: 4789, FlowBased: true,
			}
			req := vniFilterVxlanRequest(vxlan)
			raw := req.Serialize()
			require.Equal(t, uint16(unix.RTM_NEWLINK), req.Type)
			require.Equal(t, uint16(unix.NLM_F_REQUEST|unix.NLM_F_ACK|unix.NLM_F_CREATE|unix.NLM_F_EXCL), req.Flags)
			attrs := testAttributes(t, raw[unix.SizeofNlMsghdr+unix.SizeofIfInfomsg:])
			require.Equal(t, []byte("vni100\x00"), attrs[unix.IFLA_IFNAME])
			require.Equal(t, uint32(42), binary.NativeEndian.Uint32(attrs[unix.IFLA_MASTER]))
			info := testAttributes(t, attrs[unix.IFLA_LINKINFO])
			require.Equal(t, []byte("vxlan\x00"), info[unix.IFLA_INFO_KIND])
			data := testAttributes(t, info[unix.IFLA_INFO_DATA])
			require.Equal(t, []byte{1}, data[nl.IFLA_VXLAN_FLOWBASED])
			require.Equal(t, []byte{1}, data[unix.IFLA_VXLAN_VNIFILTER])
			require.Equal(t, []byte{0}, data[nl.IFLA_VXLAN_LEARNING])
			require.Equal(t, uint16(4789), binary.BigEndian.Uint16(data[nl.IFLA_VXLAN_PORT]))
			require.Equal(t, uint32(9), binary.NativeEndian.Uint32(data[nl.IFLA_VXLAN_LINK]))
			if ip := net.ParseIP(source).To4(); ip != nil {
				require.Equal(t, []byte(ip), data[nl.IFLA_VXLAN_LOCAL])
			} else {
				require.Equal(t, []byte(net.ParseIP(source).To16()), data[nl.IFLA_VXLAN_LOCAL6])
			}
		})
	}
}

func TestCreateVniFilterVxlanRequiresExternalMode(t *testing.T) {
	_, err := createVniFilterVxlan(&netlink.Vxlan{LinkAttrs: netlink.LinkAttrs{Name: "vni100"}})
	require.EqualError(t, err,
		"creating VXLAN vni100 with VNI filtering: VNI filtering requires collect-metadata (external) mode")
}

func TestVniFilterRequestWire(t *testing.T) {
	tests := []struct {
		name    string
		command int
		entry   vniFilterRange
	}{
		{
			name:    "single vni add",
			command: unix.RTM_NEWTUNNEL,
			entry:   vniFilterRange{Start: 100, End: 100},
		},
		{
			name:    "range delete",
			command: unix.RTM_DELTUNNEL,
			entry:   vniFilterRange{Start: 100, End: 200},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := vniFilterRequest(tt.command, 41, tt.entry)
			require.Equal(t, uint16(tt.command), req.Type)
			raw := req.Serialize()[unix.SizeofNlMsghdr:]
			require.Equal(t, byte(unix.AF_BRIDGE), raw[0])
			require.Equal(t, []byte{0, 0, 0}, raw[1:4])
			require.Equal(t, uint32(41), binary.NativeEndian.Uint32(raw[4:8]))

			entries, err := parseVniFilterEntries(raw, 41)
			require.NoError(t, err)
			require.Equal(t, []vniFilterRange{tt.entry}, entries)

			entries, err = parseVniFilterEntries(raw, 42)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestVniFilterMalformedReplies(t *testing.T) {
	for _, input := range [][]byte{
		{},
		{unix.AF_BRIDGE},
		append(tunnelMessageHeader(41), []byte{3, 0, 1, 0}...),
		append(tunnelMessageHeader(41), nl.NewRtAttr(vniFilterEntryAttr, nil).Serialize()...),
		append(tunnelMessageHeader(41), nl.NewRtAttr(vniFilterEntryAttr,
			nl.NewRtAttr(vniFilterStartAttr, []byte{1}).Serialize()).Serialize()...),
	} {
		_, err := parseVniFilterEntries(input, 41)
		require.Error(t, err, "input %x", input)
	}
}

func TestVniFilterFlag(t *testing.T) {
	tests := []struct {
		name      string
		flag      []byte
		want      bool
		wantError string
	}{
		{name: "enabled", flag: []byte{1}, want: true},
		{name: "disabled", flag: []byte{0}},
		{name: "truncated", wantError: "invalid VXLAN VNI filter flag length"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := nl.NewRtAttr(unix.IFLA_LINKINFO|unix.NLA_F_NESTED, nil)
			data := info.AddRtAttr(unix.IFLA_INFO_DATA|unix.NLA_F_NESTED, nil)
			data.AddRtAttr(unix.IFLA_VXLAN_VNIFILTER, tt.flag)
			got, err := parseVniFilterFlag(info.Serialize())
			if tt.wantError != "" {
				require.EqualError(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	got, err := parseVniFilterFlag(nil)
	require.NoError(t, err)
	require.False(t, got)
}

func TestTunnelMappingDeviceScope(t *testing.T) {
	msg := nl.NewIfInfomsg(unix.AF_BRIDGE)
	msg.Index = 41
	af := nl.NewRtAttr(unix.IFLA_AF_SPEC, nil)
	mapping := af.AddRtAttr(nl.IFLA_BRIDGE_VLAN_TUNNEL_INFO, nil)
	mapping.AddRtAttr(nl.IFLA_BRIDGE_VLAN_TUNNEL_ID, nl.Uint32Attr(16777215))
	mapping.AddRtAttr(nl.IFLA_BRIDGE_VLAN_TUNNEL_VID, nl.Uint16Attr(1))
	raw := append(msg.Serialize(), af.Serialize()...)

	got, err := parseTunnelMappings(raw, 41)
	require.NoError(t, err)
	require.Equal(t, []nl.TunnelInfo{{Vid: 1, TunId: 16777215}}, got)

	got, err = parseTunnelMappings(raw, 42)
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = parseTunnelMappings(raw[:len(raw)-1], 41)
	require.EqualError(t, err, "invalid netlink attribute length")
}

func TestVniFilterKernelErrorKeepsErrno(t *testing.T) {
	_, err := vniFilterEntries(1 << 30)
	require.ErrorContains(t, err, "listing VXLAN 1073741824 VNI filters")
	var errno syscall.Errno
	require.True(t, errors.As(err, &errno), "kernel errno must remain discoverable: %v", err)
	// Before VXLAN is loaded, RTM_GETTUNNEL can fail before checking the interface index.
	require.Contains(t, []syscall.Errno{unix.ENODEV, unix.EPERM, unix.EOPNOTSUPP}, errno, "%v", err)
}

func testAttributes(t *testing.T, raw []byte) map[uint16][]byte {
	t.Helper()
	attrs, err := nl.ParseRouteAttr(raw)
	require.NoError(t, err)
	result := map[uint16][]byte{}
	for _, attr := range attrs {
		result[attr.Attr.Type&netlinkAttributeTypeMask] = attr.Value
	}
	return result
}
