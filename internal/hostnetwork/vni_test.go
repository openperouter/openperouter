// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/aws/smithy-go/ptr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const testNSName = "vnitestns"

var _ = Describe("L3 VNI configuration", func() {
	var testNS netns.NsHandle

	BeforeEach(func() {
		cleanTest(testNSName)
		testNS = createTestNS(testNSName)
		setupLoopback(testNS)
	})
	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should work with IPv4 only L3VNI", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: 4789,
			},
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3HostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with IPv6 only L3VNI", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: 4789,
			},
			HostVeth: &Veth{
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3HostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with dual-stack L3VNI", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: 4789,
			},
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3HostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with multiple L3VNIs + cleanup", func() {
		params := []L3VNIParams{
			{
				VNIParams: VNIParams{
					VRF:       "testred",
					TargetNS:  testNSName,
					VTEPIP:    "192.170.0.9/32",
					VNI:       100,
					VXLanPort: 4789,
				},
				HostVeth: &Veth{
					HostIPv4: "192.168.9.1/32",
					NSIPv4:   "192.168.9.0/32",
				},
			},
			{
				VNIParams: VNIParams{
					VRF:       "testblue",
					TargetNS:  testNSName,
					VTEPIP:    "192.170.0.10/32",
					VNI:       101,
					VXLanPort: 4789,
				},
				HostVeth: &Veth{
					HostIPv4: "192.168.9.2/32",
					NSIPv4:   "192.168.9.3/32",
				},
			},
		}
		for _, p := range params {
			err := SetupL3VNI(context.Background(), p)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				validateL3HostLeg(g, p)
				_ = inNamespace(testNS, func() error {
					validateL3VNI(g, p)
					return nil
				})
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		}

		remaining := params[0]
		toDelete := params[1]

		By("removing non configured L3VNIs")
		err := RemoveNonConfiguredVNIs(testNSName, []VNIParams{remaining.VNIParams})
		Expect(err).NotTo(HaveOccurred())

		By("checking remaining L3VNIs")
		Eventually(func(g Gomega) {
			validateL3HostLeg(g, remaining)
			_ = inNamespace(testNS, func() error {
				validateL3VNI(g, remaining)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("checking non needed L3VNIs are removed")
		hostSide, _ := vethNamesFromVNI(toDelete.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, hostSide)
			_ = inNamespace(testNS, func() error {
				validateVNIIsNotConfigured(g, toDelete.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should be idempotent", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: 4789,
			},
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		err = SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3HostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

	})

	It("should configure VXLAN and VRF when HostVeth is nil", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: 4789,
			},
			HostVeth: nil,
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			_ = inNamespace(testNS, func() error {
				validateVNI(g, params.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		// Verify that no host veth was created
		hostSide, _ := vethNamesFromVNI(params.VNI)
		_, err = netlink.LinkByName(hostSide)
		Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "host veth should not exist when HostVeth is nil")
	})
})

var _ = Describe("L2 VNI configuration", func() {
	var testNS netns.NsHandle
	const bridgeName = "testbridge"

	BeforeEach(func() {
		cleanTest(testNSName)
		testNS = createTestNS(testNSName)
		setupLoopback(testNS)
		createLinuxBridge(bridgeName)
	})
	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should work with a single L2VNI", func() {
		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: 4789,
			},
			L2GatewayIP: ptr.String("192.168.1.0/24"),
			HostMaster: &HostMaster{
				Name: bridgeName,
			},
		}

		err := SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateL2VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("removing the VNI")
		err = RemoveNonConfiguredVNIs(testNSName, []VNIParams{})
		Expect(err).NotTo(HaveOccurred())

		By("checking the VNI is removed")
		hostSide, _ := vethNamesFromVNI(params.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, hostSide)
			checkLinkExists(g, bridgeName)

			_ = inNamespace(testNS, func() error {
				validateVNIIsNotConfigured(g, params.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

	})

	It("should work with multiple L2VNIs + cleanup", func() {
		params := []L2VNIParams{
			{
				VNIParams: VNIParams{
					VRF:       "testred",
					TargetNS:  testNSName,
					VTEPIP:    "192.170.0.9/32",
					VNI:       100,
					VXLanPort: 4789,
				},
				L2GatewayIP: ptr.String("192.168.1.0/24"),
				HostMaster: &HostMaster{
					Name: bridgeName,
				},
			},
			{
				VNIParams: VNIParams{
					VRF:       "testblue",
					TargetNS:  testNSName,
					VTEPIP:    "192.170.0.10/32",
					VNI:       101,
					VXLanPort: 4789,
				},
				L2GatewayIP: ptr.String("192.168.1.0/24"),
				HostMaster: &HostMaster{
					AutoCreate: true,
				},
			},
		}
		for _, p := range params {
			err := SetupL2VNI(context.Background(), p)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				validateL2HostLeg(g, p)
				_ = inNamespace(testNS, func() error {
					validateL2VNI(g, p)
					return nil
				})
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		}

		remaining := params[0]
		toDelete := params[1]

		By("removing non configured L2VNIs")
		err := RemoveNonConfiguredVNIs(testNSName, []VNIParams{remaining.VNIParams})
		Expect(err).NotTo(HaveOccurred())

		By("checking remaining L2VNIs")

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, remaining)
			_ = inNamespace(testNS, func() error {
				validateL2VNI(g, remaining)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("checking non needed L2VNIs are removed")
		hostSide, _ := vethNamesFromVNI(toDelete.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, hostSide)
			checkHostBridgedeleted(g, toDelete)
			_ = inNamespace(testNS, func() error {
				validateVNIIsNotConfigured(g, toDelete.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should be idempotent", func() {
		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSName,
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: 4789,
			},
			L2GatewayIP: ptr.String("192.168.1.0/24"),
			HostMaster: &HostMaster{
				Name: bridgeName,
			},
		}

		err := SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		err = SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, params)

			_ = inNamespace(testNS, func() error {
				validateL2VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
})

func validateL3HostLeg(g Gomega, params L3VNIParams) {
	hostSide, _ := vethNamesFromVNI(params.VNI)
	hostLegLink, err := netlink.LinkByName(hostSide)
	g.Expect(err).NotTo(HaveOccurred(), "host side not found", hostSide)

	g.Expect(hostLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))

	// Check IPv4 address if provided
	if params.HostVeth.HostIPv4 != "" {
		hasIP, err := interfaceHasIP(hostLegLink, params.HostVeth.HostIPv4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "host leg does not have IPv4", params.HostVeth.HostIPv4)
	}

	// Check IPv6 address if provided
	if params.HostVeth.HostIPv6 != "" {
		hasIP, err := interfaceHasIP(hostLegLink, params.HostVeth.HostIPv6)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "host leg does not have IPv6", params.HostVeth.HostIPv6)
	}
}

func validateL2HostLeg(g Gomega, params L2VNIParams) {
	hostSide, _ := vethNamesFromVNI(params.VNI)
	hostLegLink, err := netlink.LinkByName(hostSide)
	g.Expect(err).NotTo(HaveOccurred(), "host side not found", hostSide)

	g.Expect(hostLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
	hasNoIP, err := interfaceHasNoIP(hostLegLink, netlink.FAMILY_V4)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasNoIP).To(BeTrue(), "host leg does have ip")
	if params.HostMaster != nil {
		hostMasterName := params.HostMaster.Name
		if params.HostMaster.AutoCreate {
			hostMasterName = hostBridgeName(params.VNI)
		}
		hostmaster, err := netlink.LinkByName(hostMasterName)
		g.Expect(err).NotTo(HaveOccurred(), "host master not found", *params.HostMaster)
		g.Expect(hostLegLink.Attrs().MasterIndex).To(Equal(hostmaster.Attrs().Index),
			"host leg is not attached to the bridge", params.HostMaster)
	} else {
		g.Expect(hostLegLink.Attrs().MasterIndex).To(BeZero(), "host leg is attached to a bridge but should not be")
	}
}

func validateL3VNI(g Gomega, params L3VNIParams) {
	validateVNI(g, params.VNIParams)

	if params.HostVeth == nil {
		return
	}
	validateVethForVNI(g, params.VNIParams)

	_, peSide := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(peSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", peSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))

	vrfLink, err := netlink.LinkByName(params.VRF)
	g.Expect(err).NotTo(HaveOccurred(), "vrf not found", params.VRF)
	g.Expect(peLegLink.Attrs().MasterIndex).To(Equal(vrfLink.Attrs().Index))

	// Check IPv4 address if provided
	if params.HostVeth.NSIPv4 != "" {
		hasIP, err := interfaceHasIP(peLegLink, params.HostVeth.NSIPv4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "PE leg does not have IPv4", params.HostVeth.NSIPv4)
	}

	// Check IPv6 address if provided
	if params.HostVeth.NSIPv6 != "" {
		hasIP, err := interfaceHasIP(peLegLink, params.HostVeth.NSIPv6)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "PE leg does not have IPv6", params.HostVeth.NSIPv6)
	}
}

func validateL2VNI(g Gomega, params L2VNIParams) {
	validateVNI(g, params.VNIParams)
	validateVethForVNI(g, params.VNIParams)

	_, peSide := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(peSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", peSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))

	hasNoIP, err := interfaceHasNoIP(peLegLink, netlink.FAMILY_V4)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasNoIP).To(BeTrue(), "host leg does have ip")

	bridgeLink, err := netlink.LinkByName(bridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "bridge not found", bridgeName(params.VNI))
	g.Expect(peLegLink.Attrs().MasterIndex).To(Equal(bridgeLink.Attrs().Index))
	if params.L2GatewayIP != nil {
		hasIP, err := interfaceHasIP(bridgeLink, *params.L2GatewayIP)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "bridge does not have ip", params.L2GatewayIP)

		validateBridgeMacAddress(g, bridgeLink, params.VNI)
		return
	} else {
		hasNoIP, err := interfaceHasNoIP(bridgeLink, netlink.FAMILY_V4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasNoIP).To(BeTrue(), "bridge does have ip")
	}
}

func validateVNI(g Gomega, params VNIParams) {
	loopback, err := netlink.LinkByName(UnderlayLoopback)
	g.Expect(err).NotTo(HaveOccurred(), "loopback not found", UnderlayLoopback)

	vxlanLink, err := netlink.LinkByName(vxLanNameFromVNI(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "vxlan link not found", vxLanNameFromVNI(params.VNI))

	vxlan := vxlanLink.(*netlink.Vxlan)
	g.Expect(vxlan.OperState).To(BeEquivalentTo(netlink.OperUnknown))

	addrGenModeNone := checkAddrGenModeNone(vxlan)
	g.Expect(addrGenModeNone).To(BeTrue())

	vrfLink, err := netlink.LinkByName(params.VRF)
	g.Expect(err).NotTo(HaveOccurred(), "vrf not found", params.VRF)

	vrf := vrfLink.(*netlink.Vrf)
	g.Expect(vrf.OperState).To(BeEquivalentTo(netlink.OperUp))

	bridgeLink, err := netlink.LinkByName(bridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "bridge not found", bridgeName(params.VNI))

	bridge := bridgeLink.(*netlink.Bridge)
	g.Expect(bridge.OperState).To(BeEquivalentTo(netlink.OperUp))

	g.Expect(bridge.MasterIndex).To(Equal(vrf.Index))

	addrGenModeNone = checkAddrGenModeNone(bridge)
	g.Expect(addrGenModeNone).To(BeTrue())

	err = checkVXLanConfigured(vxlan, bridge.Index, loopback.Attrs().Index, params)
	g.Expect(err).NotTo(HaveOccurred())
}

func validateVethForVNI(g Gomega, params VNIParams) {
	_, peSide := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(peSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", peSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
}

func checkHostBridgedeleted(g Gomega, params L2VNIParams) {
	g.Expect(params.HostMaster).ToNot(BeNil())
	g.Expect(params.HostMaster.AutoCreate).To(BeTrue())

	hostBridge := hostBridgeName(params.VNI)
	_, err := netlink.LinkByName(hostBridge)
	g.Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "host bridge not deleted", hostBridge, err)
}

func checkLinkdeleted(g Gomega, name string) {
	_, err := netlink.LinkByName(name)
	g.Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "link not deleted", name, err)
}

func checkLinkExists(g Gomega, name string) {
	_, err := netlink.LinkByName(name)
	g.Expect(err).NotTo(HaveOccurred(), "link not found", name)
}

func validateVNIIsNotConfigured(g Gomega, params VNIParams) {
	checkLinkdeleted(g, vxLanNameFromVNI(params.VNI))
	checkLinkdeleted(g, params.VRF)
	checkLinkdeleted(g, bridgeName(params.VNI))

	_, peSide := vethNamesFromVNI(params.VNI)
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

func createLinuxBridge(name string) {
	_, err := netlink.LinkByName(name)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}
		err = netlink.LinkAdd(bridge)
		Expect(err).NotTo(HaveOccurred(), "failed to create bridge", name)
		return
	}
	Expect(err).NotTo(HaveOccurred(), "failed to get bridge", name)
}

func validateBridgeMacAddress(g Gomega, bridge netlink.Link, vni int) {
	expectedMacs := map[int]net.HardwareAddr{
		100: {0x00, 0xF3, 0x00, 0x00, 0x00, 0x65}, // VNI+1 = 101 as big-endian int32
		101: {0x00, 0xF3, 0x00, 0x00, 0x00, 0x66}, // VNI+1 = 102 as big-endian int32
		300: {0x00, 0xF3, 0x00, 0x00, 0x01, 0x2D}, // VNI+1 = 301 as big-endian int32
		400: {0x00, 0xF3, 0x00, 0x00, 0x01, 0x91}, // VNI+1 = 401 as big-endian int32
	}

	expectedMac, exists := expectedMacs[vni]
	g.Expect(exists).To(BeTrue(), "no expected MAC address defined for VNI %d", vni)

	actualMac := bridge.Attrs().HardwareAddr
	g.Expect(actualMac).NotTo(BeNil(), "bridge should have a MAC address")
	g.Expect(actualMac).To(Equal(expectedMac), "bridge MAC address should be %v for VNI %d", expectedMac, vni)
}
