// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var _ = Describe("VLAN-aware VNI lifecycle", func() {
	const namespace = "vnilifecycle"
	var ns netns.NsHandle
	var ovsClient *OVSClient
	var l3 L3VNIParams
	var l2 L2VNIParams
	BeforeEach(func() {
		ovsClient = &OVSClient{}
		DeferCleanup(ovsClient.Close)
		cleanTest(namespace)
		ns = createTestNS(namespace)
		setupLoopback(ns)
		common := VNIParams{
			TargetNS: fmt.Sprintf("/var/run/netns/%s", namespace), VRF: "testred",
			VTEPIP: "192.0.2.1/32", VXLanPort: new(int32(4789)),
		}
		l3 = L3VNIParams{VNIParams: common}
		l3.VNI = 8000
		l2 = L2VNIParams{VNIParams: common}
		l2.VNI = 8001
	})
	AfterEach(func() {
		Expect(ns.Close()).To(Succeed())
		cleanTest(namespace)
	})

	It("removes every owned fixed-ID tunnel before rebuilding mixed L3 and L2 VNIs", func() {
		Expect(netnamespace.In(ns, func() error {
			for _, vni := range []int32{l3.VNI, l2.VNI} {
				bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: BridgeName(vni)}}
				Expect(netlink.LinkAdd(bridge)).To(Succeed())
				lo, err := netlink.LinkByName(loopbackName)
				Expect(err).NotTo(HaveOccurred())
				legacy := &netlink.Vxlan{
					LinkAttrs: netlink.LinkAttrs{Name: vxLanNameFromVNI(vni), MasterIndex: bridge.Index},
					VxlanId:   int(vni), VtepDevIndex: lo.Attrs().Index,
					SrcAddr: net.ParseIP("192.0.2.1"), Port: 4789,
				}
				Expect(netlink.LinkAdd(legacy)).To(Succeed())
			}
			Expect(netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "unrelated"}})).To(Succeed())
			return nil
		})).To(Succeed())
		legacy, err := LegacyVNIIDs(l3.TargetNS)
		Expect(err).NotTo(HaveOccurred())
		Expect(legacy).To(ConsistOf(l3.VNI, l2.VNI))
		Expect(RemoveLegacyVNIs(l3.TargetNS, legacy)).To(Succeed())
		Expect(RemoveLegacyVNIs(l3.TargetNS, legacy)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			for _, vni := range legacy {
				_, err := netlink.LinkByName(vxLanNameFromVNI(vni))
				Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue())
				_, err = netlink.LinkByName(BridgeName(vni))
				Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue())
			}
			return nil
		})).To(Succeed())
		Expect(SetupL3VNI(context.Background(), l3)).To(Succeed())
		Expect(SetupL2VNI(context.Background(), ovsClient, l2)).To(Succeed())
		Expect(LegacyVNIIDs(l3.TargetNS)).To(BeEmpty())
		Expect(netnamespace.In(ns, func() error {
			for _, vni := range legacy {
				link, err := netlink.LinkByName(vxLanNameFromVNI(vni))
				Expect(err).NotTo(HaveOccurred())
				Expect(link.(*netlink.Vxlan).FlowBased).To(BeTrue())
				Expect(vxlanVNIFilterEnabled(link.Attrs().Index)).To(BeTrue())
			}
			Expect(netlink.LinkByName("unrelated")).To(HaveField("LinkAttrs.Name", "unrelated"))
			return nil
		})).To(Succeed())
	})

	It("repairs an external tunnel missing the immutable receive-filter flag", func() {
		Expect(SetupL3VNI(context.Background(), l3)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			link, err := netlink.LinkByName(vxLanNameFromVNI(l3.VNI))
			Expect(err).NotTo(HaveOccurred())
			original := link.(*netlink.Vxlan)
			Expect(netlink.LinkDel(original)).To(Succeed())
			unfiltered := &netlink.Vxlan{
				LinkAttrs: netlink.LinkAttrs{Name: original.Name, MasterIndex: original.MasterIndex},
				FlowBased: true, VtepDevIndex: original.VtepDevIndex, SrcAddr: original.SrcAddr, Port: original.Port,
			}
			Expect(netlink.LinkAdd(unfiltered)).To(Succeed())
			Expect(vxlanVNIFilterEnabled(unfiltered.Index)).To(BeFalse())
			return nil
		})).To(Succeed())
		Expect(SetupL3VNI(context.Background(), l3)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			link, err := netlink.LinkByName(vxLanNameFromVNI(l3.VNI))
			Expect(err).NotTo(HaveOccurred())
			Expect(vxlanVNIFilterEnabled(link.Attrs().Index)).To(BeTrue())
			return nil
		})).To(Succeed())
	})

	It("cleans partially deleted gateway topology repeatedly while preserving configured VNIs", func() {
		Expect(SetupL3VNI(context.Background(), l3)).To(Succeed())
		l2.L2GatewayIPs = []string{"198.51.100.1/24"}
		Expect(SetupL2VNI(context.Background(), ovsClient, l2)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			svi, err := netlink.LinkByName(BridgeName(l2.VNI))
			Expect(err).NotTo(HaveOccurred())
			return netlink.LinkDel(svi)
		})).To(Succeed())
		for range 2 {
			Expect(RemoveNonConfiguredVNIs(ovsClient, l3.TargetNS, []VNIParams{l3.VNIParams})).To(Succeed())
		}
		Expect(netnamespace.In(ns, func() error {
			Expect(netlink.LinkByName(vxLanNameFromVNI(l3.VNI))).Error().NotTo(HaveOccurred())
			for _, name := range []string{BridgeName(l2.VNI), vlanSVIName(l2.VNI),
				vlanBridgeName(l2.VNI), vxLanNameFromVNI(l2.VNI), vethNamesFromVNI(l2.VNI).NamespaceSide} {
				_, err := netlink.LinkByName(name)
				Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), name)
			}
			return nil
		})).To(Succeed())
		for range 2 {
			Expect(RemoveAllVNIs(ovsClient, l3.TargetNS)).To(Succeed())
		}
	})

	It("retries a partially deleted gateway and reconstructs the same configuration in a recreated namespace", func() {
		l2.L2GatewayIPs = []string{"198.51.100.1/24", "2001:db8::1/64"}
		Expect(SetupL3VNI(context.Background(), l3)).To(Succeed())
		Expect(SetupL2VNI(context.Background(), ovsClient, l2)).To(Succeed())
		hostName := vethNamesFromVNI(l2.VNI).HostSide
		host, err := netlink.LinkByName(hostName)
		Expect(err).NotTo(HaveOccurred())
		hostIndex := host.Attrs().Index
		indices := map[string]int{}
		Expect(netnamespace.In(ns, func() error {
			for _, vni := range []int32{l3.VNI, l2.VNI} {
				for _, name := range []string{vlanBridgeName(vni), vxLanNameFromVNI(vni)} {
					link, err := netlink.LinkByName(name)
					Expect(err).NotTo(HaveOccurred())
					indices[name] = link.Attrs().Index
				}
			}
			svi, err := netlink.LinkByName(BridgeName(l2.VNI))
			Expect(err).NotTo(HaveOccurred())
			Expect(netlink.LinkDel(svi)).To(Succeed())
			_, err = netlink.LinkByName(BridgeName(l2.VNI))
			Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue())
			return nil
		})).To(Succeed())
		Expect(SetupL2VNI(context.Background(), ovsClient, l2)).To(Succeed())
		Expect(netlink.LinkByName(hostName)).To(HaveField("LinkAttrs.Index", hostIndex))
		Expect(netnamespace.In(ns, func() error {
			for name, index := range indices {
				Expect(netlink.LinkByName(name)).To(HaveField("LinkAttrs.Index", index))
			}
			validateLifecycleGateway(l2)
			return nil
		})).To(Succeed())

		Expect(ns.Close()).To(Succeed())
		Expect(netns.DeleteNamed(namespace)).To(Succeed())
		// Namespace teardown runs asynchronously; its old veth pair must disappear
		// before setup can create a pair for the replacement namespace.
		Eventually(func() bool {
			_, err := netlink.LinkByName(hostName)
			return errors.As(err, &netlink.LinkNotFoundError{})
		}).WithTimeout(5 * time.Second).WithPolling(time.Millisecond).Should(BeTrue())
		ns = createTestNS(namespace)
		setupLoopback(ns)
		Expect(SetupL3VNI(context.Background(), l3)).To(Succeed())
		Expect(SetupL2VNI(context.Background(), ovsClient, l2)).To(Succeed())
		host, err = netlink.LinkByName(hostName)
		Expect(err).NotTo(HaveOccurred())
		Expect(host.Type()).To(Equal("veth"))
		Expect(netnamespace.In(ns, func() error {
			validateLifecycleGateway(l2)
			endpoint, err := netlink.LinkByName(BridgeName(l3.VNI))
			Expect(err).NotTo(HaveOccurred())
			vrf, err := lookupVRF(l3.VRF)
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoint.Attrs().MasterIndex).To(Equal(vrf.Index))
			Expect(netlink.AddrList(endpoint, netlink.FAMILY_ALL)).To(BeEmpty())
			pe, err := netlink.LinkByName(vethNamesFromVNI(l2.VNI).NamespaceSide)
			Expect(err).NotTo(HaveOccurred())
			bridge, err := netlink.LinkByName(vlanBridgeName(l2.VNI))
			Expect(err).NotTo(HaveOccurred())
			Expect(pe.Attrs().MasterIndex).To(Equal(bridge.Attrs().Index))
			return nil
		})).To(Succeed())
	})
})

