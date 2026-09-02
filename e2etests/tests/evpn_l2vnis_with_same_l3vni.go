// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// Two L2VNIs (blue and green) attached to the same L3VNI (red) form a symmetric
// IRB topology: each L2VNI is its own bridge domain with its own anycast gateway,
// and the shared L3VNI VRF routes between them. This test guards that capability
// against regressions: cross-subnet pod traffic must be routed (source IP
// preserved, not NATed) through the shared VRF, and either subnet must still reach
// a host on the external red L3 domain.
var _ = Describe("Multiple L2VNIs sharing one L3VNI (symmetric IRB)", Ordered, func() {
	var cs clientset.Interface

	const (
		linuxBridgeHostAttachment = v1alpha1.LinuxBridge
		testNamespace             = "test-l2vnis-same-l3vni"

		blueVNI  = 110
		greenVNI = 120
	)

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: 100,
		},
	}

	l2VniBlue := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blue",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain: l3vniRoutingDomain("red"),
			VNI:           blueVNI,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		},
	}

	l2VniGreen := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "green",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain: l3vniRoutingDomain("red"),
			VNI:           greenVNI,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		},
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		cs = k8sclient.New()
		routers, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routers.Dump(ginkgo.GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())
		By("waiting for all router pods to be ready after removing the underlay")
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs, testNamespace)
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafBConfig.Reset()).To(Succeed())
		Expect(Updater.CleanButUnderlay()).To(Succeed())
		Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())
	})

	type testCase struct {
		blueGatewayIPs, greenGatewayIPs []string
		bluePodIPs, greenPodIPs         []string
		hostARedIPs                     []string
	}

	DescribeTable("should route between the two L2 overlays through the shared VRF", func(tc testCase) {
		By("setting redistribute connected on leaves so the external red subnet is advertised as a Type 5 route")
		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		blue := l2VniBlue.DeepCopy()
		blue.Spec.GatewayIPs = tc.blueGatewayIPs
		green := l2VniGreen.DeepCopy()
		green.Spec.GatewayIPs = tc.greenGatewayIPs

		By("creating the L3VNI and both L2VNIs pointing at it")
		Expect(Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{*blue, *green},
		})).To(Succeed())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		blueNad, err := k8s.CreateMacvlanNad(
			fmt.Sprintf("%d", blueVNI),
			testNamespace,
			fmt.Sprintf("br-hs-%d", blueVNI),
			tc.blueGatewayIPs,
		)
		Expect(err).NotTo(HaveOccurred())
		greenNad, err := k8s.CreateMacvlanNad(
			fmt.Sprintf("%d", greenVNI),
			testNamespace,
			fmt.Sprintf("br-hs-%d", greenVNI),
			tc.greenGatewayIPs,
		)
		Expect(err).NotTo(HaveOccurred())

		By("creating one pod per L2VNI on different nodes")
		bluePod, err := k8s.CreateAgnhostPod(
			cs,
			"blue",
			testNamespace,
			k8s.WithNad(blueNad.Name, testNamespace, tc.bluePodIPs),
			k8s.OnNode(nodes[0].Name),
		)
		Expect(err).NotTo(HaveOccurred())
		greenPod, err := k8s.CreateAgnhostPod(
			cs,
			"green",
			testNamespace,
			k8s.WithNad(greenNad.Name, testNamespace, tc.greenPodIPs),
			k8s.OnNode(nodes[1].Name),
		)
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface so cross-subnet traffic uses the anycast gateway")
		Expect(removeGatewayFromPod(bluePod)).To(Succeed())
		Expect(removeGatewayFromPod(greenPod)).To(Succeed())

		By("waiting for BGP sessions to establish on both nodes before traffic check")
		leafExec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				leafExec,
				validationParameters{
					fromName:    infra.KindLeaf,
					toName:      node.Name,
					neighborIP:  neighborIP,
					established: Established,
				},
			)
		}

		blueExecutor := executor.ForPod(bluePod.Namespace, bluePod.Name, "agnhost")
		greenExecutor := executor.ForPod(greenPod.Namespace, greenPod.Name, "agnhost")

		By("checking cross-subnet reachability between the two L2 overlays (routed through the red VRF)")
		Expect(tc.bluePodIPs).To(HaveLen(len(tc.greenPodIPs)))
		for i := range tc.bluePodIPs {
			bluePodIP := discardAddressLength(tc.bluePodIPs[i])
			greenPodIP := discardAddressLength(tc.greenPodIPs[i])
			// The clientip must be preserved, proving the traffic is routed in the
			// shared VRF and not NATed.
			checkPodIsReachable(blueExecutor, bluePodIP, greenPodIP)
			checkPodIsReachable(greenExecutor, greenPodIP, bluePodIP)
		}

		By("checking both L2 overlays can reach a host on the external red L3 domain")
		Expect(tc.bluePodIPs).To(HaveLen(len(tc.hostARedIPs)))
		for i := range tc.hostARedIPs {
			hostARedIP := discardAddressLength(tc.hostARedIPs[i])
			checkPodIsReachable(blueExecutor, discardAddressLength(tc.bluePodIPs[i]), hostARedIP)
			checkPodIsReachable(greenExecutor, discardAddressLength(tc.greenPodIPs[i]), hostARedIP)
		}
	},
		Entry("for single stack ipv4", testCase{
			blueGatewayIPs:  []string{"192.171.26.1/24"},
			greenGatewayIPs: []string{"192.171.27.1/24"},
			bluePodIPs:      []string{"192.171.26.2/24"},
			greenPodIPs:     []string{"192.171.27.2/24"},
			hostARedIPs:     []string{infra.HostARedIPv4},
		}),
		Entry("for dual stack", testCase{
			blueGatewayIPs:  []string{"192.171.26.1/24", "fd00:10:246:1::1/64"},
			greenGatewayIPs: []string{"192.171.27.1/24", "fd00:10:247:1::1/64"},
			bluePodIPs:      []string{"192.171.26.2/24", "fd00:10:246:1::2/64"},
			greenPodIPs:     []string{"192.171.27.2/24", "fd00:10:247:1::2/64"},
			hostARedIPs:     []string{infra.HostARedIPv4, infra.HostARedIPv6},
		}),
		Entry("for single stack ipv6", testCase{
			blueGatewayIPs:  []string{"fd00:10:246:1::1/64"},
			greenGatewayIPs: []string{"fd00:10:247:1::1/64"},
			bluePodIPs:      []string{"fd00:10:246:1::2/64"},
			greenPodIPs:     []string{"fd00:10:247:1::2/64"},
			hostARedIPs:     []string{infra.HostARedIPv6},
		}),
	)
})
