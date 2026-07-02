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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"k8s.io/utils/ptr"
)

const testNSName = "vnitestns"

func testNSPath() string {
	return fmt.Sprintf("/var/run/netns/%s", testNSName)
}

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
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			CrossNsIPs: &CrossNsIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3HostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with IPv6 only L3VNI", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			CrossNsIPs: &CrossNsIPs{
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3HostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with dual-stack L3VNI", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			CrossNsIPs: &CrossNsIPs{
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

			_ = netnamespace.In(testNS, func() error {
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
					TargetNS:  testNSPath(),
					VTEPIP:    "192.170.0.9/32",
					VNI:       100,
					VXLanPort: new(int32(4789)),
				},
				CrossNsIPs: &CrossNsIPs{
					HostIPv4: "192.168.9.1/32",
					NSIPv4:   "192.168.9.0/32",
				},
			},
			{
				VNIParams: VNIParams{
					VRF:       "testblue",
					TargetNS:  testNSPath(),
					VTEPIP:    "192.170.0.10/32",
					VNI:       101,
					VXLanPort: new(int32(4789)),
				},
				CrossNsIPs: &CrossNsIPs{
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
				_ = netnamespace.In(testNS, func() error {
					validateL3VNI(g, p)
					return nil
				})
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		}

		remaining := params[0]
		toDelete := params[1]

		By("removing non configured L3VNIs")
		err := RemoveNonConfiguredVNIs(testNSPath(), []VNIParams{remaining.VNIParams})
		Expect(err).NotTo(HaveOccurred())

		By("checking remaining L3VNIs")
		Eventually(func(g Gomega) {
			validateL3HostLeg(g, remaining)
			_ = netnamespace.In(testNS, func() error {
				validateL3VNI(g, remaining)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("checking non needed L3VNIs are removed")
		vethNames := vethNamesFromVNI(toDelete.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, vethNames.HostSide)
			_ = netnamespace.In(testNS, func() error {
				validateVNIIsNotConfigured(g, toDelete.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should be idempotent", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			CrossNsIPs: &CrossNsIPs{
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

			_ = netnamespace.In(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should configure VXLAN and VRF when CrossNsIPs is nil", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			CrossNsIPs: nil,
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			_ = netnamespace.In(testNS, func() error {
				validateVNI(g, params.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		// Verify that no host veth was created
		vethNames := vethNamesFromVNI(params.VNI)
		_, err = netlink.LinkByName(vethNames.HostSide)
		Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "host veth should not exist when CrossNsIPs is nil")
	})

	It("should set veth MTU to underlay MTU minus VXLan overhead when an underlay interface is configured", func() {
		const underlayMTU = 9000
		setupFakeUnderlay(testNS, "testunderlayl3", underlayMTU)

		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			CrossNsIPs: &CrossNsIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		expectedMTU := underlayMTU - VXLanOverhead
		Eventually(func(g Gomega) {
			validateVethMTU(g, params.VNIParams, expectedMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, params.VNIParams, expectedMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should leave veth MTU at default when no underlay interface is configured", func() {
		// No fake underlay is set up here, so findUnderlayMTU returns 0
		// and setVethMTUForVXLAN must leave the veth MTU untouched. The
		// host-side veth is not enslaved to any bridge in the L3 path
		// (it is only attached to a VRF in the target ns), so the host
		// leg's MTU reflects only what the code under test set.
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			CrossNsIPs: &CrossNsIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateVethMTU(g, params.VNIParams, defaultVethMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, params.VNIParams, defaultVethMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
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
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.168.1.0/24"},
			HostMaster: &HostMaster{
				Name: new(bridgeName),
				Type: BridgeLinkType,
			},
		}

		err := SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL2VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("removing the VNI")
		err = RemoveNonConfiguredVNIs(testNSPath(), []VNIParams{})
		Expect(err).NotTo(HaveOccurred())

		By("checking the VNI is removed")
		vethNames := vethNamesFromVNI(params.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, vethNames.HostSide)
			checkLinkExists(g, bridgeName)

			_ = netnamespace.In(testNS, func() error {
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
					TargetNS:  testNSPath(),
					VTEPIP:    "192.170.0.9/32",
					VNI:       100,
					VXLanPort: new(int32(4789)),
				},
				L2GatewayIPs: []string{"192.168.1.0/24"},
				HostMaster: &HostMaster{
					Name: new(bridgeName),
					Type: BridgeLinkType,
				},
			},
			{
				VNIParams: VNIParams{
					VRF:       "testblue",
					TargetNS:  testNSPath(),
					VTEPIP:    "192.170.0.10/32",
					VNI:       101,
					VXLanPort: new(int32(4789)),
				},
				L2GatewayIPs: []string{"192.168.1.0/24"},
				HostMaster: &HostMaster{
					AutoCreate: new(true),
					Type:       BridgeLinkType,
				},
			},
		}
		for _, p := range params {
			err := SetupL2VNI(context.Background(), p)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				validateL2HostLeg(g, p)
				_ = netnamespace.In(testNS, func() error {
					validateL2VNI(g, p)
					return nil
				})
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		}

		remaining := params[0]
		toDelete := params[1]

		By("removing non configured L2VNIs")
		err := RemoveNonConfiguredVNIs(testNSPath(), []VNIParams{remaining.VNIParams})
		Expect(err).NotTo(HaveOccurred())

		By("checking remaining L2VNIs")

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, remaining)
			_ = netnamespace.In(testNS, func() error {
				validateL2VNI(g, remaining)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("checking non needed L2VNIs are removed")
		vethNames := vethNamesFromVNI(toDelete.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, vethNames.HostSide)
			checkHostBridgedeleted(g, toDelete)
			_ = netnamespace.In(testNS, func() error {
				validateVNIIsNotConfigured(g, toDelete.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	DescribeTable("should be idempotent",
		func(params L2VNIParams) {
			err := SetupL2VNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())

			// Test idempotency - calling setup twice should work
			err = SetupL2VNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				validateL2HostLeg(g, params)

				_ = netnamespace.In(testNS, func() error {
					validateL2VNI(g, params)
					return nil
				})
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		},
		Entry("IPv4 single-stack", L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.168.1.0/24"},
			HostMaster: &HostMaster{
				Name: new(bridgeName),
				Type: BridgeLinkType,
			},
		}),
		Entry("dual-stack (IPv4 and IPv6)", L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testgreen",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.11/32",
				VNI:       300,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.168.2.0/24", "2001:db8::1/64"},
			HostMaster: &HostMaster{
				Name: new(bridgeName),
				Type: BridgeLinkType,
			},
		}),
		Entry("IPv6 single-stack", L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testblue",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.12/32",
				VNI:       400,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"2001:db8::1/64"},
			HostMaster: &HostMaster{
				Name: new(bridgeName),
				Type: BridgeLinkType,
			},
		}),
		Entry("disconnected L2VNI (no VRF)", L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.13/32",
				VNI:       500,
				VXLanPort: new(int32(4789)),
			},
			HostMaster: &HostMaster{
				Name: new(bridgeName),
				Type: BridgeLinkType,
			},
		}),
	)

	It("should set veth MTU to underlay MTU minus VXLan overhead when an underlay interface is configured", func() {
		const underlayMTU = 9000
		setupFakeUnderlay(testNS, "testunderlayl2", underlayMTU)

		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.168.1.0/24"},
			HostMaster: &HostMaster{
				Name: new(bridgeName),
				Type: BridgeLinkType,
			},
		}

		err := SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		expectedMTU := underlayMTU - VXLanOverhead
		Eventually(func(g Gomega) {
			validateVethMTU(g, params.VNIParams, expectedMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, params.VNIParams, expectedMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should leave veth MTU at default when no underlay interface is configured", func() {
		// No fake underlay is set up here, so findUnderlayMTU returns 0
		// and setVethMTUForVXLAN must leave the veth MTU untouched.
		// HostMaster is intentionally omitted so the host veth is not
		// enslaved to a bridge — Linux bridges auto-clamp their MTU to
		// the smallest member, which would couple this assertion to
		// bridge default MTU rather than to the code under test.
		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.168.1.0/24"},
		}

		err := SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateVethMTU(g, params.VNIParams, defaultVethMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, params.VNIParams, defaultVethMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
})

func validateL3HostLeg(g Gomega, params L3VNIParams) {
	vethNames := vethNamesFromVNI(params.VNI)
	hostLegLink, err := netlink.LinkByName(vethNames.HostSide)
	g.Expect(err).NotTo(HaveOccurred(), "host side not found", vethNames.HostSide)

	g.Expect(hostLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))

	// Check IPv4 address if provided
	if params.CrossNsIPs.HostIPv4 != "" {
		hasIP, err := interfaceHasIP(hostLegLink, params.CrossNsIPs.HostIPv4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "host leg does not have IPv4", params.CrossNsIPs.HostIPv4)
	}

	// Check IPv6 address if provided
	if params.CrossNsIPs.HostIPv6 != "" {
		hasIP, err := interfaceHasIP(hostLegLink, params.CrossNsIPs.HostIPv6)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "host leg does not have IPv6", params.CrossNsIPs.HostIPv6)
	}
}

func validateL2HostLeg(g Gomega, params L2VNIParams) {
	vethNames := vethNamesFromVNI(params.VNI)
	hostLegLink, err := netlink.LinkByName(vethNames.HostSide)
	g.Expect(err).NotTo(HaveOccurred(), "host side not found", vethNames.HostSide)

	g.Expect(hostLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
	hasNoIP, err := interfaceHasNoIP(hostLegLink, netlink.FAMILY_V4)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasNoIP).To(BeTrue(), "host leg does have ip")
	if params.HostMaster == nil {
		g.Expect(hostLegLink.Attrs().MasterIndex).To(BeZero(), "host leg is attached to a bridge but should not be")
		return
	}

	hostMasterName := ptr.Deref(params.HostMaster.Name, "")
	if ptr.Deref(params.HostMaster.AutoCreate, false) {
		hostMasterName = hostBridgeName(params.VNI)
	}

	switch params.HostMaster.Type {
	case OVSBridgeLinkType:
		checkOVSBridgeExists(g, hostMasterName)
		checkVethAttachedToOVSBridge(g, hostMasterName, vethNames.HostSide)
	case BridgeLinkType:
		hostmaster, err := netlink.LinkByName(hostMasterName)
		g.Expect(err).NotTo(HaveOccurred(), "host master not found", *params.HostMaster)
		g.Expect(hostLegLink.Attrs().MasterIndex).To(Equal(hostmaster.Attrs().Index),
			"host leg is not attached to the bridge", params.HostMaster)
	default:
		g.Expect(params.HostMaster.Type).To(BeEmpty(), "unknown bridge type: %s", params.HostMaster.Type)
	}
}

func validateL3VNI(g Gomega, params L3VNIParams) {
	validateVNI(g, params.VNIParams)

	if params.CrossNsIPs == nil {
		return
	}
	validateVethForVNI(g, params.VNIParams)

	bridgeLink, err := netlink.LinkByName(BridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "bridge not found for addr_gen_mode check", BridgeName(params.VNI))
	g.Expect(checkAddrGenModeNone(bridgeLink)).To(BeTrue(), "L3VNI bridge must have addr_gen_mode=1")

	vethNames := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", vethNames.NamespaceSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))

	vrfLink, err := netlink.LinkByName(params.VRF)
	g.Expect(err).NotTo(HaveOccurred(), "vrf not found", params.VRF)
	g.Expect(peLegLink.Attrs().MasterIndex).To(Equal(vrfLink.Attrs().Index))

	// Check IPv4 address if provided
	if params.CrossNsIPs.NSIPv4 != "" {
		hasIP, err := interfaceHasIP(peLegLink, params.CrossNsIPs.NSIPv4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "PE leg does not have IPv4", params.CrossNsIPs.NSIPv4)
	}

	// Check IPv6 address if provided
	if params.CrossNsIPs.NSIPv6 != "" {
		hasIP, err := interfaceHasIP(peLegLink, params.CrossNsIPs.NSIPv6)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "PE leg does not have IPv6", params.CrossNsIPs.NSIPv6)
	}
}

func validateL2VNI(g Gomega, params L2VNIParams) {
	validateVNI(g, params.VNIParams)
	validateVethForVNI(g, params.VNIParams)

	bridgeLinkForMode, err := netlink.LinkByName(BridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "bridge not found for addr_gen_mode check", BridgeName(params.VNI))
	g.Expect(checkAddrGenModeNone(bridgeLinkForMode)).To(BeFalse(), "L2VNI bridge must NOT have addr_gen_mode=1")

	vethNames := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", vethNames.NamespaceSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))

	hasNoIP, err := interfaceHasNoIP(peLegLink, netlink.FAMILY_V4)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasNoIP).To(BeTrue(), "host leg does have ip")

	bridgeLink, err := netlink.LinkByName(BridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "bridge not found", BridgeName(params.VNI))
	g.Expect(peLegLink.Attrs().MasterIndex).To(Equal(bridgeLink.Attrs().Index))
	if len(params.L2GatewayIPs) > 0 {
		for _, ip := range params.L2GatewayIPs {
			hasIP, err := interfaceHasIP(bridgeLink, ip)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(hasIP).To(BeTrue(), "bridge does not have ip", ip)
		}

		validateBridgeMacAddress(g, bridgeLink, params.VNI)
		return
	} else {
		hasNoIP, err := interfaceHasNoIP(bridgeLink, netlink.FAMILY_V4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasNoIP).To(BeTrue(), "bridge does have ip")
	}
}

func validateVNI(g Gomega, params VNIParams) {
	vtepDev, err := netlink.LinkByName(loopbackName)
	g.Expect(err).NotTo(HaveOccurred(), "vtep device not found %q", loopbackName)

	vxlanLink, err := netlink.LinkByName(vxLanNameFromVNI(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "vxlan link not found %q", vxLanNameFromVNI(params.VNI))

	vxlan := vxlanLink.(*netlink.Vxlan)
	g.Expect(vxlan.OperState).To(BeEquivalentTo(netlink.OperUnknown))

	addrGenModeNone := checkAddrGenModeNone(vxlan)
	g.Expect(addrGenModeNone).To(BeTrue())

	bridgeLink, err := netlink.LinkByName(BridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "bridge not found", BridgeName(params.VNI))

	bridge := bridgeLink.(*netlink.Bridge)
	g.Expect(bridge.OperState).To(BeEquivalentTo(netlink.OperUp))

	if params.VRF == "" {
		g.Expect(bridge.MasterIndex).To(BeZero(), "disconnected bridge should not be enslaved to a VRF")

		err = checkVXLanConfigured(vxlan, bridge.Index, vtepDev.Attrs().Index, params)
		g.Expect(err).NotTo(HaveOccurred())
		return
	}

	vrfLink, err := netlink.LinkByName(params.VRF)
	g.Expect(err).NotTo(HaveOccurred(), "vrf not found", params.VRF)

	vrf, isVrf := vrfLink.(*netlink.Vrf)
	g.Expect(isVrf).To(BeTrue(), "link %s is not a VRF", params.VRF)
	g.Expect(vrf.OperState).To(BeEquivalentTo(netlink.OperUp))

	g.Expect(bridge.MasterIndex).To(Equal(vrf.Index))

	err = checkVXLanConfigured(vxlan, bridge.Index, vtepDev.Attrs().Index, params)
	g.Expect(err).NotTo(HaveOccurred())

	ok, err := hasUnreachableDefaultRoute(int(vrf.Table), netlink.FAMILY_V4)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ok).To(BeTrue())

	ok, err = hasUnreachableDefaultRoute(int(vrf.Table), netlink.FAMILY_V6)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ok).To(BeTrue())

}

func validateVethForVNI(g Gomega, params VNIParams) {
	vethNames := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", vethNames.NamespaceSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
}

func checkHostBridgedeleted(g Gomega, params L2VNIParams) {
	g.Expect(params.HostMaster).ToNot(BeNil())
	g.Expect(ptr.Deref(params.HostMaster.AutoCreate, false)).To(BeTrue())

	hostBridge := hostBridgeName(params.VNI)
	_, err := netlink.LinkByName(hostBridge)
	g.Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "host bridge not deleted", hostBridge, err)
}

func checkLinkdeleted(g Gomega, name string) {
	_, err := netlink.LinkByName(name)
	g.Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "link not deleted", name, err)
}

func checkInterfaceHasNoNonLoopbackIPs(g Gomega, intf string) {
	lo, err := netlink.LinkByName(intf)
	g.Expect(err).NotTo(HaveOccurred())

	addresses, err := netlink.AddrList(lo, netlink.FAMILY_ALL)
	g.Expect(err).NotTo(HaveOccurred())

	numAddresses := 0
	for _, address := range addresses {
		if address.IP.IsLoopback() {
			continue
		}
		numAddresses++
	}
	g.Expect(numAddresses).To(Equal(0))
}

func checkLinkExists(g Gomega, name string) {
	_, err := netlink.LinkByName(name)
	g.Expect(err).NotTo(HaveOccurred(), "link not found %q", name)
}

func validateVNIIsNotConfigured(g Gomega, params VNIParams) {
	checkLinkdeleted(g, vxLanNameFromVNI(params.VNI))
	if params.VRF != "" {
		checkLinkdeleted(g, params.VRF)
	}
	checkLinkdeleted(g, BridgeName(params.VNI))

	vethNames := vethNamesFromVNI(params.VNI)
	checkLinkdeleted(g, vethNames.NamespaceSide)
}

func checkAddrGenModeNone(l netlink.Link) bool {
	fileName := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/addr_gen_mode", l.Attrs().Name)
	addrGenMode, err := os.ReadFile(fileName)
	Expect(err).NotTo(HaveOccurred())

	return strings.Trim(string(addrGenMode), "\n") == "1"
}

func setupLoopback(ns netns.NsHandle) {
	handle, err := netlink.NewHandleAt(ns)
	Expect(err).NotTo(HaveOccurred())
	defer handle.Close()

	lo, err := handle.LinkByName(loopbackName)
	Expect(err).NotTo(HaveOccurred())
	Expect(handle.LinkSetUp(lo)).To(Succeed())
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

func validateBridgeMacAddress(g Gomega, bridge netlink.Link, vni int32) {
	expectedMacs := map[int32]net.HardwareAddr{
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

func validateVethMTU(g Gomega, params VNIParams, expectedMTU int) {
	vethNames := vethNamesFromVNI(params.VNI)
	hostLeg, err := netlink.LinkByName(vethNames.HostSide)
	g.Expect(err).NotTo(HaveOccurred(), "host veth not found %q", vethNames.HostSide)
	g.Expect(hostLeg.Attrs().MTU).To(Equal(expectedMTU),
		"host veth MTU should be %d, got %d", expectedMTU, hostLeg.Attrs().MTU)
}

func validateNSVethMTU(g Gomega, params VNIParams, expectedMTU int) {
	vethNames := vethNamesFromVNI(params.VNI)
	peLeg, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "pe veth not found %q", vethNames.NamespaceSide)
	g.Expect(peLeg.Attrs().MTU).To(Equal(expectedMTU),
		"pe veth MTU should be %d, got %d", expectedMTU, peLeg.Attrs().MTU)
}

// defaultVethMTU is the MTU veth pairs receive when no explicit MTU is set.
const defaultVethMTU = 1500

// setupFakeUnderlay creates a dummy interface inside the given namespace with
// the underlay special address and a configurable MTU, so that findUnderlayMTU
// can locate it. It is intended for unit tests exercising the MTU propagation
// behavior of SetupL2VNI / SetupL3VNI.
func setupFakeUnderlay(ns netns.NsHandle, name string, mtu int) {
	err := netnamespace.In(ns, func() error {
		dummy := &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name: name,
				MTU:  mtu,
			},
		}
		if err := netlink.LinkAdd(dummy); err != nil {
			return fmt.Errorf("failed to add fake underlay dummy %s: %w", name, err)
		}
		link, err := netlink.LinkByName(name)
		if err != nil {
			return fmt.Errorf("failed to get fake underlay dummy %s: %w", name, err)
		}
		if err := netlink.LinkSetGroup(link, int(UnderlayGroupID)); err != nil {
			return fmt.Errorf("failed to set underlay group ID on %s: %w", name, err)
		}
		return nil
	})
	Expect(err).NotTo(HaveOccurred(), "failed to set up fake underlay")
}
