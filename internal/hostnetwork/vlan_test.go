// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

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
