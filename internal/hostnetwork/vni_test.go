// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const testNSName = "vnitestns"

var _ = Describe("VNI configuration", func() {
	var testNS netns.NsHandle

	BeforeEach(func() {
		cleanTest(testNSName)
		testNS = createTestNS(testNSName)
		setupLoopback(testNS)
	})
	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should work with a single VNI", func() {
		params := VNIParams{
			VRF:        "testred",
			TargetNS:   testNSName,
			VTEPIP:     "192.170.0.9/32",
			VethHostIP: "192.168.9.1/32",
			VethNSIP:   "192.168.9.0/32",
			VNI:        100,
			VXLanPort:  4789,
		}

		err := SetupVNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateHostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateNS(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with multiple vnis + cleanup", func() {
		params := []VNIParams{
			{

				VRF:        "testred",
				TargetNS:   testNSName,
				VTEPIP:     "192.170.0.9/32",
				VethHostIP: "192.168.9.1/32",
				VethNSIP:   "192.168.9.0/32",
				VNI:        100,
				VXLanPort:  4789,
			},
			{
				VRF:        "testblue",
				TargetNS:   testNSName,
				VTEPIP:     "192.170.0.10/32",
				VethHostIP: "192.168.9.2/32",
				VethNSIP:   "192.168.9.3/32",
				VNI:        101,
				VXLanPort:  4789,
			},
		}
		for _, p := range params {
			err := SetupVNI(context.Background(), p)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				validateHostLeg(g, p)
				_ = inNamespace(testNS, func() error {
					validateNS(g, p)
					return nil
				})
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		}

		remaining := params[0]
		toDelete := params[1]

		err := RemoveNonConfiguredVNIs(testNS, []VNIParams{remaining})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateHostLeg(g, remaining)
			_ = inNamespace(testNS, func() error {
				validateNS(g, remaining)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		hostSide, _ := vethNamesFromVRF(toDelete.VRF)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, hostSide)
			validateVNIIsNotConfigured(g, toDelete)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should be idempotent", func() {
		params := VNIParams{
			VRF:        "testred",
			TargetNS:   testNSName,
			VTEPIP:     "192.170.0.9/32",
			VethHostIP: "192.168.9.1/32",
			VethNSIP:   "192.168.9.0/32",
			VNI:        100,
			VXLanPort:  4789,
		}

		err := SetupVNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		err = SetupVNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateHostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateNS(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

	})
})

func validateHostLeg(g Gomega, params VNIParams) {
	hostSide, _ := vethNamesFromVRF(params.VRF)
	hostLegLink, err := netlink.LinkByName(hostSide)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(hostLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
	hasIP, err := interfaceHasIP(hostLegLink, params.VethHostIP)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasIP).To(BeTrue(), "host leg does not have ip", params.VethHostIP)
}

func validateNS(g Gomega, params VNIParams) {
	loopback, err := netlink.LinkByName(UnderlayLoopback)
	g.Expect(err).NotTo(HaveOccurred(), "loopback not found", UnderlayLoopback)

	vxlanLink, err := netlink.LinkByName(vxLanName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred())

	vxlan := vxlanLink.(*netlink.Vxlan)
	g.Expect(vxlan.OperState).To(BeEquivalentTo(netlink.OperUnknown))

	addrGenModeNone := checkAddrGenModeNone(vxlan)
	g.Expect(addrGenModeNone).To(BeTrue())

	vrfLink, err := netlink.LinkByName(params.VRF)
	g.Expect(err).NotTo(HaveOccurred())

	vrf := vrfLink.(*netlink.Vrf)
	g.Expect(vrf.OperState).To(BeEquivalentTo(netlink.OperUp))

	bridgeLink, err := netlink.LinkByName(bridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred())

	bridge := bridgeLink.(*netlink.Bridge)
	g.Expect(bridge.OperState).To(BeEquivalentTo(netlink.OperUp))

	g.Expect(bridge.MasterIndex).To(Equal(vrf.Index))

	addrGenModeNone = checkAddrGenModeNone(bridge)
	g.Expect(addrGenModeNone).To(BeTrue())

	err = checkVXLanConfigured(vxlan, bridge.Index, loopback.Attrs().Index, params)
	g.Expect(err).NotTo(HaveOccurred())

	_, peSide := vethNamesFromVRF(params.VRF)
	peLegLink, err := netlink.LinkByName(peSide)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
	g.Expect(peLegLink.Attrs().MasterIndex).To(Equal(vrf.Index))

	hasIP, err := interfaceHasIP(peLegLink, params.VethNSIP)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasIP).To(BeTrue())
}

func checkLinkdeleted(g Gomega, name string) {
	_, err := netlink.LinkByName(name)
	g.Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "link not deleted", name, err)
}

func validateVNIIsNotConfigured(g Gomega, params VNIParams) {

	checkLinkdeleted(g, vxLanName(params.VNI))
	checkLinkdeleted(g, params.VRF)
	checkLinkdeleted(g, bridgeName(params.VNI))

	_, peSide := vethNamesFromVRF(params.VRF)
	checkLinkdeleted(g, peSide)
}

func checkAddrGenModeNone(l netlink.Link) bool {
	fileName := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/addr_gen_mode", l.Attrs().Name)
	addrGenMode, err := os.ReadFile(fileName)
	Expect(err).NotTo(HaveOccurred())

	return strings.Trim(string(addrGenMode), "\n") == "1"
}

func setupLoopback(ns netns.NsHandle) {
	_ = inNamespace(ns, func() error {
		_, err := netlink.LinkByName(UnderlayLoopback)
		if errors.As(err, &netlink.LinkNotFoundError{}) {
			loopback := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: UnderlayLoopback}}
			err = netlink.LinkAdd(loopback)
			Expect(err).NotTo(HaveOccurred(), "failed to create loopback", UnderlayLoopback)
		}
		return nil
	})
}
