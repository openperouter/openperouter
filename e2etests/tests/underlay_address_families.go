// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// Test IPv6 and Unnumbered peering in a separate context, and run a handful of tests only for l2vpn, l3vpn and
// passthrough. These tests are basically copies of some tests in other files. The reasons for using a separate context
// are:
// - This reduces the number of overall tests run (by just spotchecking that IPv6 and Unnumbered functionality work).
// - It reduces the number of underlay creations and teardowns.
var _ = Describe("Routes between bgp and the fabric", Ordered, func() {
	DescribeTableSubtree("underlay address family", runUnderlayTests,
		Entry("IPv6", ipfamily.IPv6, infra.UnderlayIPv6),
		Entry("Unnumbered", ipfamily.Unnumbered, infra.UnderlayUnnumbered),
	)
})

var runUnderlayTests = func(af ipfamily.Family, underlay v1alpha1.Underlay) {
	const (
		testNamespace             = "test-namespace"
		linuxBridgeHostAttachment = "linux-bridge"
		l2GatewayIP               = "192.171.24.1/24"
		nadMaster                 = "br-hs-110"
		firstPodIP                = "192.171.24.2/24"
		secondPodIP               = "192.171.24.3/24"
	)

	var (
		cs      clientset.Interface
		routers openperouter.Routers
		nodes   []corev1.Node
	)

	passthrough := v1alpha1.L3Passthrough{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "passthrough",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3PassthroughSpec{
			HostSession: v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64515,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.10.0/24",
					IPv6: "2001:db8:1::/64",
				},
			},
		},
	}

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF:          ptr.To("red"),
			VNI:          110,
			L2GatewayIPs: []string{l2GatewayIP},
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		},
	}

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

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routers.Dump(GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("setting address family on leafkind node")
		err = infra.UpdateLeafKindConfig(nodes, infra.LeafKindConfiguration{AddressFamily: af})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		By("resetting address family on leafkind node")
		err = infra.UpdateLeafKindConfig(nodes, infra.LeafKindConfiguration{})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())

		Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
		Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
	})

	Context("passthrough and frr-k8s", func() {
		var frrk8sPods []*corev1.Pod

		BeforeEach(func() {
			var err error
			frrk8sPods, err = frrk8s.Pods(cs)
			Expect(err).NotTo(HaveOccurred())
			DumpPods("FRRK8s pods", frrk8sPods)
		})

		It("translates BGP incoming routes as BGP routes", func() {
			frrK8sPassthroughConfigs, err := frrk8s.ConfigFromHostSession(passthrough.Spec.HostSession, passthrough.Name)
			Expect(err).NotTo(HaveOccurred())
			err = Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{
					passthrough,
				},
				FRRConfigurations: frrK8sPassthroughConfigs,
			})
			Expect(err).NotTo(HaveOccurred())
			validateFRRK8sSessionForHostSession(passthrough.Name, passthrough.Spec.HostSession, Established, frrk8sPods...)

			shouldExist := true

			By("advertising routes from both leaves")
			Expect(infra.LeafAConfig.ChangePrefixes(leafADefaultPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
			Expect(infra.LeafBConfig.ChangePrefixes(leafBDefaultPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())

			By("checking routes are propagated via BGP")
			for _, frrk8sPod := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8sPod, passthrough.Spec.HostSession, leafADefaultPrefixes, shouldExist)
				checkBGPPrefixesForHostSession(frrk8sPod, passthrough.Spec.HostSession, leafBDefaultPrefixes, shouldExist)
			}

			By("removing routes from the leaf B")
			Expect(infra.LeafAConfig.ChangePrefixes(leafADefaultPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
			Expect(infra.LeafBConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())

			By("checking routes are propagated via BGP")
			for _, frrk8sPod := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8sPod, passthrough.Spec.HostSession, leafADefaultPrefixes, shouldExist)
				checkBGPPrefixesForHostSession(frrk8sPod, passthrough.Spec.HostSession, leafBDefaultPrefixes, !shouldExist)
			}
		})
	})

	Context("with l2vni and l3vni", func() {
		BeforeEach(func() {
			By("setting redistribute connected on leaves")
			redistributeConnectedForLeaf(infra.LeafAConfig)
			redistributeConnectedForLeaf(infra.LeafBConfig)

			_, err := k8s.CreateNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			err := k8s.DeleteNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create two pods connected to the l2 overlay", func() {
			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{
					vniRed,
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VniRed,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			nad, err := k8s.CreateMacvlanNad("110", testNamespace, nadMaster, []string{l2GatewayIP})
			Expect(err).NotTo(HaveOccurred())

			firstPodIPs := []string{firstPodIP}
			secondPodIPs := []string{secondPodIP}
			hostARedIPs := []string{infra.HostARedIPv4}
			hostBRedIPs := []string{infra.HostBRedIPv4}

			By("creating the pods")
			firstPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace, k8s.WithNad(nad.Name, testNamespace, firstPodIPs), k8s.OnNode(nodes[0].Name))
			Expect(err).NotTo(HaveOccurred())
			secondPod, err := k8s.CreateAgnhostPod(cs, "pod2", testNamespace, k8s.WithNad(nad.Name, testNamespace, secondPodIPs), k8s.OnNode(nodes[1].Name))
			Expect(err).NotTo(HaveOccurred())

			By("removing the default gateway via the primary interface")
			Expect(removeGatewayFromPod(firstPod)).To(Succeed())
			Expect(removeGatewayFromPod(secondPod)).To(Succeed())

			firstPodExecutor := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
			secondPodExecutor := executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost")
			hostARedExecutor := executor.ForContainer("clab-kind-hostA_red")

			tests := []struct {
				exec    executor.Executor
				from    string
				to      string
				fromIPs []string
				toIPs   []string
			}{
				{exec: firstPodExecutor, from: "firstPod", to: "secondPod", fromIPs: firstPodIPs, toIPs: secondPodIPs},
				{exec: secondPodExecutor, from: "secondPod", to: "firstPod", fromIPs: secondPodIPs, toIPs: firstPodIPs},
				{exec: firstPodExecutor, from: "firstPod", to: "hostARed", fromIPs: firstPodIPs, toIPs: hostARedIPs},
				{exec: firstPodExecutor, from: "firstPod", to: "hostBRed", fromIPs: firstPodIPs, toIPs: hostBRedIPs},
				{exec: secondPodExecutor, from: "secondPod", to: "hostARed", fromIPs: secondPodIPs, toIPs: hostARedIPs},
				{exec: secondPodExecutor, from: "secondPod", to: "hostBRed", fromIPs: secondPodIPs, toIPs: hostBRedIPs},
				{exec: hostARedExecutor, from: "hostARed", to: "firstPod", fromIPs: hostARedIPs, toIPs: firstPodIPs},
			}

			for _, test := range tests {
				By(fmt.Sprintf("checking reachability from %s to %s", test.from, test.to))
				Expect(test.fromIPs).To(HaveLen(len(test.toIPs)))
				for i, fromIP := range test.fromIPs {
					from := discardAddressLength(fromIP)
					to := discardAddressLength(test.toIPs[i])
					checkPodIsReachable(test.exec, from, to)
				}
			}
		})
	})
}
