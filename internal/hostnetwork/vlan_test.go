// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"errors"
	"net"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
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

var _ = Describe("VLAN-aware primitives", func() {
	const nsName = "vlantestns"
	var ns netns.NsHandle

	BeforeEach(func() {
		cleanTest(nsName)
		ns = createTestNS(nsName)
	})
	AfterEach(func() {
		cleanTest(nsName)
	})

	// portForVLANs creates the private switching bridge and a veth port
	// enslaved to it, returning both.
	portForVLANs := func() (*netlink.Bridge, netlink.Link) {
		var bridge *netlink.Bridge
		var port netlink.Link
		Expect(netnamespace.In(ns, func() error {
			b, err := ensureSwitchingBridge("vltestbr")
			if err != nil {
				return err
			}
			bridge = b
			veth := &netlink.Veth{
				LinkAttrs: netlink.LinkAttrs{Name: "vltestA"},
				PeerName:  "vltestB",
			}
			if err := netlink.LinkAdd(veth); err != nil {
				return err
			}
			p, err := netlink.LinkByName("vltestA")
			if err != nil {
				return err
			}
			port = p
			return linkSetMaster(port, bridge)
		})).To(Succeed())
		return bridge, port
	}

	memberships := func(link netlink.Link) []vlanMembership {
		var got []vlanMembership
		Expect(netnamespace.In(ns, func() error {
			var err error
			got, err = portVlanMemberships(link)
			return err
		})).To(Succeed())
		return got
	}

	It("converges a trunk port to the exact tag set", func() {
		_, port := portForVLANs()
		Expect(netnamespace.In(ns, func() error {
			// A bridge created before the default PVID was disabled leaves an
			// implicit vid 1 pvid untagged entry that must not survive.
			if err := netlink.BridgeVlanAdd(port, 1, true, true, false, false); err != nil {
				return err
			}
			return ensurePortVlans(port, newTrunk([]uint16{100, 200}))
		})).To(Succeed())

		Expect(memberships(port)).To(ConsistOf(
			vlanMembership{VID: 100},
			vlanMembership{VID: 200},
		))
	})

	It("converges an access port and removes a stray tagged member", func() {
		_, port := portForVLANs()
		Expect(netnamespace.In(ns, func() error {
			if err := netlink.BridgeVlanAdd(port, 50, false, false, false, false); err != nil {
				return err
			}
			return ensurePortVlans(port, newAccessPort(1))
		})).To(Succeed())

		Expect(memberships(port)).To(ConsistOf(
			vlanMembership{VID: 1, PVID: true, Untagged: true},
		))
	})

	It("converges the bridge self entry and removes a pre-seeded pvid", func() {
		bridge, _ := portForVLANs()
		Expect(netnamespace.In(ns, func() error {
			if err := netlink.BridgeVlanAdd(bridge, 1, true, true, true, false); err != nil {
				return err
			}
			return ensurePortVlans(bridge, newTrunk([]uint16{100}), withBridgeInterface())
		})).To(Succeed())

		Expect(memberships(bridge)).To(ConsistOf(vlanMembership{VID: 100}))
	})

	It("reads one link's VLAN table like the full dump", func() {
		bridge, port := portForVLANs()
		Expect(netnamespace.In(ns, func() error {
			if err := netlink.BridgeVlanAdd(port, 100, false, false, false, false); err != nil {
				return err
			}
			if err := netlink.BridgeVlanAdd(port, 200, true, true, false, false); err != nil {
				return err
			}
			return netlink.BridgeVlanAdd(bridge, 300, false, false, true, false)
		})).To(Succeed())

		var wantPort, wantBridge []vlanMembership
		Expect(netnamespace.In(ns, func() error {
			all, err := netlink.BridgeVlanList()
			if err != nil {
				return err
			}
			wantPort = expandVlanMemberships(all[int32(port.Attrs().Index)])
			wantBridge = expandVlanMemberships(all[int32(bridge.Attrs().Index)])
			return nil
		})).To(Succeed())

		Expect(memberships(port)).To(ConsistOf(wantPort))
		Expect(memberships(bridge)).To(ConsistOf(wantBridge))
	})
})

