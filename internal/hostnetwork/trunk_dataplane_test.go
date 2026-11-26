// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
)

var _ = Describe("L2Trunk dataplane", func() {
	const (
		nsName         = "trunkdpns"
		trunkNamespace = "tenant-a"
		trunkName      = "workloads"
		trunkVRF       = "testred"
	)

	var (
		ns        netns.NsHandle
		ovsClient *OVSClient
		cfg       *VNIConfigurator
		names     TrunkDeviceNames
	)

	trunkParams := func() L2TrunkParams {
		return L2TrunkParams{
			Name:      trunkName,
			Namespace: trunkNamespace,
			TargetNS:  "/var/run/netns/" + nsName,
			VTEPIP:    "192.170.0.9/32",
			VXLanPort: 4789,
		}
	}

	trunkedVNI := func(name string, tag, vni int32, gatewayIPs []string) L2VNIParams {
		return L2VNIParams{
			VNIParams: VNIParams{
				VRF:       trunkVRF,
				TargetNS:  "/var/run/netns/" + nsName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       vni,
				VXLanPort: new(int32(4789)),
			},
			Name:         name,
			L2GatewayIPs: gatewayIPs,
			Trunk: &L2VNITrunkParams{
				TrunkID: TrunkID(trunkNamespace, trunkName),
				Name:    trunkName,
				VLAN:    tag,
			},
		}
	}

	BeforeEach(func() {
		cleanTest(nsName)
		ns = createTestNS(nsName)
		setupTunnelLoopback(ns)
		ovsClient = &OVSClient{}
		var err error
		cfg, err = NewVNIConfigurator(ns, ovsClient)
		Expect(err).NotTo(HaveOccurred())
		names = TrunkDeviceNamesFor(trunkNamespace, trunkName)
	})

	AfterEach(func() {
		cleanTest(nsName)
		ovsClient.Close()
	})

	inNS := func(fn func() error) {
		Expect(netnamespace.In(ns, fn)).To(Succeed())
	}

	memberships := func(link netlink.Link) []vlanMembership {
		var got []vlanMembership
		inNS(func() error {
			var err error
			got, err = portVlanMemberships(link)
			return err
		})
		return got
	}

	It("creates all four devices for a trunk with no L2VNIs", func() {
		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())

		hostLink, err := netlink.LinkByName(names.HostSide)
		Expect(err).NotTo(HaveOccurred())
		Expect(hostLink.Type()).To(Equal(VethLinkType))
		Expect(hostLink.Attrs().Alias).To(Equal(trunkNamespace + "/" + trunkName))

		inNS(func() error {
			bridgeLink, err := netlink.LinkByName(names.Bridge)
			Expect(err).NotTo(HaveOccurred())
			bridge := bridgeLink.(*netlink.Bridge)
			Expect(bridge.VlanFiltering).NotTo(BeNil())
			Expect(*bridge.VlanFiltering).To(BeTrue())
			Expect(bridge.VlanDefaultPVID).NotTo(BeNil())
			Expect(*bridge.VlanDefaultPVID).To(Equal(uint16(0)))
			Expect(bridge.MasterIndex).To(BeZero())

			routerLink, err := netlink.LinkByName(names.NamespaceSide)
			Expect(err).NotTo(HaveOccurred())
			Expect(routerLink.Attrs().MasterIndex).To(Equal(bridge.Index))

			svdLink, err := netlink.LinkByName(names.VXLan)
			Expect(err).NotTo(HaveOccurred())
			svd := svdLink.(*netlink.Vxlan)
			Expect(svd.FlowBased).To(BeTrue())
			Expect(svd.VxlanId).To(BeZero())
			Expect(svd.MasterIndex).To(Equal(bridge.Index))
			enabled, err := vniFilterEnabled(svd.Index)
			Expect(err).NotTo(HaveOccurred())
			Expect(enabled).To(BeTrue())
			Expect(checkVXLanPostSetup(svd)).To(Succeed())
			return nil
		})

		for _, link := range []netlink.Link{hostLink} {
			mode, err := checkAddrGenModeNone(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(mode).To(BeTrue(), "%s must not autogenerate a link-local", link.Attrs().Name)
		}
		for _, membership := range memberships(hostLink) {
			Expect(membership.PVID).To(BeFalse(), "no trunk port may carry a PVID")
		}
	})

	It("attaches the host leg to an external bridge", func() {
		const hostBridge = "trunkhostbr"
		createLinuxBridge(hostBridge)

		params := trunkParams()
		params.HostMaster = &HostMaster{Name: new(hostBridge), Type: BridgeLinkType}
		Expect(cfg.SetupL2Trunk(context.Background(), params)).To(Succeed())

		hostLink, err := netlink.LinkByName(names.HostSide)
		Expect(err).NotTo(HaveOccurred())
		master, err := netlink.LinkByName(hostBridge)
		Expect(err).NotTo(HaveOccurred())
		Expect(hostLink.Attrs().MasterIndex).To(Equal(master.Attrs().Index))
	})

	It("is idempotent and does not recreate the veth", func() {
		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())
		hostLink, err := netlink.LinkByName(names.HostSide)
		Expect(err).NotTo(HaveOccurred())
		var routerIndex int
		inNS(func() error {
			routerLink, err := netlink.LinkByName(names.NamespaceSide)
			if err != nil {
				return err
			}
			routerIndex = routerLink.Attrs().Index
			return nil
		})

		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())

		hostAfter, err := netlink.LinkByName(names.HostSide)
		Expect(err).NotTo(HaveOccurred())
		Expect(hostAfter.Attrs().Index).To(Equal(hostLink.Attrs().Index))
		inNS(func() error {
			routerAfter, err := netlink.LinkByName(names.NamespaceSide)
			if err != nil {
				return err
			}
			Expect(routerAfter.Attrs().Index).To(Equal(routerIndex))
			return nil
		})
	})

	It("recreates the veth when only the host leg survives", func() {
		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())

		hostLink, err := netlink.LinkByName(names.HostSide)
		Expect(err).NotTo(HaveOccurred())
		Expect(netlink.LinkDel(hostLink)).To(Succeed())

		orphan := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{Name: names.HostSide},
			PeerName:  "trunkorphan",
		}
		Expect(netlink.LinkAdd(orphan)).To(Succeed())

		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())

		var peerIndex int
		inNS(func() error {
			routerLink, err := netlink.LinkByName(names.NamespaceSide)
			if err != nil {
				return err
			}
			peerIndex, err = netlink.VethPeerIndex(routerLink.(*netlink.Veth))
			return err
		})
		hostAfter, err := netlink.LinkByIndex(peerIndex)
		Expect(err).NotTo(HaveOccurred())
		Expect(hostAfter.Attrs().Name).To(Equal(names.HostSide))
		_, err = netlink.LinkByName("trunkorphan")
		Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue())
	})

	It("maps two L2VNIs to their tags on one shared trunk", func() {
		createVRFInNamespace(ns, trunkVRF)
		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())

		first := trunkedVNI("first", 100, 10100, []string{"192.168.10.1/24"})
		second := trunkedVNI("second", 200, 10200, nil)
		Expect(cfg.SetupL2TrunkVNI(context.Background(), first)).To(Succeed())
		Expect(cfg.SetupL2TrunkVNI(context.Background(), second)).To(Succeed())

		Eventually(func(g Gomega) {
			inNS(func() error {
				bridgeLink, err := netlink.LinkByName(names.Bridge)
				g.Expect(err).NotTo(HaveOccurred())
				bridge := bridgeLink.(*netlink.Bridge)
				routerLink, err := netlink.LinkByName(names.NamespaceSide)
				g.Expect(err).NotTo(HaveOccurred())
				svdLink, err := netlink.LinkByName(names.VXLan)
				g.Expect(err).NotTo(HaveOccurred())

				firstEndpoint, err := netlink.LinkByName(BridgeName(first.VNI))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(firstEndpoint).To(BeAssignableToTypeOf(&netlink.Vlan{}))
				vlan := firstEndpoint.(*netlink.Vlan)
				g.Expect(vlan.VlanId).To(Equal(100))
				g.Expect(vlan.ParentIndex).To(Equal(bridge.Index))

				secondEndpoint, err := netlink.LinkByName(BridgeName(second.VNI))
				g.Expect(err).NotTo(HaveOccurred())
				vlan = secondEndpoint.(*netlink.Vlan)
				g.Expect(vlan.VlanId).To(Equal(200))
				g.Expect(vlan.ParentIndex).To(Equal(bridge.Index))

				hasIP, err := interfaceHasIP(firstEndpoint, "192.168.10.1/24")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(hasIP).To(BeTrue())
				vrfLink, err := netlink.LinkByName(trunkVRF)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(firstEndpoint.Attrs().MasterIndex).To(Equal(vrfLink.Attrs().Index))
				mac, err := BridgeFixedMAC(first.VNI)
				g.Expect(err).NotTo(HaveOccurred())
				validateLocalFDB(g, bridge, 100, mac)

				hasNoIP, err := interfaceHasNoIP(secondEndpoint, netlink.FAMILY_ALL)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(hasNoIP).To(BeTrue())
				g.Expect(secondEndpoint.Attrs().MasterIndex).To(BeZero())
				Expect(sysctl.Read(sysctl.DisableIPv4Forwarding(secondEndpoint.Attrs().Name))).To(Equal("0"))

				g.Expect(vxlanTunnelMappings(svdLink.Attrs().Index)).To(ConsistOf(
					nl.TunnelInfo{Vid: 100, TunId: 10100},
					nl.TunnelInfo{Vid: 200, TunId: 10200},
				))
				g.Expect(vniFilterEntries(svdLink.Attrs().Index)).To(ConsistOf(
					vniFilterRange{Start: 10100, End: 10100},
					vniFilterRange{Start: 10200, End: 10200},
				))
				g.Expect(portVlanMemberships(routerLink)).To(ConsistOf(
					vlanMembership{VID: 100},
					vlanMembership{VID: 200},
				))
				g.Expect(portVlanMemberships(bridge)).To(ConsistOf(
					vlanMembership{VID: 100},
					vlanMembership{VID: 200},
				))
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("removes one L2VNI and leaves the other intact", func() {
		createVRFInNamespace(ns, trunkVRF)
		params := trunkParams()
		Expect(cfg.SetupL2Trunk(context.Background(), params)).To(Succeed())

		first := trunkedVNI("first", 100, 10100, []string{"192.168.10.1/24"})
		second := trunkedVNI("second", 200, 10200, nil)
		Expect(cfg.SetupL2TrunkVNI(context.Background(), first)).To(Succeed())
		Expect(cfg.SetupL2TrunkVNI(context.Background(), second)).To(Succeed())

		Expect(RemoveNonConfiguredTrunks("/var/run/netns/"+nsName,
			[]L2TrunkParams{params}, []L2VNIParams{first})).To(Succeed())

		inNS(func() error {
			_, err := netlink.LinkByName(BridgeName(first.VNI))
			Expect(err).NotTo(HaveOccurred())
			_, err = netlink.LinkByName(BridgeName(second.VNI))
			Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue())

			svdLink, err := netlink.LinkByName(names.VXLan)
			Expect(err).NotTo(HaveOccurred())
			Expect(vxlanTunnelMappings(svdLink.Attrs().Index)).To(ConsistOf(
				nl.TunnelInfo{Vid: 100, TunId: 10100}))
			Expect(vniFilterEntries(svdLink.Attrs().Index)).To(ConsistOf(
				vniFilterRange{Start: 10100, End: 10100}))

			bridgeLink, err := netlink.LinkByName(names.Bridge)
			Expect(err).NotTo(HaveOccurred())
			Expect(portVlanMemberships(bridgeLink)).To(ConsistOf(vlanMembership{VID: 100}))
			return nil
		})
	})

	It("removes every device of a deleted trunk", func() {
		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())

		Expect(RemoveNonConfiguredTrunks("/var/run/netns/"+nsName, nil, nil)).To(Succeed())

		_, err := netlink.LinkByName(names.HostSide)
		Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue())
		inNS(func() error {
			for _, name := range []string{names.NamespaceSide, names.Bridge, names.VXLan} {
				_, err := netlink.LinkByName(name)
				Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "link %s not deleted", name)
			}
			return nil
		})
	})

	It("relocates an L2VNI between standalone and trunk-attached", func() {
		const vni = 10100
		standalone := L2VNIParams{
			VNIParams: VNIParams{
				TargetNS:  "/var/run/netns/" + nsName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       vni,
				VXLanPort: new(int32(4789)),
			},
			Name: "relocated",
		}
		params := trunkParams()
		trunked := trunkedVNI("relocated", 100, vni, nil)

		By("starting from a standalone L2VNI")
		Expect(cfg.SetupL2VNI(context.Background(), standalone)).To(Succeed())

		By("moving it onto the trunk on the first reconcile")
		Expect(cfg.SetupL2Trunk(context.Background(), params)).To(Succeed())
		Expect(ReclaimVNIs("/var/run/netns/"+nsName, []L2VNIParams{trunked})).To(Succeed())
		Expect(cfg.SetupL2TrunkVNI(context.Background(), trunked)).To(Succeed())
		Expect(RemoveNonConfiguredVNIs(ovsClient, "/var/run/netns/"+nsName, nil)).To(Succeed())
		Expect(RemoveNonConfiguredTrunks("/var/run/netns/"+nsName,
			[]L2TrunkParams{params}, []L2VNIParams{trunked})).To(Succeed())

		inNS(func() error {
			for _, name := range []string{vxLanNameFromVNI(vni), switchingBridgeName(vni), vethNamesFromVNI(vni).NamespaceSide} {
				_, err := netlink.LinkByName(name)
				Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "standalone device %s not deleted", name)
			}
			endpoint, err := netlink.LinkByName(BridgeName(vni))
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoint.(*netlink.Vlan).VlanId).To(Equal(100))
			return nil
		})
		_, err := netlink.LinkByName(vethNamesFromVNI(vni).HostSide)
		Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "standalone host veth not deleted")

		By("moving it back to standalone on the first reconcile")
		Expect(RemoveNonConfiguredTrunks("/var/run/netns/"+nsName,
			[]L2TrunkParams{params}, nil)).To(Succeed())
		Expect(ReclaimVNIs("/var/run/netns/"+nsName, []L2VNIParams{standalone})).To(Succeed())
		Expect(cfg.SetupL2VNI(context.Background(), standalone)).To(Succeed())

		inNS(func() error {
			for _, name := range []string{vxLanNameFromVNI(vni), switchingBridgeName(vni), BridgeName(vni)} {
				_, err := netlink.LinkByName(name)
				Expect(err).NotTo(HaveOccurred(), "standalone device %s not recreated", name)
			}
			return nil
		})
	})

	It("reports the host legs to detach only for trunks that are gone", func() {
		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams())).To(Succeed())
		id := TrunkID(trunkNamespace, trunkName)

		kept, err := trunkHostPortsToDetach(map[string]struct{}{id: {}})
		Expect(err).NotTo(HaveOccurred())
		Expect(kept).To(BeEmpty(), "a configured trunk keeps its host leg")

		gone, err := trunkHostPortsToDetach(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(gone).To(HaveKey(id), "a deleted trunk exposes its host leg for cleanup")
	})
})
