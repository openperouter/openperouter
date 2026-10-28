// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

var _ = Describe("VLAN-aware anycast gateways", func() {
	var ns netns.NsHandle
	BeforeEach(func() { ns = createTestNS("gatewaytestns") })
	AfterEach(func() {
		cleanTest("gatewaytestns")
		Expect(ns.Close()).To(Succeed())
	})

	It("keeps gateway identity on a private macvlan and repairs its VLAN-scoped local FDB", func() {
		Expect(netnamespace.In(ns, func() error {
			Expect(setupVRF("testvrf")).To(Succeed())
			params := L2VNIParams{
				VNIParams:    VNIParams{VNI: 5000, VRF: "testvrf"},
				L2GatewayIPs: []string{"192.0.2.1/24", "2001:db8::1/64"},
			}
			bridge, err := setupBridge(params.VNIParams)
			Expect(err).NotTo(HaveOccurred())
			Expect(netlink.LinkSetMTU(bridge, 1450)).To(Succeed())
			endpoint, err := setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			gateway := endpoint.(*netlink.Macvlan)
			Expect(gateway.Mode).To(Equal(netlink.MACVLAN_MODE_PRIVATE))
			Expect(gateway.Name).To(Equal("br-pe-5000"))
			Expect(gateway.MTU).To(Equal(1450))
			svi, err := netlink.LinkByName("svi5000")
			Expect(err).NotTo(HaveOccurred())
			Expect(svi.(*netlink.Vlan).VlanId).To(Equal(1))
			Expect(svi.Attrs().ParentIndex).To(Equal(bridge.Index))
			Expect(gateway.ParentIndex).To(Equal(svi.Attrs().Index))
			vrf, err := lookupVRF("testvrf")
			Expect(err).NotTo(HaveOccurred())
			Expect(gateway.MasterIndex).To(Equal(vrf.Index))
			Expect(svi.Attrs().MasterIndex).To(Equal(vrf.Index))
			Expect(netlink.AddrList(svi, netlink.FAMILY_ALL)).To(BeEmpty())
			mac, err := BridgeFixedMAC(params.VNI)
			Expect(err).NotTo(HaveOccurred())
			Expect([]byte(gateway.HardwareAddr)).To(Equal(mac))
			Expect([]byte(svi.Attrs().HardwareAddr)).NotTo(Equal(mac))
			for _, address := range params.L2GatewayIPs {
				Expect(interfaceHasIP(gateway, address)).To(BeTrue())
			}
			entries, err := netlink.NeighList(bridge.Index, unix.AF_BRIDGE)
			Expect(err).NotTo(HaveOccurred())
			var local *netlink.Neigh
			for _, entry := range entries {
				if entry.Vlan == 1 && entry.HardwareAddr.String() == gateway.HardwareAddr.String() {
					local = &entry
				}
			}
			Expect(local).NotTo(BeNil())
			Expect(local.State & netlink.NUD_PERMANENT).NotTo(BeZero())
			local.Flags = netlink.NTF_SELF
			Expect(netlink.NeighDel(local)).To(Succeed())
			repeated, err := setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			Expect(repeated.Attrs().Index).To(Equal(gateway.Index))
			entries, err = netlink.NeighList(bridge.Index, unix.AF_BRIDGE)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(ContainElement(And(
				HaveField("Vlan", 1), HaveField("HardwareAddr", gateway.HardwareAddr),
			)))
			direct, err := setupVNIEndpoint(params.VNIParams, bridge)
			Expect(err).NotTo(HaveOccurred())
			Expect(direct).To(BeAssignableToTypeOf(&netlink.Vlan{}))
			Expect(netlink.AddrList(direct, netlink.FAMILY_ALL)).To(BeEmpty())
			_, err = netlink.LinkByName(vlanSVIName(params.VNI))
			Expect(err).To(BeAssignableToTypeOf(netlink.LinkNotFoundError{}))
			entries, err = netlink.NeighList(bridge.Index, unix.AF_BRIDGE)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).NotTo(ContainElement(And(
				HaveField("Vlan", 1), HaveField("HardwareAddr", gateway.HardwareAddr),
			)))

			return nil
		})).To(Succeed())
	})
	It("reconciles unchanged gateway links and local FDB without emitting updates", func() {
		Expect(netnamespace.In(ns, func() error {
			params := L2VNIParams{
				VNIParams: VNIParams{VNI: 6000}, L2GatewayIPs: []string{"192.0.2.1/24"},
			}
			bridge, err := setupBridge(params.VNIParams)
			Expect(err).NotTo(HaveOccurred())
			gateway, err := setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			svi, err := netlink.LinkByName(vlanSVIName(params.VNI))
			Expect(err).NotTo(HaveOccurred())
			owned := map[int]struct{}{
				bridge.Index: {}, gateway.Attrs().Index: {}, svi.Attrs().Index: {},
			}
			links := make(chan netlink.LinkUpdate, 32)
			neighbors := make(chan netlink.NeighUpdate, 32)
			done := make(chan struct{})
			defer close(done)
			Expect(netlink.LinkSubscribe(links, done)).To(Succeed())
			Expect(netlink.NeighSubscribe(neighbors, done)).To(Succeed())
			bridge, err = setupBridge(params.VNIParams)
			Expect(err).NotTo(HaveOccurred())
			repeated, err := setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			Expect(repeated.Attrs().Index).To(Equal(gateway.Attrs().Index))
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

})