var _ = Describe("VLAN-aware VXLAN and endpoint primitives", func() {
	const nsName = "svdtestns"
	var ns netns.NsHandle

	BeforeEach(func() {
		cleanTest(nsName)
		ns = createTestNS(nsName)
	})
	AfterEach(func() {
		cleanTest(nsName)
	})

	It("maps the internal VLAN to the VNI and removes the mapping", func() {
		Expect(netnamespace.In(ns, func() error {
			bridge, err := ensureSwitchingBridge("svdtestbr")
			if err != nil {
				return err
			}
			vtepDev, err := vtepDeviceInTestNS("192.0.2.1/32")
			if err != nil {
				return err
			}
			svd, err := ensureSVD(svdParams{
				Name:            "vni100",
				Bridge:          bridge,
				VTEPIP:          "192.0.2.1/32",
				Port:            4789,
				HardwareAddr:    bridge.HardwareAddr,
				VtepDeviceIndex: vtepDev.Attrs().Index,
			})
			if err != nil {
				return err
			}
			Expect(svd.FlowBased).To(BeTrue(), "SVD must be a collect-metadata device")
			Expect(svd.VxlanId).To(BeZero(), "SVD must not have a fixed VNI")

			enabled, err := vniFilterEnabled(svd.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeTrue(), "SVD must enable VNI filtering")

			protinfo, err := netlink.LinkGetProtinfo(svd)
			Expect(err).NotTo(HaveOccurred())
			Expect(protinfo.VlanTunnel).To(BeTrue(), "SVD must enable VLAN tunnel mode")

			Expect(ensureVlanToVNI(svd, internalVLANID, 100)).To(Succeed())
			mappings, err := vxlanTunnelMappings(svd.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(mappings).To(ContainElement(nl.TunnelInfo{Vid: internalVLANID, TunId: 100}))
			entries, err := vniFilterEntries(svd.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(ContainElement(vniFilterRange{Start: 100, End: 100}))

			Expect(removeVlanToVNI(svd, internalVLANID, 100)).To(Succeed())
			mappings, err = vxlanTunnelMappings(svd.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(mappings).NotTo(ContainElement(nl.TunnelInfo{Vid: internalVLANID, TunId: 100}))
			entries, err = vniFilterEntries(svd.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).NotTo(ContainElement(vniFilterRange{Start: 100, End: 100}))
			return nil
		})).To(Succeed())
	})

	It("turns a pre-existing bridge into a VLAN endpoint and removes it", func() {
		Expect(netnamespace.In(ns, func() error {
			bridge, err := ensureSwitchingBridge("endptestbr")
			if err != nil {
				return err
			}
			name := BridgeName(100)
			if err := netlink.LinkAdd(&netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}); err != nil {
				return err
			}

			endpoint, err := ensureVLANEndpoint(name, bridge, internalVLANID)
			if err != nil {
				return err
			}
			vlan, ok := endpoint.(*netlink.Vlan)
			Expect(ok).To(BeTrue(), "endpoint must be a VLAN device, got %s", endpoint.Type())
			Expect(vlan.VlanId).To(Equal(int(internalVLANID)))
			Expect(vlan.ParentIndex).To(Equal(bridge.Index))
			Expect(vlan.Attrs().HardwareAddr).To(Equal(bridge.HardwareAddr))
			Expect(vlan.Attrs().Flags & net.FlagUp).To(Equal(net.FlagUp))

			Expect(removeVLANEndpoint(name)).To(Succeed())
			_, err = netlink.LinkByName(name)
			Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue())
			Expect(removeVLANEndpoint(name)).To(Succeed(), "removal must tolerate absence")
			return nil
		})).To(Succeed())
	})

	It("installs and removes the local gateway FDB entry", func() {
		Expect(netnamespace.In(ns, func() error {
			bridge, err := ensureSwitchingBridge("fdbtestbr")
			if err != nil {
				return err
			}
			mac, err := BridgeFixedMAC(100)
			if err != nil {
				return err
			}

			// The kernel rejects an FDB entry whose VLAN is missing from the
			// bridge's self entry; setupVNI configures it before installing.
			Expect(ensurePortVlans(bridge, newTrunk([]uint16{internalVLANID}), withBridgeInterface())).To(Succeed())

			Expect(ensureLocalFDBEntry(bridge, internalVLANID, mac)).To(Succeed())
			Expect(hasLocalFDBEntry(bridge, internalVLANID, mac)).To(BeTrue(),
				"the anycast MAC must be installed as a local entry")
			Expect(ensureLocalFDBEntry(bridge, internalVLANID, mac)).To(Succeed(), "install must be idempotent")

			Expect(removeLocalFDBEntry(bridge, internalVLANID, mac)).To(Succeed())
			Expect(hasLocalFDBEntry(bridge, internalVLANID, mac)).To(BeFalse())
			return nil
		})).To(Succeed())
	})
})

func hasLocalFDBEntry(bridge *netlink.Bridge, vid uint16, mac []byte) bool {
	entries, err := netlink.NeighList(bridge.Index, unix.AF_BRIDGE)
	Expect(err).NotTo(HaveOccurred())
	for _, entry := range entries {
		if entry.Vlan == int(vid) && bytes.Equal(entry.HardwareAddr, mac) &&
			entry.State&netlink.NUD_PERMANENT != 0 {
			return true
		}
	}
	return false
}

// vtepDeviceInTestNS creates a dummy carrying vtepIP so an external VXLAN can
// use it as its VTEP source, mirroring the tunnel loopback of a real node.
func vtepDeviceInTestNS(vtepIP string) (netlink.Link, error) {
	const name = "vtepdummy"
	link, err := netlink.LinkByName(name)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		if err := netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}); err != nil {
			return nil, err
		}
		link, err = netlink.LinkByName(name)
	}
	if err != nil {
		return nil, err
	}
	if err := AssignIPToInterface(link, vtepIP); err != nil {
		return nil, err
	}
	if err := linkSetUp(link); err != nil {
		return nil, err
	}
	return link, nil
}
