// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
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
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
	"k8s.io/utils/ptr"
)

const testNSName = "vnitestns"

func testNSPath() string {
	return fmt.Sprintf("/var/run/netns/%s", testNSName)
}

var _ = Describe("L3 VNI configuration", func() {
	var (
		testNS    netns.NsHandle
		ovsClient *OVSClient
	)

	BeforeEach(func() {
		cleanTest(testNSName)
		testNS = createTestNS(testNSName)
		ovsClient = &OVSClient{}
		DeferCleanup(func() {
			ovsClient.Close()
		})
		setupTunnelLoopback(testNS)
	})
	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should share the router MAC across L3VNIs and restore it after drift or recreation", func() {
		routerMAC, err := RouterMACForNode("02:00:00:00:00:00/32", 42)
		Expect(err).To(Succeed())
		cfg := newConfiguratorForTest(ovsClient)

		for i, vni := range []int32{100, 200} {
			params := L3VNIParams{
				VNIParams: VNIParams{
					VRF:       fmt.Sprintf("testred%d", vni),
					TargetNS:  testNSPath(),
					VTEPIP:    fmt.Sprintf("192.170.0.%d/32", 9+i),
					VNI:       vni,
					VXLanPort: new(int32(4789)),
				},
				RouterMAC: routerMAC,
			}

			By(fmt.Sprintf("setting up L3VNI %d", vni))
			Expect(cfg.SetupL3VNI(context.Background(), params)).To(Succeed())
			Expect(l3VNISVIMAC(testNS, vni)).To(Equal(routerMAC))

			By(fmt.Sprintf("restoring the drifted router MAC of L3VNI %d", vni))
			Expect(inVLANEndpoint(testNS, vni, func(endpoint netlink.Link) error {
				return netlink.LinkSetHardwareAddr(endpoint, net.HardwareAddr{2, 0, 0, 0, 0, 2})
			})).To(Succeed())
			Expect(cfg.SetupL3VNI(context.Background(), params)).To(Succeed())
			Expect(l3VNISVIMAC(testNS, vni)).To(Equal(routerMAC))

			By(fmt.Sprintf("recreating the router MAC of L3VNI %d", vni))
			Expect(inVLANEndpoint(testNS, vni, netlink.LinkDel)).To(Succeed())
			Expect(cfg.SetupL3VNI(context.Background(), params)).To(Succeed())
			Expect(l3VNISVIMAC(testNS, vni)).To(Equal(routerMAC))
		}
	})

	It("should apply the endpoint options before bringing the SVI up", func() {
		Expect(netnamespace.In(testNS, func() error {
			bridge, err := ensureSwitchingBridge(switchingBridgeName(100))
			if err != nil {
				return err
			}
			mac := net.HardwareAddr{2, 0, 0, 0, 0, 1}
			endpoint, err := ensureVLANEndpoint(BridgeName(100), bridge, internalVLANID, func(link netlink.Link) error {
				actual, err := netlink.LinkByName(link.Attrs().Name)
				if err != nil {
					return err
				}
				Expect(actual.Attrs().Flags&net.FlagUp).To(BeZero(), "endpoint was up before its options ran")
				return ensureBridgeMACAddress(link, mac)
			})
			if err != nil {
				return err
			}
			Expect(endpoint.Attrs().HardwareAddr).To(Equal(mac))
			Expect(endpoint.Attrs().Flags & net.FlagUp).To(Equal(net.FlagUp))
			return nil
		})).To(Succeed())
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
			LinkIPs: &LinkIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL3VNI(context.Background(), params)
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
			LinkIPs: &LinkIPs{
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL3VNI(context.Background(), params)
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
			LinkIPs: &LinkIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL3VNI(context.Background(), params)
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
				LinkIPs: &LinkIPs{
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
				LinkIPs: &LinkIPs{
					HostIPv4: "192.168.9.2/32",
					NSIPv4:   "192.168.9.3/32",
				},
			},
		}
		cfg := newConfiguratorForTest(ovsClient)
		for _, p := range params {
			err := cfg.SetupL3VNI(context.Background(), p)
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
		err := RemoveNonConfiguredVNIs(ovsClient, testNSPath(), []VNIParams{remaining.VNIParams})
		Expect(err).NotTo(HaveOccurred())
		err = RemoveNonConfiguredVRFs(testNSPath(), map[string]bool{remaining.VRF: true})
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
				if toDelete.VRF != "" {
					checkLinkdeleted(g, toDelete.VRF)
				}
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
			LinkIPs: &LinkIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		err = cfg.SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL3HostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL3VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should configure VXLAN and VRF when LinkIPs is nil", func() {
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			LinkIPs: nil,
		}

		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL3VNI(context.Background(), params)
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
		Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "host veth should not exist when LinkIPs is nil")
	})

	It("should set veth MTU to underlay MTU minus VXLan overhead when an underlay interface is configured", func() {
		const underlayMTU = 9000
		setupFakeUnderlay(testNS, "testunderlayl3", underlayMTU)

		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:            "testred",
				TargetNS:       testNSPath(),
				VTEPIP:         "192.170.0.9/32",
				VNI:            100,
				VXLanPort:      new(int32(4789)),
				TunnelOverhead: VXLanOverhead,
			},
			LinkIPs: &LinkIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		expectedMTU := underlayMTU - VXLanOverhead
		Eventually(func(g Gomega) {
			vethNames := vethNamesFromVNI(params.VNI)
			validateVethMTU(g, vethNames, expectedMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, vethNames, expectedMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should use the underlay MTU resolved for the pass instead of looking it up again", func() {
		const underlayMTU = 9000
		setupFakeUnderlay(testNS, "testunderlayl3", underlayMTU)

		cfg := newConfiguratorForTest(ovsClient)
		Expect(cfg.underlayMTU).To(Equal(underlayMTU))

		// Remove the underlay after the pass state is resolved: a fresh lookup
		// would find no underlay and leave the veth MTU at its default.
		removeFakeUnderlay(testNS, "testunderlayl3")

		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:            "testred",
				TargetNS:       testNSPath(),
				VTEPIP:         "192.170.0.9/32",
				VNI:            100,
				VXLanPort:      new(int32(4789)),
				TunnelOverhead: VXLanOverhead,
			},
			LinkIPs: &LinkIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		Expect(cfg.SetupL3VNI(context.Background(), params)).To(Succeed())

		expectedMTU := underlayMTU - VXLanOverhead
		Eventually(func(g Gomega) {
			vethNames := vethNamesFromVNI(params.VNI)
			validateVethMTU(g, vethNames, expectedMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, vethNames, expectedMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should leave veth MTU at default when no underlay interface is configured", func() {
		// No underlay MTU is passed here, so SetVethMTUForTunnelOverhead must
		// leave the veth MTU untouched.
		// The host-side veth is not attached to any bridge in the L3 path.
		// The peer veth is attached to a VRF in the target namespace.
		// The host leg's MTU reflects only what the code under test set.
		params := L3VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			LinkIPs: &LinkIPs{
				HostIPv4: "192.168.9.1/32",
				NSIPv4:   "192.168.9.0/32",
			},
		}

		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL3VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			vethNames := vethNamesFromVNI(params.VNI)
			validateVethMTU(g, vethNames, defaultVethMTU)
			_ = netnamespace.In(testNS, func() error {
				validateNSVethMTU(g, vethNames, defaultVethMTU)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
})

var _ = Describe("L2 VNI configuration", func() {
	var (
		testNS    netns.NsHandle
		ovsClient *OVSClient
	)
	const bridgeName = "testbridge"

	BeforeEach(func() {
		cleanTest(testNSName)
		testNS = createTestNS(testNSName)
		ovsClient = &OVSClient{}
		setupTunnelLoopback(testNS)
		createLinuxBridge(bridgeName)
	})
	AfterEach(func() {
		cleanTest(testNSName)
		ovsClient.Close()
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

		createVRFInNamespace(testNS, params.VRF)
		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, params)

			_ = netnamespace.In(testNS, func() error {
				validateL2VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("removing the VNI")
		err = RemoveNonConfiguredVNIs(ovsClient, testNSPath(), []VNIParams{})
		Expect(err).NotTo(HaveOccurred())
		err = RemoveNonConfiguredVRFs(testNSPath(), map[string]bool{})
		Expect(err).NotTo(HaveOccurred())

		By("checking the VNI is removed")
		vethNames := vethNamesFromVNI(params.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, vethNames.HostSide)
			checkLinkExists(g, bridgeName)

			_ = netnamespace.In(testNS, func() error {
				validateVNIIsNotConfigured(g, params.VNIParams)
				if params.VRF != "" {
					checkLinkdeleted(g, params.VRF)
				}
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
		cfg := newConfiguratorForTest(ovsClient)
		for _, p := range params {
			createVRFInNamespace(testNS, p.VRF)
			err := cfg.SetupL2VNI(context.Background(), p)
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
		err := RemoveNonConfiguredVNIs(ovsClient, testNSPath(), []VNIParams{remaining.VNIParams})
		Expect(err).NotTo(HaveOccurred())
		err = RemoveNonConfiguredVRFs(testNSPath(), map[string]bool{remaining.VRF: true})
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
				if toDelete.VRF != "" {
					checkLinkdeleted(g, toDelete.VRF)
				}
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	DescribeTable("should be idempotent",
		func(params L2VNIParams) {
			if params.VRF != "" {
				createVRFInNamespace(testNS, params.VRF)
			}
			cfg := newConfiguratorForTest(ovsClient)
			err := cfg.SetupL2VNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())

			// Test idempotency - calling setup twice should work
			err = cfg.SetupL2VNI(context.Background(), params)
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
				VRF:            "testred",
				TargetNS:       testNSPath(),
				VTEPIP:         "192.170.0.9/32",
				VNI:            100,
				VXLanPort:      new(int32(4789)),
				TunnelOverhead: VXLanOverhead,
			},
			L2GatewayIPs: []string{"192.168.1.0/24"},
			HostMaster: &HostMaster{
				Name: new(bridgeName),
				Type: BridgeLinkType,
			},
		}

		createVRFInNamespace(testNS, params.VRF)
		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		expectedMTU := underlayMTU - VXLanOverhead
		Eventually(func(g Gomega) {
			validateBothVethMTUs(g, testNS, params.VNI, expectedMTU)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should use the underlay MTU resolved for the pass instead of looking it up again", func() {
		const underlayMTU = 9000
		setupFakeUnderlay(testNS, "testunderlayl2", underlayMTU)

		cfg := newConfiguratorForTest(ovsClient)
		Expect(cfg.underlayMTU).To(Equal(underlayMTU))

		// Remove the underlay after the pass state is resolved: a fresh lookup
		// would find no underlay and leave the veth MTU at its default.
		removeFakeUnderlay(testNS, "testunderlayl2")

		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF:            "testred",
				TargetNS:       testNSPath(),
				VTEPIP:         "192.170.0.9/32",
				VNI:            100,
				VXLanPort:      new(int32(4789)),
				TunnelOverhead: VXLanOverhead,
			},
			L2GatewayIPs: []string{"192.168.1.0/24"},
		}

		createVRFInNamespace(testNS, params.VRF)
		Expect(cfg.SetupL2VNI(context.Background(), params)).To(Succeed())

		expectedMTU := underlayMTU - VXLanOverhead
		Eventually(func(g Gomega) {
			validateBothVethMTUs(g, testNS, params.VNI, expectedMTU)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should bind the vxlan to the loopback resolved for the pass", func() {
		const vtepDeviceName = "test-vtep"
		var vtepDevice netlink.Link
		Expect(netnamespace.In(testNS, func() error {
			dummy := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: vtepDeviceName}}
			if err := netlink.LinkAdd(dummy); err != nil {
				return err
			}
			var err error
			vtepDevice, err = netlink.LinkByName(vtepDeviceName)
			return err
		})).To(Succeed())

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

		createVRFInNamespace(testNS, params.VRF)
		cfg := newConfiguratorForTest(ovsClient)
		cfg.vtepDeviceIndex = vtepDevice.Attrs().Index
		Expect(cfg.SetupL2VNI(context.Background(), params)).To(Succeed())

		vxlanName := vxLanNameFromVNI(params.VNI)
		Eventually(func(g Gomega) {
			_ = netnamespace.In(testNS, func() error {
				link, err := netlink.LinkByName(vxlanName)
				g.Expect(err).NotTo(HaveOccurred())
				vxlan, ok := link.(*netlink.Vxlan)
				g.Expect(ok).To(BeTrue())
				g.Expect(vxlan.VtepDevIndex).To(Equal(vtepDevice.Attrs().Index))
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should set up the L2VNI in the namespace resolved for the pass", func() {
		cfg := newConfiguratorForTest(ovsClient)

		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  "/nonexistent",
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.168.1.0/24"},
		}

		createVRFInNamespace(testNS, params.VRF)
		Expect(cfg.SetupL2VNI(context.Background(), params)).To(Succeed())

		Eventually(func(g Gomega) {
			_ = netnamespace.In(testNS, func() error {
				validateL2VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should leave veth MTU at default when no underlay interface is configured", func() {
		// No fake underlay is set up here, so FindUnderlayMTU returns 0
		// and SetVethMTUForTunnelOverhead must leave the veth MTU untouched.
		// HostMaster is intentionally omitted so the host veth is not
		// attached to a bridge — Linux bridges auto-clamp their MTU to
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

		createVRFInNamespace(testNS, params.VRF)
		cfg := newConfiguratorForTest(ovsClient)
		err := cfg.SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateBothVethMTUs(g, testNS, params.VNI, defaultVethMTU)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should convert a pre-existing gateway bridge into a VLAN endpoint", func() {
		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF:       "testred",
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       100,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.168.1.1/24"},
		}
		createVRFInNamespace(testNS, params.VRF)

		By("pre-creating a non-VLAN-filtering gateway bridge")
		Expect(netnamespace.In(testNS, func() error {
			if err := netlink.LinkAdd(&netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: BridgeName(params.VNI)}}); err != nil {
				return err
			}
			current, err := netlink.LinkByName(BridgeName(params.VNI))
			if err != nil {
				return err
			}
			mac, err := BridgeFixedMAC(params.VNI)
			if err != nil {
				return err
			}
			if err := netlink.LinkSetHardwareAddr(current, mac); err != nil {
				return err
			}
			for _, ip := range params.L2GatewayIPs {
				if err := AssignIPToInterface(current, ip); err != nil {
					return err
				}
			}
			vrf, err := netlink.LinkByName(params.VRF)
			if err != nil {
				return err
			}
			return linkSetMaster(current, vrf)
		})).To(Succeed())

		cfg := newConfiguratorForTest(ovsClient)
		Expect(cfg.SetupL2VNI(context.Background(), params)).To(Succeed())

		Eventually(func(g Gomega) {
			_ = netnamespace.In(testNS, func() error {
				validateL2VNI(g, params)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should bind multiple L2VNIs to one routing domain VRF", func() {
		createVRFInNamespace(testNS, "testred")
		params := []L2VNIParams{
			{
				VNIParams: VNIParams{
					VRF:       "testred",
					TargetNS:  testNSPath(),
					VTEPIP:    "192.170.0.9/32",
					VNI:       100,
					VXLanPort: new(int32(4789)),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
				HostMaster: &HostMaster{
					Name: new(bridgeName),
					Type: BridgeLinkType,
				},
			},
			{
				VNIParams: VNIParams{
					VRF:       "testred",
					TargetNS:  testNSPath(),
					VTEPIP:    "192.170.0.10/32",
					VNI:       101,
					VXLanPort: new(int32(4789)),
				},
				L2GatewayIPs: []string{"192.168.2.1/24"},
			},
		}

		cfg := newConfiguratorForTest(ovsClient)
		for _, p := range params {
			Expect(cfg.SetupL2VNI(context.Background(), p)).To(Succeed())
		}

		Eventually(func(g Gomega) {
			_ = netnamespace.In(testNS, func() error {
				for _, p := range params {
					validateL2VNI(g, p)
				}
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
	if params.LinkIPs.HostIPv4 != "" {
		hasIP, err := interfaceHasIP(hostLegLink, params.LinkIPs.HostIPv4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "host leg does not have IPv4", params.LinkIPs.HostIPv4)
	}

	// Check IPv6 address if provided
	if params.LinkIPs.HostIPv6 != "" {
		hasIP, err := interfaceHasIP(hostLegLink, params.LinkIPs.HostIPv6)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "host leg does not have IPv6", params.LinkIPs.HostIPv6)
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

	if params.LinkIPs == nil {
		return
	}
	validateVethForVNI(g, params.VNIParams)

	endpointLink, err := netlink.LinkByName(BridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "VLAN endpoint not found", BridgeName(params.VNI))
	hasNoIP, err := interfaceHasNoIP(endpointLink, netlink.FAMILY_ALL)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasNoIP).To(BeTrue(), "L3-SVI must not carry an address")

	vrfLink, err := netlink.LinkByName(params.VRF)
	g.Expect(err).NotTo(HaveOccurred(), "vrf not found", params.VRF)
	g.Expect(endpointLink.Attrs().MasterIndex).To(Equal(vrfLink.Attrs().Index))

	vethNames := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", vethNames.NamespaceSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
	g.Expect(peLegLink.Attrs().MasterIndex).To(Equal(vrfLink.Attrs().Index))

	// Check IPv4 address if provided
	if params.LinkIPs.NSIPv4 != "" {
		hasIP, err := interfaceHasIP(peLegLink, params.LinkIPs.NSIPv4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "PE leg does not have IPv4", params.LinkIPs.NSIPv4)
	}

	// Check IPv6 address if provided
	if params.LinkIPs.NSIPv6 != "" {
		hasIP, err := interfaceHasIP(peLegLink, params.LinkIPs.NSIPv6)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "PE leg does not have IPv6", params.LinkIPs.NSIPv6)
	}
}

func validateL2VNI(g Gomega, params L2VNIParams) {
	switchingBridge := validateVNI(g, params.VNIParams)
	validateVethForVNI(g, params.VNIParams)

	vethNames := vethNamesFromVNI(params.VNI)
	peLegLink, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "veth pe side not found", vethNames.NamespaceSide)
	g.Expect(peLegLink.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))
	g.Expect(peLegLink.Attrs().MasterIndex).To(Equal(switchingBridge.Index))

	hasNoIP, err := interfaceHasNoIP(peLegLink, netlink.FAMILY_V4)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasNoIP).To(BeTrue(), "pe leg does have ip")

	validateVlanMembership(g, peLegLink, []vlanMembership{{VID: internalVLANID, PVID: true, Untagged: true}})

	endpointLink, err := netlink.LinkByName(BridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "VLAN endpoint not found", BridgeName(params.VNI))

	if len(params.L2GatewayIPs) == 0 {
		hasNoIP, err := interfaceHasNoIP(endpointLink, netlink.FAMILY_ALL)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasNoIP).To(BeTrue(), "endpoint does have ip")
		g.Expect(endpointLink.Attrs().MasterIndex).To(BeZero(), "gateway-less endpoint should not be in a VRF")
		return
	}

	for _, ip := range params.L2GatewayIPs {
		hasIP, err := interfaceHasIP(endpointLink, ip)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "endpoint does not have ip", ip)
	}
	validateBridgeMacAddress(g, endpointLink, params.VNI)

	vrfLink, err := netlink.LinkByName(params.VRF)
	g.Expect(err).NotTo(HaveOccurred(), "vrf not found", params.VRF)
	g.Expect(endpointLink.Attrs().MasterIndex).To(Equal(vrfLink.Attrs().Index))

	mac, err := BridgeFixedMAC(params.VNI)
	g.Expect(err).NotTo(HaveOccurred())
	validateLocalFDB(g, switchingBridge, internalVLANID, mac)
}

// validateVNI checks the shared VLAN-aware device model of an L2VNI or L3VNI
// and returns the private switching bridge.
func validateVNI(g Gomega, params VNIParams) *netlink.Bridge {
	switchingBridgeLink, err := netlink.LinkByName(switchingBridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "switching bridge not found", switchingBridgeName(params.VNI))
	switchingBridge := switchingBridgeLink.(*netlink.Bridge)
	g.Expect(switchingBridge.VlanFiltering).NotTo(BeNil())
	g.Expect(*switchingBridge.VlanFiltering).To(BeTrue(), "switching bridge must filter VLANs")
	g.Expect(switchingBridge.VlanDefaultPVID).NotTo(BeNil())
	g.Expect(*switchingBridge.VlanDefaultPVID).To(Equal(uint16(0)), "switching bridge must not have a default PVID")
	g.Expect(switchingBridge.MasterIndex).To(BeZero(), "switching bridge must not belong to a VRF")
	g.Expect(switchingBridge.OperState).To(BeEquivalentTo(netlink.OperUp))

	validateVlanMembership(g, switchingBridge, []vlanMembership{{VID: internalVLANID}})

	validateSVD(g, params, switchingBridge)

	endpointLink, err := netlink.LinkByName(BridgeName(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "VLAN endpoint not found", BridgeName(params.VNI))
	endpoint, ok := endpointLink.(*netlink.Vlan)
	g.Expect(ok).To(BeTrue(), "%s must be a VLAN device, got %s", BridgeName(params.VNI), endpointLink.Type())
	g.Expect(endpoint.VlanId).To(Equal(int(internalVLANID)))
	g.Expect(endpoint.ParentIndex).To(Equal(switchingBridge.Index))
	g.Expect(endpoint.OperState).To(BeEquivalentTo(netlink.OperUp))

	addrGenModeNone, err := checkAddrGenModeNone(endpoint)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(addrGenModeNone).To(BeTrue(), "VLAN endpoint must have addr_gen_mode=1")

	return switchingBridge
}

func validateSVD(g Gomega, params VNIParams, switchingBridge *netlink.Bridge) *netlink.Vxlan {
	vtepDev, err := netlink.LinkByName(TunnelLoopback)
	g.Expect(err).NotTo(HaveOccurred(), "vtep device not found %q", TunnelLoopback)

	vxlanLink, err := netlink.LinkByName(vxLanNameFromVNI(params.VNI))
	g.Expect(err).NotTo(HaveOccurred(), "vxlan link not found %q", vxLanNameFromVNI(params.VNI))
	vxlan := vxlanLink.(*netlink.Vxlan)

	g.Expect(vxlan.FlowBased).To(BeTrue(), "VXLAN must be in collect-metadata mode")
	g.Expect(vxlan.VxlanId).To(BeZero(), "external VXLAN must not have a fixed VNI")
	g.Expect(params.VXLanPort).NotTo(BeNil())
	g.Expect(vxlan.Port).To(Equal(int(*params.VXLanPort)))
	g.Expect(vxlan.VtepDevIndex).To(Equal(vtepDev.Attrs().Index))
	g.Expect(vxlan.MasterIndex).To(Equal(switchingBridge.Index))

	enabled, err := vniFilterEnabled(vxlan.Index)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(enabled).To(BeTrue(), "VXLAN VNI filtering must be enabled")

	g.Expect(checkVXLanPostSetup(vxlan)).To(Succeed())

	protinfo, err := netlink.LinkGetProtinfo(vxlan)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(protinfo.VlanTunnel).To(BeTrue(), "VXLAN must have vlan_tunnel on")

	mappings, err := vxlanTunnelMappings(vxlan.Index)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mappings).To(ContainElement(nl.TunnelInfo{Vid: internalVLANID, TunId: uint32(params.VNI)}))

	entries, err := vniFilterEntries(vxlan.Index)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(entries).To(ContainElement(vniFilterRange{Start: uint32(params.VNI), End: uint32(params.VNI)}))

	return vxlan
}

func validateVlanMembership(g Gomega, link netlink.Link, desired []vlanMembership) {
	all, err := netlink.BridgeVlanList()
	g.Expect(err).NotTo(HaveOccurred())
	observed := expandVlanMemberships(all[int32(link.Attrs().Index)])
	for _, want := range desired {
		got, found := indexOfMembership(observed, want.VID)
		g.Expect(found).To(BeTrue(), "VLAN %d missing on %s: %v", want.VID, link.Attrs().Name, observed)
		g.Expect(got.PVID).To(Equal(want.PVID), "VLAN %d PVID flag on %s", want.VID, link.Attrs().Name)
		g.Expect(got.Untagged).To(Equal(want.Untagged), "VLAN %d untagged flag on %s", want.VID, link.Attrs().Name)
	}
}

func indexOfMembership(memberships []vlanMembership, vid uint16) (vlanMembership, bool) {
	for _, membership := range memberships {
		if membership.VID == vid {
			return membership, true
		}
	}
	return vlanMembership{}, false
}

func validateLocalFDB(g Gomega, bridge *netlink.Bridge, vid uint16, mac net.HardwareAddr) {
	entries, err := netlink.NeighList(bridge.Index, unix.AF_BRIDGE)
	g.Expect(err).NotTo(HaveOccurred())
	found := false
	for _, entry := range entries {
		if entry.Vlan == int(vid) && bytes.Equal(entry.HardwareAddr, mac) && entry.State&netlink.NUD_PERMANENT != 0 {
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), "no local FDB entry for %s on VLAN %d of %s", mac, vid, bridge.Name)
}

func checkVXLanPostSetup(vxlan *netlink.Vxlan) error {
	// Verify 'bridge_slave learning off' to make sure that MAC learning is disabled (note: different from 'nolearning').
	protinfo, err := netlink.LinkGetProtinfo(vxlan)
	if err != nil {
		return fmt.Errorf("failed to retrieve VXLAN protinfo, err: %q", err)
	}
	if protinfo.Learning {
		return errors.New("MAC learning is enabled")
	}
	if !protinfo.NeighSuppress {
		return errors.New("neighbor suppression is disabled")
	}
	addrGenModeNone, err := checkAddrGenModeNone(vxlan)
	if err != nil {
		return fmt.Errorf("failed to retrieve addr_gen_mode, err: %q", err)
	}
	if !addrGenModeNone {
		return errors.New("addr_gen_mode is not set to none")
	}
	return nil
}

func validateVRF(g Gomega, vrfName string) (netlink.Link, *netlink.Vrf) {
	vrfLink, err := netlink.LinkByName(vrfName)
	g.Expect(err).NotTo(HaveOccurred(), "vrf not found", vrfName)
	vrf, isVrf := vrfLink.(*netlink.Vrf)
	g.Expect(isVrf).To(BeTrue(), "link %s is not a VRF", vrfName)
	g.Expect(vrf.OperState).To(BeEquivalentTo(netlink.OperUp))
	return vrfLink, vrf
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

func checkLinkExists(g Gomega, name string) {
	_, err := netlink.LinkByName(name)
	g.Expect(err).NotTo(HaveOccurred(), "link not found %q", name)
}

func validateVNIIsNotConfigured(g Gomega, params VNIParams) {
	checkLinkdeleted(g, vxLanNameFromVNI(params.VNI))
	checkLinkdeleted(g, BridgeName(params.VNI))
	checkLinkdeleted(g, switchingBridgeName(params.VNI))

	vethNames := vethNamesFromVNI(params.VNI)
	checkLinkdeleted(g, vethNames.NamespaceSide)
}

func checkAddrGenModeNone(l netlink.Link) (bool, error) {
	fileName := fmt.Sprintf("/proc/sys/net/ipv6/conf/%s/addr_gen_mode", l.Attrs().Name)
	addrGenMode, err := os.ReadFile(fileName)
	if err != nil {
		return false, err
	}

	return strings.Trim(string(addrGenMode), "\n") == "1", nil
}

func setupTunnelLoopback(ns netns.NsHandle) {
	handle, err := netlink.NewHandleAt(ns)
	Expect(err).NotTo(HaveOccurred())
	defer handle.Close()

	tunnelEndpoint := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: TunnelLoopback}}
	Expect(handle.LinkAdd(tunnelEndpoint)).To(Succeed())
	Expect(handle.LinkSetUp(tunnelEndpoint)).To(Succeed())
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

func validateVethMTU(g Gomega, vethNames VethNames, expectedMTU int) {
	hostLeg, err := netlink.LinkByName(vethNames.HostSide)
	g.Expect(err).NotTo(HaveOccurred(), "host veth not found %q", vethNames.HostSide)
	g.Expect(hostLeg.Attrs().MTU).To(Equal(expectedMTU),
		"host veth MTU should be %d, got %d", expectedMTU, hostLeg.Attrs().MTU)
}

func validateNSVethMTU(g Gomega, vethNames VethNames, expectedMTU int) {
	peLeg, err := netlink.LinkByName(vethNames.NamespaceSide)
	g.Expect(err).NotTo(HaveOccurred(), "pe veth not found %q", vethNames.NamespaceSide)
	g.Expect(peLeg.Attrs().MTU).To(Equal(expectedMTU),
		"pe veth MTU should be %d, got %d", expectedMTU, peLeg.Attrs().MTU)
}

func validateBothVethMTUs(g Gomega, ns netns.NsHandle, vni int32, expectedMTU int) {
	vethNames := vethNamesFromVNI(vni)
	validateVethMTU(g, vethNames, expectedMTU)
	_ = netnamespace.In(ns, func() error {
		validateNSVethMTU(g, vethNames, expectedMTU)
		return nil
	})
}

// defaultVethMTU is the MTU veth pairs receive when no explicit MTU is set.
const defaultVethMTU = 1500

// setupFakeUnderlay creates a dummy interface inside the given namespace with
// the underlay special address and a configurable MTU, so that FindUnderlayMTU
// can locate it. It is intended for unit tests exercising the MTU propagation
// behavior of VNIConfigurator.SetupL2VNI / SetupL3VNI.
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

func removeFakeUnderlay(ns netns.NsHandle, name string) {
	Expect(netnamespace.In(ns, func() error {
		return RemoveLinkByName(name)
	})).NotTo(HaveOccurred(), "failed to remove fake underlay")
}

// newConfiguratorForTest builds a VNI configurator for the test namespace,
// borrowing the given OVS client, and closes its namespace handle when the test
// finishes.
func newConfiguratorForTest(ovsClient *OVSClient) *VNIConfigurator {
	ns, err := netns.GetFromPath(testNSPath())
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(ns.Close()).To(Succeed())
	})
	cfg, err := NewVNIConfigurator(ns, ovsClient)
	Expect(err).NotTo(HaveOccurred())
	return cfg
}

func createVRFInNamespace(ns netns.NsHandle, name string) {
	err := netnamespace.In(ns, func() error {
		return setupVRF(name)
	})
	Expect(err).NotTo(HaveOccurred(), "failed to create VRF %s", name)
}

func l3VNISVIMAC(ns netns.NsHandle, vni int32) (net.HardwareAddr, error) {
	var mac net.HardwareAddr
	err := inVLANEndpoint(ns, vni, func(endpoint netlink.Link) error {
		mac = endpoint.Attrs().HardwareAddr
		return nil
	})
	return mac, err
}

func inVLANEndpoint(ns netns.NsHandle, vni int32, fn func(endpoint netlink.Link) error) error {
	return netnamespace.In(ns, func() error {
		endpoint, err := netlink.LinkByName(BridgeName(vni))
		if err != nil {
			return err
		}
		return fn(endpoint)
	})
}
