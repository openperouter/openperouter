// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
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
	"github.com/openperouter/openperouter/e2etests/pkg/url"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("Alpha: Named netns and kernel objects survive FRR crash", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

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

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: ptr.To("red"),
			VNI: 110,
			HostMaster: &v1alpha1.HostMaster{
				Type: "linux-bridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		},
	}

	BeforeAll(func() {
		if !NamedNSMode {
			Skip("named-ns mode is not enabled; skip alpha resiliency tests")
		}

		cs = k8sclient.New()
		oldRouters, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for router pods to roll after CleanAll")
		routers, err = openperouter.WaitForRolledRouters(cs, HostMode, oldRouters, 2*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(ginkgo.GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		redistributeConnectedForLeaf(infra.LeafAConfig)
		redistributeConnectedForLeaf(infra.LeafBConfig)

		err = Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{l2VniRed},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for all router pods to be ready")
		Eventually(func(g Gomega) {
			pods, err := openperouter.RouterPods(cs)
			g.Expect(err).NotTo(HaveOccurred())
			for _, p := range pods {
				g.Expect(k8s.PodIsReady(p)).To(BeTrue(), "pod %s must be ready", p.Name)
			}
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	It("should preserve named netns at /var/run/netns/perouter when FRR process crashes", func() {
		routerPods, err := openperouter.RouterPodsForNodes(cs, allNodes(cs))
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).NotTo(BeEmpty())
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName

		By("verifying named netns exists before crash")
		exists, err := openperouter.NamedNetnsExists(nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue(), "named netns must exist before FRR crash")

		By("verifying kernel objects exist before crash")
		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			present, err := openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
			Expect(err).NotTo(HaveOccurred())
			Expect(present).To(BeTrue(), "interface type %s must exist before crash", ifType)
		}

		By("killing the FRR container entrypoint process")
		frrExec := executor.ForPod(openperouter.Namespace, routerPod.Name, "frr")
		killFRREntrypoint(frrExec)

		By("immediately asserting named netns and kernel objects survived the crash")
		exists, err = openperouter.NamedNetnsExists(nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue(), "named netns must survive FRR crash immediately")

		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			present, err := openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
			Expect(err).NotTo(HaveOccurred())
			Expect(present).To(BeTrue(), "interface type %s must survive FRR crash immediately", ifType)
		}

		By("waiting for the FRR container to restart and become ready")
		Eventually(func(g Gomega) []v1.PodCondition {
			pod, err := cs.CoreV1().Pods(openperouter.Namespace).Get(context.Background(), routerPod.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			return pod.Status.Conditions
		}).
			WithTimeout(2*time.Minute).
			WithPolling(time.Second).
			Should(
				ContainElement(
					SatisfyAll(
						HaveField("Type", Equal(v1.PodReady)),
						HaveField("Status", Equal(v1.ConditionTrue)),
					),
				),
				"router pod should become ready after FRR restart",
			)

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)
	})
})

var _ = Describe("Beta: BGP Graceful Restart provides zero data plane disruption", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	restartTime, stalePathTime := uint32(120), uint32(360)
	underlayWithGR := v1alpha1.Underlay{}

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

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: ptr.To("red"),
			VNI: 110,
			HostMaster: &v1alpha1.HostMaster{
				Type: "linux-bridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		},
	}

	BeforeAll(func() {
		if !NamedNSMode {
			Skip("named-ns mode is not enabled; skip beta resiliency tests")
		}

		infra.Underlay.DeepCopyInto(&underlayWithGR)
		underlayWithGR.Spec.GracefulRestart = &v1alpha1.GracefulRestartConfig{
			RestartTime:   &restartTime,
			StalePathTime: &stalePathTime,
		}

		cs = k8sclient.New()
		oldRouters, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for router pods to roll after CleanAll")
		routers, err = openperouter.WaitForRolledRouters(cs, HostMode, oldRouters, 2*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(ginkgo.GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{underlayWithGR},
		})
		Expect(err).NotTo(HaveOccurred())

		redistributeConnectedForLeaf(infra.LeafAConfig)
		redistributeConnectedForLeaf(infra.LeafBConfig)
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for all router pods to be ready")
		Eventually(func(g Gomega) {
			pods, err := openperouter.RouterPods(cs)
			g.Expect(err).NotTo(HaveOccurred())
			for _, p := range pods {
				g.Expect(k8s.PodIsReady(p)).To(BeTrue(), "pod %s must be ready", p.Name)
			}
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	const testNamespace = "test-namespace-gr"

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
		if err := k8s.DeleteNamespace(cs, testNamespace); err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		By("waiting for all router pods to be ready")
		Eventually(func(g Gomega) {
			pods, err := openperouter.RouterPods(cs)
			g.Expect(err).NotTo(HaveOccurred())
			for _, p := range pods {
				g.Expect(k8s.PodIsReady(p)).To(BeTrue(), "pod %s must be ready", p.Name)
			}
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	// This test uses north-south L3 traffic (curl from hostA_red to a pod behind the PE
	// gateway) to verify minimal data plane disruption during a router pod deletion with BGP GR.
	// The data plane survives because:
	//   - The named netns (with br-pe-110, kernel ARP for the pod) persists across the restart.
	//   - zebra -K 60 preserves L3 kernel routes and nexthops (L3VNI path) during FRR restart.
	//   - BGP GR stale routes at leafkind keep the fabric routing intact throughout.
	//   - redistribute connected ensures the /24 Type-5 prefix is immediately re-advertised
	//     by the new FRR, so the /24 fallback covers any brief stale /32 host-route gap.
	It("should preserve named netns and maintain minimal data plane disruption when router pod is deleted", func() {
		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.L2GatewayIPs = []string{"192.171.24.1/24"}

		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{*l2VniRedWithGateway},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{"192.171.24.1/24"})
		Expect(err).NotTo(HaveOccurred())

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 1))

		By("creating the client pod")
		clientPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{"192.171.24.2/24"}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(clientPod)).To(Succeed())

		hostARedExecutor := executor.ForContainer("clab-kind-hostA_red")
		firstPodIP := "192.171.24.2"
		const port = "8090"
		hostPort := net.JoinHostPort(firstPodIP, port)
		urlStr := url.Format("http://%s/clientip", hostPort)

		By("verifying north/south traffic works before router pod deletion")
		Eventually(func() error {
			_, err := hostARedExecutor.Exec("curl", "-sS", "--max-time", "2", urlStr)
			return err
		}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

		By("identifying the router pod on clientPod's node")
		routerPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{clientPod.Spec.NodeName: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).To(HaveLen(1))
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName

		By("starting continuous traffic measurement")
		stopAndCount := measureTrafficLoss(hostARedExecutor, urlStr)

		By("deleting the router pod")
		err = cs.CoreV1().Pods(openperouter.Namespace).Delete(context.Background(), routerPod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("immediately asserting named netns survived pod deletion")
		exists, err := openperouter.NamedNetnsExists(nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue(), "named netns must survive pod deletion")

		By("waiting for a new router pod to become ready")
		Eventually(func(g Gomega) {
			newRouterPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{nodeName: true})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newRouterPods).To(HaveLen(1))
			newPod := newRouterPods[0]
			g.Expect(newPod.Name).NotTo(Equal(routerPod.Name), "a new router pod must be created")
			g.Expect(k8s.PodIsReady(newPod)).To(BeTrue(), "new router pod must be ready")
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)

		By("asserting data plane disruption is within acceptable bounds during router pod deletion and recovery")
		result := stopAndCount()
		By(fmt.Sprintf("==> %s", result.String()))
		Expect(result.eval()).To(Succeed(), "curl failures exceeded threshold during router pod deletion and recovery (%d/%d failed). Failed timestamps: %+v", result.failCount, result.totalCount, result.failedTimestamps)
	})

	// This test verifies East-West stretched L2 traffic resilience between two pods on
	// different nodes. Traffic flows pod-to-pod through the EVPN VXLAN fabric (no gateway,
	// pure L2). BGP GR and zebra -K 60 ensure that FDB entries (MAC→VTEP mappings) on the
	// client's PE survive the server's router pod restart, keeping traffic flowing with
	// minimal disruption.
	It("should maintain stretched L2 traffic across nodes with minimal disruption when a router pod is deleted", func() {
		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{l2VniRed},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		// No gateway IPs: pure L2 pod-to-pod. The kernel adds a connected subnet route
		// for 192.171.24.0/24 via the macvlan NIC automatically; no default route injection
		// or removeGatewayFromPod needed.
		nad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", nil)
		Expect(err).NotTo(HaveOccurred())

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "stretched L2 test requires at least 2 nodes")

		const (
			serverIP = "192.171.24.2"
			clientIP = "192.171.24.3"
			subnet   = "/24"
			port     = "8090"
		)

		By("creating the server pod on node 0")
		serverPod, err := k8s.CreateAgnhostPod(cs, "server", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{serverIP + subnet}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("creating the client pod on node 1")
		clientPod, err := k8s.CreateAgnhostPod(cs, "client", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{clientIP + subnet}),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		clientExec := executor.ForPod(clientPod.Namespace, clientPod.Name, "agnhost")
		hostPort := net.JoinHostPort(serverIP, port)
		urlStr := url.Format("http://%s/clientip", hostPort)

		DeferCleanup(func() {
			if !ginkgo.CurrentSpecReport().Failed() {
				return
			}
			leafExec := executor.ForContainer(infra.KindLeaf)
			out, err := leafExec.Exec("vtysh", "-c", "show bgp l2vpn evpn route type macip")
			if err != nil {
				ginkgo.GinkgoWriter.Printf("failed to dump leafkind Type-2 routes: %v\n", err)
			} else {
				ginkgo.GinkgoWriter.Printf("=== leafkind Type-2 EVPN routes ===\n%s\n", out)
			}
			out, err = leafExec.Exec("vtysh", "-c", "show evpn mac vni all")
			if err != nil {
				ginkgo.GinkgoWriter.Printf("failed to dump leafkind EVPN MACs: %v\n", err)
			} else {
				ginkgo.GinkgoWriter.Printf("=== leafkind EVPN MACs ===\n%s\n", out)
			}
		})

		By("verifying stretched L2 traffic works before router pod deletion")
		Eventually(func() error {
			_, err := clientExec.Exec("curl", "-sS", "--max-time", "2", urlStr)
			return err
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())

		By("identifying the router pod on the server's node")
		routerPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{serverPod.Spec.NodeName: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).To(HaveLen(1))
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName

		By("starting continuous traffic measurement")
		stopAndCount := measureTrafficLoss(clientExec, urlStr)

		By("deleting the router pod on the server's node")
		err = cs.CoreV1().Pods(openperouter.Namespace).Delete(context.Background(), routerPod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for a new router pod to become ready")
		Eventually(func(g Gomega) {
			newRouterPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{nodeName: true})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newRouterPods).To(HaveLen(1))
			newPod := newRouterPods[0]
			g.Expect(newPod.Name).NotTo(Equal(routerPod.Name), "a new router pod must be created")
			g.Expect(k8s.PodIsReady(newPod)).To(BeTrue(), "new router pod must be ready")
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)

		By("asserting stretched L2 disruption is within acceptable bounds during router pod deletion and recovery")
		result := stopAndCount()
		By(fmt.Sprintf("==> %s", result.String()))
		Expect(result.eval()).To(Succeed(), "curl failures exceeded threshold during router pod deletion and recovery (%d/%d failed). Failed timestamps: %+v", result.failCount, result.totalCount, result.failedTimestamps)
	})
})

var _ = Describe("Beta: Named netns auto-rebuilds after deletion", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	restartTime, stalePathTime := uint32(120), uint32(360)
	underlayWithGR := v1alpha1.Underlay{}

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

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: ptr.To("red"),
			VNI: 110,
			HostMaster: &v1alpha1.HostMaster{
				Type: "linux-bridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		},
	}

	BeforeAll(func() {
		if !NamedNSMode {
			Skip("named-ns mode is not enabled; skip beta netns rebuild tests")
		}

		infra.Underlay.DeepCopyInto(&underlayWithGR)
		underlayWithGR.Spec.GracefulRestart = &v1alpha1.GracefulRestartConfig{
			RestartTime:   &restartTime,
			StalePathTime: &stalePathTime,
		}

		cs = k8sclient.New()
		oldRouters, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for router pods to roll after CleanAll")
		routers, err = openperouter.WaitForRolledRouters(cs, HostMode, oldRouters, 2*time.Minute)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(ginkgo.GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{underlayWithGR},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for all router pods to be ready")
		Eventually(func(g Gomega) {
			pods, err := openperouter.RouterPods(cs)
			g.Expect(err).NotTo(HaveOccurred())
			for _, p := range pods {
				g.Expect(k8s.PodIsReady(p)).To(BeTrue(), "pod %s must be ready", p.Name)
			}
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	const testNamespace = "test-namespace-rebuild"

	It("should auto-recover when the named netns is deleted via ip netns delete", func() {
		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.L2GatewayIPs = []string{"192.171.24.1/24"}

		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{*l2VniRedWithGateway},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{"192.171.24.1/24"})
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			err = k8s.DeleteNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 1))

		By("creating the client pod")
		clientPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{"192.171.24.2/24"}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(clientPod)).To(Succeed())

		hostARedExecutor := executor.ForContainer("clab-kind-hostA_red")
		firstPodIP := "192.171.24.2"
		const port = "8090"
		hostPort := net.JoinHostPort(firstPodIP, port)
		urlStr := url.Format("http://%s/clientip", hostPort)

		By("waiting for BGP sessions to establish before traffic check")
		for _, nodeName := range []string{nodes[0].Name} {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				infra.KindLeaf,
				nodeName,
				executor.ForContainer(infra.KindLeaf),
				neighborIP,
				Established,
			)
		}

		By("verifying traffic works before netns deletion")
		Eventually(func() error {
			_, err := hostARedExecutor.Exec("curl", "-sS", "--max-time", "2", urlStr)
			return err
		}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

		By("identifying the router pod on clientPod's node")
		routerPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{clientPod.Spec.NodeName: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).To(HaveLen(1))
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName
		oldPodUID := routerPod.UID

		By("deleting the named netns and the router pod")
		Expect(openperouter.DeleteNamedNetns(nodeName)).To(Succeed())
		err = cs.CoreV1().Pods(openperouter.Namespace).Delete(context.Background(), routerPod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the controller to recreate the named netns")
		Eventually(func() (bool, error) {
			return openperouter.NamedNetnsExists(nodeName)
		}, 2*time.Minute, 2*time.Second).Should(BeTrue(), "controller must recreate named netns")

		By("waiting for all interface types to be recreated in the new netns")
		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			Eventually(func() (bool, error) {
				return openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
			}, 2*time.Minute, 2*time.Second).Should(BeTrue(), "interface type %s must be recreated", ifType)
		}

		By("waiting for a new router pod to come up and become ready")
		Eventually(func(g Gomega) {
			newRouterPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{nodeName: true})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newRouterPods).To(HaveLen(1))
			newPod := newRouterPods[0]
			g.Expect(newPod.UID).NotTo(Equal(oldPodUID), "a new router pod must be created after netns deletion")
			g.Expect(k8s.PodIsReady(newPod)).To(BeTrue(), "new router pod must be ready")
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)

		By("verifying traffic works again after rebuild")
		Eventually(func() error {
			_, err := hostARedExecutor.Exec("curl", "-sS", "--max-time", "3", urlStr)
			return err
		}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
	})
})

// allNodes returns a map of all Kubernetes node names for use with RouterPodsForNodes.
func allNodes(cs clientset.Interface) map[string]bool {
	nodes, err := k8s.GetNodes(cs)
	Expect(err).NotTo(HaveOccurred())
	result := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		result[n.Name] = true
	}
	return result
}

// killFRREntrypoint kills the tini/docker-start entrypoint process inside the given FRR container.
// A DeferCleanup is NOT registered; callers are responsible for waiting on pod restart.
func killFRREntrypoint(frrExec executor.Executor) {
	GinkgoHelper()
	psOut, err := frrExec.Exec("pgrep", "-f", "/sbin/tini -- /usr/lib/frr/docker-start")
	Expect(err).NotTo(HaveOccurred(), "failed to find FRR entrypoint PID")
	pids := strings.Split(strings.TrimSpace(psOut), "\n")
	Expect(pids).NotTo(BeEmpty(), "FRR entrypoint PID should not be empty")
	frrPID := strings.TrimSpace(pids[0])
	output, err := frrExec.Exec("kill", frrPID)
	Expect(err).NotTo(HaveOccurred(), "failed to kill FRR process %q: %v", frrPID, output)
}

type trafficTestResult struct {
	failCount        int
	totalCount       int
	failedTimestamps []time.Time
}

// measurePingLoss starts a background goroutine that sends a single ping to targetIP
// every 300ms. It registers a DeferCleanup to stop the goroutine on test completion
// or failure, preventing goroutine leaks. The returned function stops the goroutine
// and returns the number of failed pings observed.
func measurePingLoss(exec executor.Executor, targetIP string) func() int {
	var mu sync.Mutex
	var failCount int
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_, err := exec.Exec("ping", "-c", "1", "-W", "1", targetIP)
			mu.Lock()
			if err != nil {
				failCount++
			}
			mu.Unlock()
			time.Sleep(300 * time.Millisecond)
		}
	}()
	return func() int {
		cancel()
		mu.Lock()
		defer mu.Unlock()
		return failCount
	}
}

// measureTrafficLoss starts a background goroutine that sends curl requests to urlStr
// every 300ms. It registers a DeferCleanup to stop the goroutine on test completion
// or failure, preventing goroutine leaks. The returned function stops the goroutine
// and returns the number of failed requests observed.
func measureTrafficLoss(exec executor.Executor, urlStr string) func() trafficTestResult {
	var mu sync.Mutex
	var trafficTestCount trafficTestResult
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_, err := exec.Exec("curl", "-sS", "--max-time", "2", urlStr)
			mu.Lock()
			if err != nil {
				trafficTestCount.failCount++
				trafficTestCount.failedTimestamps = append(trafficTestCount.failedTimestamps, time.Now())
			}
			trafficTestCount.totalCount++
			mu.Unlock()
			time.Sleep(300 * time.Millisecond)
		}
	}()
	return func() trafficTestResult {
		cancel()
		mu.Lock()
		defer mu.Unlock()
		return trafficTestCount
	}
}

func (tr trafficTestResult) eval() error {
	const maxAllowedFailures = 5
	if tr.totalCount == 0 {
		return fmt.Errorf("no traffic was measured")
	}
	if tr.failCount > maxAllowedFailures {
		return fmt.Errorf(tr.String())
	}
	return nil
}

func (tr trafficTestResult) String() string {
	return fmt.Sprintf("failed %d/%d times", tr.failCount, tr.totalCount)
}
