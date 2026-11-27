// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

func TestSwitchingBridgeName(t *testing.T) {
	require.Equal(t, "br-vl-100", switchingBridgeName(100))
}

func TestVniFromSwitchingBridgeName(t *testing.T) {
	vni, err := vniFromSwitchingBridgeName("br-vl-100")
	require.NoError(t, err)
	require.Equal(t, int32(100), vni)

	_, err = vniFromSwitchingBridgeName("br-pe-100")
	require.True(t, errors.As(err, &NotRouterInterfaceError{}))

	_, err = vniFromSwitchingBridgeName("br-vl-notanumber")
	require.ErrorContains(t, err, "failed to get vni")
}

func TestPortMemberships(t *testing.T) {
	require.Equal(t, []vlanMembership{{VID: 1, PVID: true, Untagged: true}}, newAccessPort(1))
	require.Equal(t, []vlanMembership{{VID: 100}, {VID: 200}}, newTrunk([]uint16{100, 200}))
	require.Empty(t, newTrunk(nil))
}

func TestResolveVlanMembershipOptions(t *testing.T) {
	require.False(t, resolveVlanMembershipOptions().bridgeInterface)
	require.True(t, resolveVlanMembershipOptions(withBridgeInterface()).bridgeInterface)
}

func TestExpandVlanMemberships(t *testing.T) {
	entries := []*nl.BridgeVlanInfo{
		{Vid: 100},
		{Vid: 200, Flags: nl.BRIDGE_VLAN_INFO_RANGE_BEGIN},
		{Vid: 202, Flags: nl.BRIDGE_VLAN_INFO_RANGE_END | nl.BRIDGE_VLAN_INFO_PVID | nl.BRIDGE_VLAN_INFO_UNTAGGED},
	}

	require.Equal(t, []vlanMembership{
		{VID: 100},
		{VID: 200, PVID: true, Untagged: true},
		{VID: 201, PVID: true, Untagged: true},
		{VID: 202, PVID: true, Untagged: true},
	}, expandVlanMemberships(entries))
}

func TestMatchesVLANEndpoint(t *testing.T) {
	endpoint := &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{Name: "br-pe-100", ParentIndex: 5},
		VlanId:    1,
	}

	require.True(t, matchesVLANEndpoint(endpoint, 5, 1))
	require.False(t, matchesVLANEndpoint(endpoint, 6, 1), "another parent is a different endpoint")
	require.False(t, matchesVLANEndpoint(endpoint, 5, 2), "another VLAN is a different endpoint")
	require.False(t, matchesVLANEndpoint(&netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: "br-pe-100"}}, 5, 1),
		"a bridge is not a VLAN endpoint")
}
