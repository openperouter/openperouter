// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"net"
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

	It("keeps gateway neighbors on the VLAN SVI and repairs its VLAN-scoped local FDB", func() {
		Expect(netnamespace.In(ns, func() error {
			Expect(setupVRF("testvrf")).To(Succeed())
			params := L2VNIParams{
				VNIParams:    VNIParams{VNI: 5000, VRF: "testvrf"},
				L2GatewayIPs: []string{"192.0.2.1/24", "2001:db8::1/64"},
			}
			bridge, err := setupBridge(params.VNIParams)
			Expect(err).NotTo(HaveOccurred())
			routerMAC, err := net.ParseMAC("02:11:22:33:44:55")
			Expect(err).NotTo(HaveOccurred())
			Expect(netlink.LinkSetHardwareAddr(bridge, routerMAC)).To(Succeed())
			bridge.HardwareAddr = routerMAC
			Expect(netlink.LinkSetMTU(bridge, 1450)).To(Succeed())
			endpoint, err := setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoint).To(BeAssignableToTypeOf(&netlink.Vlan{}))
			gateway := endpoint.(*netlink.Vlan)
			Expect(gateway.VlanId).To(Equal(1))
			Expect(gateway.Name).To(Equal("br-pe-5000"))
			Expect(gateway.MTU).To(Equal(1450))
			Expect(gateway.ParentIndex).To(Equal(bridge.Index))
			vrf, err := lookupVRF("testvrf")
			Expect(err).NotTo(HaveOccurred())
			Expect(gateway.MasterIndex).To(Equal(vrf.Index))
			mac, err := BridgeFixedMAC(params.VNI)
			Expect(err).NotTo(HaveOccurred())
			Expect([]byte(gateway.HardwareAddr)).To(Equal(mac))
			Expect([]byte(bridge.HardwareAddr)).NotTo(Equal(mac))
			for _, address := range params.L2GatewayIPs {
				Expect(interfaceHasIP(gateway, address)).To(BeTrue())
			}
			linkLocal, err := netlink.ParseAddr("fe80::11:22ff:fe33:4455/64")
			Expect(err).NotTo(HaveOccurred())
			Expect(interfaceHasIP(gateway, linkLocal.String())).To(BeTrue())
			addresses, err := netlink.AddrList(gateway, netlink.FAMILY_V6)
			Expect(err).NotTo(HaveOccurred())
			Expect(addresses).To(ContainElement(And(
				HaveField("IPNet", linkLocal.IPNet), HaveField("Scope", unix.RT_SCOPE_LINK),
				HaveField("Flags", Satisfy(func(flags int) bool {
					return flags&(unix.IFA_F_TENTATIVE|unix.IFA_F_DADFAILED|unix.IFA_F_OPTIMISTIC) == 0
				})),
			)))
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
			Expect(netlink.AddrDel(gateway, linkLocal)).To(Succeed())
			repeated, err := setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			Expect(repeated.Attrs().Index).To(Equal(gateway.Index))
			Expect(interfaceHasIP(repeated, linkLocal.String())).To(BeTrue())
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
			owned := map[int]struct{}{
				bridge.Index: {}, gateway.Attrs().Index: {},
			}
			links := make(chan netlink.LinkUpdate, 32)
			neighbors := make(chan netlink.NeighUpdate, 32)
			addresses := make(chan netlink.AddrUpdate, 32)
			done := make(chan struct{})
			defer close(done)
			Expect(netlink.LinkSubscribe(links, done)).To(Succeed())
			Expect(netlink.NeighSubscribe(neighbors, done)).To(Succeed())
			Expect(netlink.AddrSubscribe(addresses, done)).To(Succeed())
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
				case update := <-addresses:
					_, exists := owned[update.LinkIndex]
					return !exists
				default:
					return true
				}
			}).WithTimeout(300 * time.Millisecond).WithPolling(10 * time.Millisecond).Should(BeTrue())
			return nil
		})).To(Succeed())
	})

	It("replaces a split SVI and macvlan gateway while preserving the switching bridge", func() {
		Expect(netnamespace.In(ns, func() error {
			params := L2VNIParams{
				VNIParams: VNIParams{VNI: 6001}, L2GatewayIPs: []string{"192.0.2.1/24"},
			}
			bridge, err := setupBridge(params.VNIParams)
			Expect(err).NotTo(HaveOccurred())
			svi, err := setupVLANEndpoint(params.VNIParams, bridge, vlanSVIName(params.VNI))
			Expect(err).NotTo(HaveOccurred())
			mac, err := BridgeFixedMAC(params.VNI)
			Expect(err).NotTo(HaveOccurred())
			gateway := &netlink.Macvlan{
				LinkAttrs: netlink.LinkAttrs{
					Name: BridgeName(params.VNI), ParentIndex: svi.Attrs().Index, HardwareAddr: mac,
				},
				Mode: netlink.MACVLAN_MODE_PRIVATE,
			}
			Expect(netlink.LinkAdd(gateway)).To(Succeed())
			Expect(AssignIPToInterface(gateway, params.L2GatewayIPs[0])).To(Succeed())
			endpoint, err := setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoint).To(BeAssignableToTypeOf(&netlink.Vlan{}))
			Expect(endpoint.Attrs().ParentIndex).To(Equal(bridge.Index))
			Expect([]byte(endpoint.Attrs().HardwareAddr)).To(Equal(mac))
			Expect(interfaceHasIP(endpoint, params.L2GatewayIPs[0])).To(BeTrue())
			Expect(netlink.LinkByName(bridge.Name)).To(HaveField("LinkAttrs.Index", bridge.Index))
			_, err = netlink.LinkByName(vlanSVIName(params.VNI))
			Expect(err).To(BeAssignableToTypeOf(netlink.LinkNotFoundError{}))
			return nil
		})).To(Succeed())
	})

	It("answers IPv6 neighbor reachability probes through the gateway SVI", func() {
		peerNS := createTestNS("gatewaypeerns")
		DeferCleanup(func() {
			Expect(netns.DeleteNamed("gatewaypeerns")).To(Succeed())
			Expect(peerNS.Close()).To(Succeed())
		})
		var gateway netlink.Link
		var peerMAC net.HardwareAddr
		target := net.ParseIP("2001:db8::10")
		Expect(netnamespace.In(ns, func() error {
			Expect(setupVRF("testvrf")).To(Succeed())
			params := L2VNIParams{
				VNIParams:    VNIParams{VNI: 6002, VRF: "testvrf"},
				L2GatewayIPs: []string{"2001:db8::1/64"},
			}
			bridge, err := setupBridge(params.VNIParams)
			Expect(err).NotTo(HaveOccurred())
			gateway, err = setupL2Gateway(params, bridge)
			Expect(err).NotTo(HaveOccurred())
			access := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "gw-access"}, PeerName: "gw-peer"}
			Expect(netlink.LinkAdd(access)).To(Succeed())
			Expect(netlink.LinkSetMaster(access, bridge)).To(Succeed())
			Expect(ensureAccessVLAN(access)).To(Succeed())
			peer, err := netlink.LinkByName("gw-peer")
			Expect(err).NotTo(HaveOccurred())
			peerMAC = peer.Attrs().HardwareAddr
			Expect(netlink.LinkSetNsFd(peer, int(peerNS))).To(Succeed())
			Expect(netlink.LinkSetUp(access)).To(Succeed())
			return nil
		})).To(Succeed())
		Expect(netnamespace.In(peerNS, func() error {
			peer, err := netlink.LinkByName("gw-peer")
			Expect(err).NotTo(HaveOccurred())
			address, err := netlink.ParseAddr("2001:db8::10/64")
			Expect(err).NotTo(HaveOccurred())
			address.Flags = unix.IFA_F_NODAD
			Expect(netlink.AddrAdd(peer, address)).To(Succeed())
			return netlink.LinkSetUp(peer)
		})).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			return netlink.NeighSet(&netlink.Neigh{
				LinkIndex: gateway.Attrs().Index, Family: unix.AF_INET6,
				IP: target, HardwareAddr: peerMAC, State: netlink.NUD_PROBE,
			})
		})).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(netnamespace.In(ns, func() error {
				neighbors, err := netlink.NeighList(gateway.Attrs().Index, netlink.FAMILY_V6)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(neighbors).To(ContainElement(And(
					HaveField("IP", target), HaveField("State", netlink.NUD_REACHABLE),
				)))
				return nil
			})).To(Succeed())
		}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
	})

})
