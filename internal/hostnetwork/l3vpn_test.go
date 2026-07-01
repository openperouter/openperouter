// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var _ = Describe("L3 VPN configuration", func() {
	var testNS netns.NsHandle

	BeforeEach(func() {
		cleanTest(testNSName)
		testNS = createTestNS(testNSName)
		setupLoopback(testNS)
	})
	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should work with IPv4 only L3VPN", func() {
		params := L3VPNParams{
			VRF:              "testred",
			TargetNS:         testNSPath(),
			RDAssignedNumber: 100,
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3VPNHostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL3VPNNamespaceLeg(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with IPv6 only L3VPN", func() {
		params := L3VPNParams{
			VRF:              "testred",
			TargetNS:         testNSPath(),
			RDAssignedNumber: 100,
			HostVeth: &Veth{
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		err := SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3VPNHostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL3VPNNamespaceLeg(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with dual-stack L3VPN", func() {
		params := L3VPNParams{
			VRF:              "testred",
			TargetNS:         testNSPath(),
			RDAssignedNumber: 100,
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		err := SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3VPNHostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL3VPNNamespaceLeg(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with multiple L3VPNs + cleanup", func() {
		params := []L3VPNParams{
			{
				VRF:              "testred",
				TargetNS:         testNSPath(),
				RDAssignedNumber: 100,
				HostVeth: &Veth{
					HostIPv4: "192.168.9.1/32",
					NSIPv4:   "192.168.9.0/32",
				},
			},
			{
				VRF:              "testblue",
				TargetNS:         testNSPath(),
				RDAssignedNumber: 101,
				HostVeth: &Veth{
					HostIPv4: "192.168.9.2/32",
					NSIPv4:   "192.168.9.3/32",
				},
			},
		}
		for _, p := range params {
			err := SetupL3VPN(context.Background(), p)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				validateL3VPNHostLeg(g, p)
				_ = netnamespace.In(testNS, func() error {
					validateL3VPNNamespaceLeg(g, p)
					return nil
				})
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		}

		remaining := params[0]
		toDelete := params[1]

		By("removing non configured L3VPNs")
		err := RemoveNonConfiguredL3VPNs(testNSPath(), []L3VPNParams{remaining})
		Expect(err).NotTo(HaveOccurred())
		err = RemoveNonConfiguredVRFs(testNSPath(), map[string]bool{remaining.VRF: true})
		Expect(err).NotTo(HaveOccurred())

		By("checking remaining L3VPNs")
		Eventually(func(g Gomega) {
			validateL3VPNHostLeg(g, remaining)
			_ = netnamespace.In(testNS, func() error {
				validateL3VPNNamespaceLeg(g, remaining)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("checking non needed L3VPNs are removed")
		vethNames := vethNamesFromL3VPN(toDelete.RDAssignedNumber)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, vethNames.HostSide)
			_ = netnamespace.In(testNS, func() error {
				vethNames := vethNamesFromL3VPN(toDelete.RDAssignedNumber)
				checkLinkdeleted(g, vethNames.NamespaceSide)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with L3VPNs + L2VNI + cleanup", func() {
		const bridgeName = "testbridge"
		createLinuxBridge(bridgeName)

		l3vpnParams := []L3VPNParams{
			{
				VRF:              "testred",
				TargetNS:         testNSPath(),
				RDAssignedNumber: 100,
				HostVeth: &Veth{
					HostIPv4: "192.168.9.1/32",
					NSIPv4:   "192.168.9.0/32",
				},
			},
			{
				VRF:              "testblue",
				TargetNS:         testNSPath(),
				RDAssignedNumber: 101,
				HostVeth: &Veth{
					HostIPv4: "192.168.9.2/32",
					NSIPv4:   "192.168.9.3/32",
				},
			},
		}
		deletedL3vpnParams := []L3VPNParams{}
		l2vniParams := []L2VNIParams{
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
		}
		deletedL2vniParams := []L2VNIParams{}

		allVRFs := func() map[string]bool {
			vrfs := map[string]bool{}
			for _, l3vpn := range l3vpnParams {
				vrfs[l3vpn.VRF] = true
			}
			for _, l2vni := range l2vniParams {
				vrfs[l2vni.VRF] = true
			}
			return vrfs
		}

		allDeletedVRFs := func() []string {
			deletedMap := map[string]bool{}
			for _, l3vpn := range deletedL3vpnParams {
				deletedMap[l3vpn.VRF] = true
			}
			for _, l2vni := range deletedL2vniParams {
				if l2vni.VRF == "" {
					continue
				}
				deletedMap[l2vni.VRF] = true
			}
			for _, l3vpn := range l3vpnParams {
				deletedMap[l3vpn.VRF] = false
			}
			for _, l2vni := range l2vniParams {
				if l2vni.VRF == "" {
					continue
				}
				deletedMap[l2vni.VRF] = false
			}

			deletedVRFs := []string{}
			for vrf, isDeleted := range deletedMap {
				if isDeleted {
					deletedVRFs = append(deletedVRFs, vrf)
				}
			}
			return deletedVRFs
		}

		allVNIParams := func() []VNIParams {
			vniParams := make([]VNIParams, len(l2vniParams))
			for i, p := range l2vniParams {
				vniParams[i] = p.VNIParams
			}
			return vniParams
		}

		validateAll := func() {
			By("checking remaining L3VPNs")
			for _, p := range l3vpnParams {
				Eventually(func(g Gomega) {
					validateL3VPNHostLeg(g, p)
					_ = netnamespace.In(testNS, func() error {
						validateL3VPNNamespaceLeg(g, p)
						return nil
					})
				}, 30*time.Second, 1*time.Second).Should(Succeed())
			}

			By("checking remaining L2VNIs")
			for _, p := range l2vniParams {
				Eventually(func(g Gomega) {
					validateL2HostLeg(g, p)
					_ = netnamespace.In(testNS, func() error {
						validateL2VNI(g, p)
						return nil
					})
				}, 30*time.Second, 1*time.Second).Should(Succeed())
			}

			By("checking non needed L3VPNs are removed")
			for _, deleted := range deletedL3vpnParams {
				vethNames := vethNamesFromL3VPN(deleted.RDAssignedNumber)
				Eventually(func(g Gomega) {
					checkLinkdeleted(g, vethNames.HostSide)
					_ = netnamespace.In(testNS, func() error {
						vethNames := vethNamesFromL3VPN(deleted.RDAssignedNumber)
						checkLinkdeleted(g, vethNames.NamespaceSide)
						return nil
					})
				}, 30*time.Second, 1*time.Second).Should(Succeed())
			}

			By("checking non needed L2VNIs are removed")
			for _, deleted := range deletedL2vniParams {
				vethNames := vethNamesFromVNI(deleted.VNI)
				Eventually(func(g Gomega) {
					checkLinkdeleted(g, vethNames.HostSide)
					checkLinkExists(g, bridgeName)

					_ = netnamespace.In(testNS, func() error {
						validateVNIIsNotConfigured(g, deleted.VNIParams)
						return nil
					})
				}, 30*time.Second, 1*time.Second).Should(Succeed())
			}

			deletedVRFs := allDeletedVRFs()
			By(fmt.Sprintf("checking non needed VRFs (%v) are removed", deletedVRFs))
			for _, vrf := range deletedVRFs {
				Eventually(func(g Gomega) {
					_ = netnamespace.In(testNS, func() error {
						checkLinkdeleted(g, vrf)
						return nil
					})
				}, 30*time.Second, 1*time.Second).Should(Succeed())
			}
		}

		By("setting up L3VPNs")
		for _, p := range l3vpnParams {
			Expect(SetupL3VPN(context.Background(), p)).To(Succeed())
		}
		By("setting up L2VNIs")
		for _, p := range l2vniParams {
			Expect(SetupL2VNI(context.Background(), p)).To(Succeed())
		}
		validateAll()

		By("removing second configured L3VPN")
		deletedL3vpnParams = l3vpnParams[1:2]
		l3vpnParams = l3vpnParams[0:1]
		Expect(RemoveNonConfiguredL3VPNs(testNSPath(), l3vpnParams)).To(Succeed())
		Expect(RemoveNonConfiguredVNIs(testNSPath(), allVNIParams())).To(Succeed())
		Expect(RemoveNonConfiguredVRFs(testNSPath(), allVRFs())).To(Succeed())
		validateAll()

		By("removing configured L2VNI")
		deletedL2vniParams = l2vniParams[0:1]
		l2vniParams = []L2VNIParams{}
		Expect(RemoveNonConfiguredL3VPNs(testNSPath(), l3vpnParams)).To(Succeed())
		Expect(RemoveNonConfiguredVNIs(testNSPath(), allVNIParams())).To(Succeed())
		Expect(RemoveNonConfiguredVRFs(testNSPath(), allVRFs())).To(Succeed())
		validateAll()

		By("removing first configured L3VPN")
		deletedL3vpnParams = append(deletedL3vpnParams, l3vpnParams[0])
		l3vpnParams = []L3VPNParams{}
		Expect(RemoveNonConfiguredL3VPNs(testNSPath(), l3vpnParams)).To(Succeed())
		Expect(RemoveNonConfiguredVNIs(testNSPath(), allVNIParams())).To(Succeed())
		Expect(RemoveNonConfiguredVRFs(testNSPath(), allVRFs())).To(Succeed())
		validateAll()
	})

	It("should be idempotent", func() {
		params := L3VPNParams{
			VRF:              "testred",
			TargetNS:         testNSPath(),
			RDAssignedNumber: 100,
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		err = SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3VPNHostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL3VPNNamespaceLeg(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should configure VRF when HostVeth is nil", func() {
		params := L3VPNParams{
			VRF:              "testred",
			TargetNS:         testNSPath(),
			RDAssignedNumber: 100,
			HostVeth:         nil,
		}

		err := SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			_ = netnamespace.In(testNS, func() error {
				_, _ = validateVRF(g, params.VRF)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		// Verify that no host veth was created
		vethNames := vethNamesFromL3VPN(params.RDAssignedNumber)
		_, err = netlink.LinkByName(vethNames.HostSide)
		Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "host veth should not exist when HostVeth is nil")
	})

	It("should set veth MTU to underlay MTU minus SRv6 overhead when an underlay interface is configured", func() {
		const underlayMTU = 9000
		setupFakeUnderlay(testNS, "testunderlayl3", underlayMTU)

		params := L3VPNParams{
			VRF:              "testred",
			TargetNS:         testNSPath(),
			RDAssignedNumber: 100,
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		expectedMTU := underlayMTU - SRv6Overhead
		Eventually(func(g Gomega) {
			vethNames := vethNamesFromL3VPN(params.RDAssignedNumber)
			validateVethMTU(g, vethNames, expectedMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, vethNames, expectedMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should leave veth MTU at default when no underlay interface is configured", func() {
		// No fake underlay is set up here, so findUnderlayMTU returns 0
		// and the veth MTU must be left untouched.
		params := L3VPNParams{
			VRF:              "testred",
			TargetNS:         testNSPath(),
			RDAssignedNumber: 100,
			HostVeth: &Veth{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		err := SetupL3VPN(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			vethNames := vethNamesFromL3VPN(params.RDAssignedNumber)
			validateVethMTU(g, vethNames, defaultVethMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, vethNames, defaultVethMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
})

func validateL3VPNHostLeg(g Gomega, params L3VPNParams) {
	vethNames := vethNamesFromL3VPN(params.RDAssignedNumber)
	hostLegLink, err := netlink.LinkByName(vethNames.HostSide)
	g.Expect(err).NotTo(HaveOccurred(), "host side not found", vethNames.HostSide)

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

func validateL3VPNNamespaceLeg(g Gomega, params L3VPNParams) {
	vrfLink, _ := validateVRF(g, params.VRF)

	if params.HostVeth == nil {
		return
	}

	vethNames := vethNamesFromL3VPN(params.RDAssignedNumber)
	peLegLink, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", vethNames.NamespaceSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
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
