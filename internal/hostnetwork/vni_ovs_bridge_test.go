// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/ovsmodel"
	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var _ = Describe("L2 VNI configuration with OVS bridges", func() {
	var testNS netns.NsHandle

	BeforeEach(func() {
		cleanTest(testNSName)
		testNS = createTestNS(testNSName)
		setupLoopback(testNS)
	})

	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should work with a single L2VNI using auto-created OVS bridge", func() {
		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF: "testred", TargetNS: testNSPath(),
				VTEPIP: "192.170.0.9/32", VNI: 100, VXLanPort: 4789,
			},
			HostMaster: &HostMaster{Type: OVSBridgeLinkType, AutoCreate: true},
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

		By("checking the VNI and OVS bridge are removed")
		vethNames := vethNamesFromVNI(params.VNI)
		Eventually(func(g Gomega) {
			checkLinkdeleted(g, vethNames.HostSide)
			checkOVSHostBridgeDeleted(g, params)
			_ = netnamespace.In(testNS, func() error {
				validateVNIIsNotConfigured(g, params.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with a single L2VNI using pre-existing named OVS bridge", func() {
		const bridgeName = "test-ovs-br"
		Expect(createExternalOVSBridge(bridgeName)).To(Succeed(), "must pre-provision an OVS bridge")

		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF: "testred", TargetNS: testNSPath(),
				VTEPIP: "192.170.0.9/32", VNI: 100, VXLanPort: 4789,
			},
			HostMaster: &HostMaster{Type: OVSBridgeLinkType, Name: bridgeName},
		}

		err := SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, params)
			checkOVSBridgeExists(g, bridgeName)
			checkVethAttachedToOVSBridge(g, bridgeName, vethNamesFromVNI(params.VNI).HostSide)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("removing the VNI")
		err = RemoveNonConfiguredVNIs(testNSPath(), []VNIParams{})
		Expect(err).NotTo(HaveOccurred())

		By("checking the bridge persists but veth is cleaned up")
		vethNames := vethNamesFromVNI(params.VNI)
		Eventually(func(g Gomega) {
			checkOVSBridgeExists(g, bridgeName) // Bridge should still exist
			checkLinkdeleted(g, vethNames.HostSide)
			checkVethNotAttachedToOVSBridge(g, bridgeName, vethNames.HostSide)
			_ = netnamespace.In(testNS, func() error {
				validateVNIIsNotConfigured(g, params.VNIParams)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with multiple L2VNIs with different auto-created OVS bridges + cleanup", func() {
		params1 := L2VNIParams{
			VNIParams: VNIParams{
				VRF: "testred", TargetNS: testNSPath(),
				VTEPIP: "192.170.0.9/32", VNI: 100, VXLanPort: 4789,
			},
			HostMaster: &HostMaster{Type: OVSBridgeLinkType, AutoCreate: true},
		}

		params2 := L2VNIParams{
			VNIParams: VNIParams{
				VRF: "testgreen", TargetNS: testNSPath(),
				VTEPIP: "192.170.0.9/32", VNI: 101, VXLanPort: 4789,
			},
			HostMaster: &HostMaster{Type: OVSBridgeLinkType, AutoCreate: true},
		}

		err := SetupL2VNI(context.Background(), params1)
		Expect(err).NotTo(HaveOccurred())
		err = SetupL2VNI(context.Background(), params2)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, params1)
			validateL2HostLeg(g, params2)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("removing VNI 100, keeping VNI 101")
		err = RemoveNonConfiguredVNIs(testNSPath(), []VNIParams{params2.VNIParams})
		Expect(err).NotTo(HaveOccurred())

		By("checking VNI 100 removed, VNI 101 persists")
		Eventually(func(g Gomega) {
			checkOVSHostBridgeDeleted(g, params1)
			checkOVSBridgeExists(g, hostBridgeName(params2.VNI))
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should be idempotent with OVS bridges", func() {
		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF: "testred", TargetNS: testNSPath(),
				VTEPIP: "192.170.0.9/32", VNI: 100, VXLanPort: 4789,
			},
			HostMaster: &HostMaster{Type: OVSBridgeLinkType, AutoCreate: true},
		}

		err := SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		By("calling SetupL2VNI a second time")
		err = SetupL2VNI(context.Background(), params)
		Expect(err).NotTo(HaveOccurred(), "second SetupL2VNI should be idempotent")

		Eventually(func(g Gomega) {
			validateL2HostLeg(g, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should configure L2 gateway IP with OVS bridge", func() {
		gwIP := "10.10.100.1/24"
		params := L2VNIParams{
			VNIParams: VNIParams{
				VRF: "testred", TargetNS: testNSPath(),
				VTEPIP: "192.170.0.9/32", VNI: 100, VXLanPort: 4789,
			},
			L2GatewayIPs: []string{gwIP},
			HostMaster:   &HostMaster{Type: OVSBridgeLinkType, AutoCreate: true},
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
	})
})

func checkOVSBridgeExists(g Gomega, bridgeName string) {
	bridge, err := getOVSBridge(bridgeName)
	g.Expect(err).NotTo(HaveOccurred(), "failed to get OVS bridge %q", bridgeName)
	g.Expect(bridge).NotTo(BeNil())
	g.Expect(bridge.Name).To(Equal(bridgeName))
}

func checkOVSHostBridgeDeleted(g Gomega, params L2VNIParams) {
	g.Expect(params.HostMaster).ToNot(BeNil())
	g.Expect(params.HostMaster.Type).To(Equal(OVSBridgeLinkType))
	g.Expect(params.HostMaster.AutoCreate).To(BeTrue())

	hostBridge := hostBridgeName(params.VNI)
	checkOVSBridgeDeleted(g, hostBridge)
}

func checkOVSBridgeDeleted(g Gomega, bridgeName string) {
	_, err := getOVSBridge(bridgeName)
	g.Expect(err).To(HaveOccurred(), "OVS bridge %q should not exist", bridgeName)
}

// checkVethAttachedToOVSBridge validates that a veth is attached to an OVS bridge
func checkVethAttachedToOVSBridge(g Gomega, bridgeName, vethName string) {
	hasPort, err := ovsBridgeHasPort(bridgeName, vethName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasPort).To(BeTrue(), "veth %s should be attached to OVS bridge %s", vethName, bridgeName)
}

// getOVSBridge retrieves an OVS bridge by name, returns error if not found
func getOVSBridge(name string) (*ovsmodel.Bridge, error) {
	ctx := context.Background()
	ovs, err := NewOVSClient(ctx)
	if err != nil {
		return nil, err
	}
	defer ovs.Close()

	_, err = ovs.Monitor(ctx, ovs.NewMonitor(libovsclient.WithTable(&ovsmodel.Bridge{})))
	if err != nil {
		return nil, err
	}

	bridge := &ovsmodel.Bridge{Name: name}
	err = ovs.Get(ctx, bridge)
	if err != nil {
		return nil, err
	}
	return bridge, nil
}

// ovsBridgeHasPort checks if a port is attached to an OVS bridge
func ovsBridgeHasPort(bridgeName, portName string) (bool, error) {
	ctx := context.Background()
	ovs, err := NewOVSClient(ctx)
	if err != nil {
		return false, err
	}
	defer ovs.Close()

	_, err = ovs.Monitor(ctx, ovs.NewMonitor(
		libovsclient.WithTable(&ovsmodel.Bridge{}),
		libovsclient.WithTable(&ovsmodel.Port{}),
	))
	if err != nil {
		return false, err
	}

	bridge := &ovsmodel.Bridge{Name: bridgeName}
	err = ovs.Get(ctx, bridge)
	if err != nil {
		return false, err
	}

	port := &ovsmodel.Port{Name: portName}
	err = ovs.Get(ctx, port)
	if err != nil {
		return false, nil // Port doesn't exist
	}

	for _, portUUID := range bridge.Ports {
		if portUUID == port.UUID {
			return true, nil
		}
	}
	return false, nil
}

// waitForOVSInterface waits for an OVS interface to appear using netlink notifications
func waitForOVSInterface(name string) error {
	if _, err := netlink.LinkByName(name); err == nil {
		return nil
	}

	ch := make(chan netlink.LinkUpdate)
	done := make(chan struct{})
	defer close(done)

	if err := netlink.LinkSubscribe(ch, done); err != nil {
		return fmt.Errorf("failed to subscribe to link updates: %w", err)
	}

	timeout := time.After(5 * time.Second)
	for {
		select {
		case update := <-ch:
			if update.Link.Attrs().Name == name {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("timeout waiting for OVS interface %s to appear", name)
		}
	}
}

// checkVethNotAttachedToOVSBridge validates that a veth port has been removed from an OVS bridge
func checkVethNotAttachedToOVSBridge(g Gomega, bridgeName, vethName string) {
	hasPort, err := ovsBridgeHasPort(bridgeName, vethName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasPort).To(BeFalse(), "veth %s should not be attached to OVS bridge %s after cleanup", vethName, bridgeName)
}

// createExternalOVSBridge creates an OVS bridge without the "created-by: openperouter"
// marker, simulating a bridge provisioned by the user externally.
func createExternalOVSBridge(name string) error {
	cmd := exec.Command("ovs-vsctl", "add-br", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ovs-vsctl add-br %s failed: %s: %w", name, string(out), err)
	}
	return waitForOVSInterface(name)
}

// cleanupOVSBridges removes all test OVS bridges
func cleanupOVSBridges() {
	cmd := exec.Command("ovs-vsctl", "list-br")
	output, err := cmd.Output()
	if err != nil {
		return // OVS not available
	}

	bridges := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, br := range bridges {
		if strings.HasPrefix(br, "br-hs-") || strings.HasPrefix(br, "test-ovs-") {
			_ = exec.Command("ovs-vsctl", "del-br", br).Run()
		}
	}
}
