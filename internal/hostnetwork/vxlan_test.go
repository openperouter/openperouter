// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

var _ = Describe("Filtered external VXLAN", func() {
	var ns netns.NsHandle
	BeforeEach(func() {
		ns = createTestNS("vxlantestns")
		setupLoopback(ns)
	})
	AfterEach(func() {
		Expect(ns.Close()).To(Succeed())
		cleanTest("vxlantestns")
	})

	It("reconciles an unchanged tunnel without emitting link or neighbor updates", func() {
		Expect(netnamespace.In(ns, func() error {
			params := VNIParams{VNI: 5000, VTEPIP: "192.0.2.1/32", VXLanPort: new(int32(4789))}
			bridge, err := setupBridge(params)
			Expect(err).NotTo(HaveOccurred())
			Expect(setupVXLan(params, bridge)).To(Succeed())
			link, err := netlink.LinkByName(vxLanNameFromVNI(params.VNI))
			Expect(err).NotTo(HaveOccurred())
			owned := map[int]struct{}{bridge.Index: {}, link.Attrs().Index: {}}
			links := make(chan netlink.LinkUpdate, 32)
			neighbors := make(chan netlink.NeighUpdate, 32)
			done := make(chan struct{})
			defer close(done)
			Expect(netlink.LinkSubscribe(links, done)).To(Succeed())
			Expect(netlink.NeighSubscribe(neighbors, done)).To(Succeed())
			Expect(setupVXLan(params, bridge)).To(Succeed())
			Expect(netlink.LinkByName(link.Attrs().Name)).To(HaveField("LinkAttrs.Index", link.Attrs().Index))
			Consistently(func() bool {
				select {
				case update := <-links:
					_, exists := owned[update.Attrs().Index]
					return !exists
				case update := <-neighbors:
					_, exists := owned[update.LinkIndex]
					return !exists
				default:
					return true
				}
			}).WithTimeout(300 * time.Millisecond).WithPolling(10 * time.Millisecond).Should(BeTrue())
			return nil
		})).To(Succeed())
	})

	It("repairs detached bridge membership without recreating the tunnel", func() {
		Expect(netnamespace.In(ns, func() error {
			params := VNIParams{VNI: 5000, VTEPIP: "192.0.2.1/32", VXLanPort: new(int32(4789))}
			bridge, err := setupBridge(params)
			Expect(err).NotTo(HaveOccurred())
			Expect(setupVXLan(params, bridge)).To(Succeed())
			link, err := netlink.LinkByName(vxLanNameFromVNI(params.VNI))
			Expect(err).NotTo(HaveOccurred())
			index := link.Attrs().Index
			Expect(netlink.LinkSetNoMaster(link)).To(Succeed())
			Expect(setupVXLan(params, bridge)).To(Succeed())
			observed, err := netlink.LinkByName(link.Attrs().Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(observed.Attrs().Index).To(Equal(index))
			Expect(observed.Attrs().MasterIndex).To(Equal(bridge.Index))
			Expect(vxlanTunnelMappings(index)).To(Equal([]nl.TunnelInfo{{Vid: 1, TunId: uint32(params.VNI)}}))
			Expect(vxlanVNIFilterEnabled(index)).To(BeTrue())
			return nil
		})).To(Succeed())
	})

	It("keeps private VLAN mappings separate and repairs tunnel state without recreating links", func() {
		Expect(netnamespace.In(ns, func() error {
			for _, vni := range []int32{5000, 16777215} {
				params := VNIParams{VNI: vni, VTEPIP: "192.0.2.1/32", VXLanPort: new(int32(4789))}
				bridge, err := setupBridge(params)
				Expect(err).NotTo(HaveOccurred())
				Expect(setupVXLan(params, bridge)).To(Succeed())
				link, err := netlink.LinkByName(vxLanNameFromVNI(vni))
				Expect(err).NotTo(HaveOccurred())
				vxlan := link.(*netlink.Vxlan)
				index := vxlan.Index
				Expect(vxlan.FlowBased).To(BeTrue())
				Expect(vxlan.VxlanId).To(BeZero())
				Expect(vxlanVNIFilterEnabled(index)).To(BeTrue())
				Expect(vxlan.HardwareAddr).To(Equal(bridge.HardwareAddr))
				Expect(vxlanTunnelMappings(index)).To(Equal([]nl.TunnelInfo{{Vid: 1, TunId: uint32(vni)}}))

				Expect(netlink.BridgeVlanDelTunnelInfo(vxlan, 1, uint32(vni), false, false)).To(Succeed())
				_, err = vniFilterRequest(unix.RTM_DELTUNNEL, index,
					vniFilterRange{Start: uint32(vni), End: uint32(vni)}).Execute(unix.NETLINK_ROUTE, 0)
				Expect(err).NotTo(HaveOccurred())
				_, err = vniFilterRequest(unix.RTM_NEWTUNNEL, index,
					vniFilterRange{Start: 99, End: 99}).Execute(unix.NETLINK_ROUTE, 0)
				Expect(err).NotTo(HaveOccurred())
				Expect(setupVXLan(params, bridge)).To(Succeed())
				Expect(netlink.LinkByName(vxlan.Name)).To(HaveField("LinkAttrs.Index", index))
				Expect(vxlanTunnelMappings(index)).To(Equal([]nl.TunnelInfo{{Vid: 1, TunId: uint32(vni)}}))
				req := nl.NewNetlinkRequest(unix.RTM_GETTUNNEL, unix.NLM_F_DUMP)
				req.AddRawData(tunnelHeader(index))
				messages, err := req.Execute(unix.NETLINK_ROUTE, unix.RTM_NEWTUNNEL)
				Expect(err).NotTo(HaveOccurred())
				filters := make([]vniFilterRange, 0, len(messages))
				for _, message := range messages {
					entries, err := parseVNIFilters(message, index)
					Expect(err).NotTo(HaveOccurred())
					filters = append(filters, entries...)
				}
				Expect(filters).To(Equal([]vniFilterRange{{Start: uint32(vni), End: uint32(vni)}}))
				Expect(setupVXLan(params, bridge)).To(Succeed())
				Expect(netlink.LinkByName(vxlan.Name)).To(HaveField("LinkAttrs.Index", index))
				port, err := netlink.LinkGetProtinfo(vxlan)
				Expect(err).NotTo(HaveOccurred())
				Expect(port.Learning).To(BeFalse())
				Expect(port.NeighSuppress).To(BeTrue())
				Expect(port.VlanTunnel).To(BeTrue())
			}
			return nil
		})).To(Succeed())
	})
})
