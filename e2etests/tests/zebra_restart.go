// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net"
	"time"

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
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Data plane continuity during zebra restart", PodModeOnly, Ordered, func() {
	var cs clientset.Interface
	var nodes []corev1.Node
	const testNamespace = "test-zebra-dataplane"

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
		Spec:       v1alpha1.L3VNISpec{VRF: "red", VNI: 100},
	}

	l2VniRedWithGateway := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "red110", Namespace: openperouter.Namespace},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain: l3vniRoutingDomain("red"),
			VNI:           110,
			GatewayIPs:    []string{"192.171.24.1/24"},
			HostMaster: &v1alpha1.HostMaster{
				Type:        "LinuxBridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{Lifecycle: v1alpha1.BridgeLifecycleManaged},
			},
		},
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		cs = k8sclient.New()
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())

		underlayWithGR := infra.Underlay.DeepCopy()
		underlayWithGR.Spec.GracefulRestart = &v1alpha1.GracefulRestartConfig{
			RestartTimeSeconds:   new(int64(120)),
			StalePathTimeSeconds: new(int64(360)),
		}

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{*underlayWithGR},
		})).To(Succeed())

		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())

		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())

		var err error
		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "the zebra restart dataplane test requires at least 2 nodes")

		Expect(infra.LeafKind1Config.UpdateConfig(
			nodes,
			infra.LeafKindConfiguration{
				ASN:              infra.LeafKind1Config.ASN,
				SpinePeerAddress: infra.LeafKind1Config.SpinePeerAddress,
				PERouterASN:      64514,
				NextHopSelf:      true,
			},
		)).To(Succeed())

		Expect(Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{l2VniRedWithGateway},
		})).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs, testNamespace)
	})

	AfterAll(func() {
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		if err := k8s.DeleteNamespace(cs, testNamespace); err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(Updater.CleanAll()).To(Succeed())

		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("should maintain data plane while zebra restarts", func() {
		const (
			serverIP = "192.171.24.2"
			clientIP = "192.171.24.3"
			subnet   = "/24"
			port     = "8090"
		)

		_, err := k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", nil)
		Expect(err).NotTo(HaveOccurred())

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

		By("waiting for BGP sessions to establish before killing zebra")
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validate.SessionWithNeighbor(
				executor.ForContainer(infra.KindLeaf),
				validate.SessionParameters{
					FromName:    infra.KindLeaf,
					ToName:      node.Name,
					NeighborIP:  neighborIP,
					Established: Established,
				},
			)
		}

		By("verifying stretched L2 traffic works before killing zebra")
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
		frrExec := executor.ForPod(openperouter.Namespace, routerPod.Name, "frr")

		var zebraPid string
		Eventually(func(g Gomega) {
			zebraPids, err := pidsForProcesses(frrExec, []string{"zebra"})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(zebraPids["zebra"]).To(HaveLen(1), "expected exactly one live zebra process")
			zebraPid = zebraPids["zebra"][0]
		}, 10*time.Second, time.Second).Should(Succeed())

		By("starting continuous traffic measurement")
		stopAndCount := measureTrafficLoss(clientExec, urlStr)
		time.Sleep(time.Second)

		By("killing zebra with SIGKILL in the router pod")
		Expect(frrExec.Exec("kill", "-9", zebraPid)).Error().ToNot(HaveOccurred())

		By("waiting for zebra to restart")
		Eventually(func(g Gomega) {
			pids, err := pidsForProcesses(frrExec, []string{"zebra"})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pids["zebra"]).To(HaveLen(1), "zebra did not restart")
			g.Expect(pids["zebra"][0]).NotTo(Equal(zebraPid), "zebra was not restarted")
		}, 4*time.Minute, 2*time.Second).Should(Succeed())

		By("waiting for the BGP session to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validate.SessionWithNeighbor(
			executor.ForContainer(infra.KindLeaf),
			validate.SessionParameters{
				FromName:    infra.KindLeaf,
				ToName:      nodeName,
				NeighborIP:  neighborIP,
				Established: Established,
			},
		)

		By("asserting data plane traffic was not impacted by the zebra restart")
		result := stopAndCount()
		By(fmt.Sprintf("==> %s", result.String()))
		Expect(result.totalCount).To(BeNumerically(">", 0), "no traffic was measured during the zebra restart")
		Expect(result.eval()).To(
			Succeed(),
			"curl failures exceeded threshold during router pod deletion and recovery (%d/%d failed). Failed timestamps: %+v",
			result.failCount,
			result.totalCount,
			result.failedTimestamps)
	})
})