func validateLifecycleGateway(params L2VNIParams) {
	gateway, err := netlink.LinkByName(BridgeName(params.VNI))
	Expect(err).NotTo(HaveOccurred())
	Expect(gateway.Type()).To(Equal("vlan"))
	mac, err := BridgeFixedMAC(params.VNI)
	Expect(err).NotTo(HaveOccurred())
	Expect(gateway.Attrs().HardwareAddr).To(Equal(net.HardwareAddr(mac)))
	addresses, err := netlink.AddrList(gateway, netlink.FAMILY_ALL)
	Expect(err).NotTo(HaveOccurred())
	cidrs := make([]string, 0, len(addresses))
	for _, address := range addresses {
		cidrs = append(cidrs, address.IPNet.String())
	}
	Expect(cidrs).To(ConsistOf(params.L2GatewayIPs))
	vrf, err := lookupVRF(params.VRF)
	Expect(err).NotTo(HaveOccurred())
	Expect(gateway.Attrs().MasterIndex).To(Equal(vrf.Index))
	bridge, err := netlink.LinkByName(vlanBridgeName(params.VNI))
	Expect(err).NotTo(HaveOccurred())
	Expect(gateway.Attrs().ParentIndex).To(Equal(bridge.Attrs().Index))
	Expect(gateway.(*netlink.Vlan).VlanId).To(Equal(1))
	tunnel, err := netlink.LinkByName(vxLanNameFromVNI(params.VNI))
	Expect(err).NotTo(HaveOccurred())
	Expect(tunnel.Attrs().MasterIndex).To(Equal(bridge.Attrs().Index))
	Expect(vxlanVNIFilterEnabled(tunnel.Attrs().Index)).To(BeTrue())
}
