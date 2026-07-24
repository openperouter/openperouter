// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// underlayRR runs the control-plane router as a pure BGP route reflector: no
// tunnel endpoint, one listen-range dynamic neighbor that reflects both ipv4
// unicast (VTEP /32 reachability) and l2vpn evpn (type-2/3) to the clients.
var underlayRR = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rr",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:          64514,
		Interfaces:   infra.DefaultInterfaces,
		NodeSelector: &k8s.ControlPlaneNodesLabelSelector,
		RouteReflector: &v1alpha1.RouteReflectorConfig{
			ClusterID: new("192.0.2.1"),
		},
		Neighbors: []v1alpha1.Neighbor{
			{
				ListenRange: new("192.168.11.0/24"),
				Type:        new("internal"),
				AddressFamilies: []v1alpha1.NeighborAddressFamily{
					{
						Type: "ipv4unicast",
						Properties: []v1alpha1.AddressFamilyProperty{
							{Type: v1alpha1.AddressFamilyPropertyRouteReflectorClient},
						},
					},
					{
						Type: "evpn",
						Properties: []v1alpha1.AddressFamilyProperty{
							{Type: v1alpha1.AddressFamilyPropertyRouteReflectorClient},
						},
					},
				},
			},
		},
	},
}

// underlayRRAddress is the control-plane router pod address on the
// leafkind1 switch subnet, where underlayRR accepts dynamic neighbors.
const underlayRRAddress = "192.168.11.3"

// underlayRRClient runs on the worker nodes as iBGP clients of underlayRR.
var underlayRRClient = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "client",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:          64514,
		Interfaces:   infra.DefaultInterfaces,
		NodeSelector: &k8s.NonControlPlaneNodesLabelSelector,
		TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
			CIDRs: []string{"100.65.0.0/24"},
		},
		Neighbors: []v1alpha1.Neighbor{
			{
				Address: new(underlayRRAddress),
				Type:    new("internal"),
			},
		},
	},
}

var _ = Describe("Route Reflector EVPN east/west traffic", Ordered, func() {
	const (
		testNamespace             = "test-rr-l2"
		linuxBridgeHostAttachment = "linux-bridge"
		firstPodIP                = "192.171.31.2"
		secondPodIP               = "192.171.31.3"
		vni                       = 300
	)

	var (
		cs      clientset.Interface
		workers []corev1.Node
	)

	l2vniReflected := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "reflected",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: vni,
			// The route reflector node has no tunnel endpoint, so the L2VNI must
			// only land on the worker (client) nodes.
			NodeSelector: &k8s.NonControlPlaneNodesLabelSelector,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
		},
	}

	BeforeAll(func() {
		cs = k8sclient.New()

		var err error
		workers, err = k8s.WorkerNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(workers)).To(BeNumerically(">=", 2), "expected at least 2 worker nodes")

		Expect(Updater.CleanAll()).To(Succeed())

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				underlayRR,
				underlayRRClient,
			},
		})).To(Succeed())
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
		dumpIfFails(cs)
		Expect(Updater.CleanButUnderlay()).To(Succeed())
		Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())
	})

	It("reflects type-2 routes between client nodes via the route reflector", func() {
		Expect(Updater.Update(config.Resources{
			L2VNIs: []v1alpha1.L2VNI{
				l2vniReflected,
			},
		})).To(Succeed())

		_, err := k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nadObj, err := k8s.CreateMacvlanNad("300", testNamespace, "br-hs-300", []string{})
		Expect(err).NotTo(HaveOccurred())

		By("creating two pods on the two worker (client) nodes")
		firstPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace,
			k8s.WithNad(nadObj.Name, testNamespace, []string{firstPodIP + "/24"}),
			k8s.OnNode(workers[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err := k8s.CreateAgnhostPod(cs, "pod2", testNamespace,
			k8s.WithNad(nadObj.Name, testNamespace, []string{secondPodIP + "/24"}),
			k8s.OnNode(workers[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(firstPod)).To(Succeed())
		Expect(removeGatewayFromPod(secondPod)).To(Succeed())

		By("checking bidirectional L2 reachability through the route reflector")
		canPingFromPod(executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost"), secondPodIP)
		canPingFromPod(executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost"), firstPodIP)

		By("verifying the worker learned pod1's type-2 route via the route reflector")
		routers, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		firstWorkerFRR, err := routers.ExecutorForNode(workers[0].Name)
		Expect(err).NotTo(HaveOccurred())
		firstWorkerVTEPCIDR, err := openperouter.GetVtepIPv4ForNode(underlayRRClient.Spec.TunnelEndpoint, &workers[0])
		Expect(err).NotTo(HaveOccurred())
		firstWorkerVTEP := ipfamily.StripCIDRMask(firstWorkerVTEPCIDR)

		secondWorkerFRR, err := routers.ExecutorForNode(workers[1].Name)
		Expect(err).NotTo(HaveOccurred())
		secondWorkerVTEPCIDR, err := openperouter.GetVtepIPv4ForNode(underlayRRClient.Spec.TunnelEndpoint, &workers[1])
		Expect(err).NotTo(HaveOccurred())
		secondWorkerVTEP := ipfamily.StripCIDRMask(secondWorkerVTEPCIDR)

		By("Checking that traffic is not forwarded over RR nodes")
		Eventually(func(g Gomega) {
			firstWorkerEVPN, err := frr.EVPNInfo(firstWorkerFRR)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(firstWorkerEVPN.ContainsType2MACIPRouteForVNI(secondPodIP, secondWorkerVTEP, vni)).To(BeTrue(),
				"first worker should have a type-2 route for %s with next hop %s reflected via %s",
				secondPodIP, secondWorkerVTEP, underlayRRAddress)

			secondWorkerEVPN, err := frr.EVPNInfo(secondWorkerFRR)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(secondWorkerEVPN.ContainsType2MACIPRouteForVNI(firstPodIP, firstWorkerVTEP, vni)).To(BeTrue(),
				"second worker should have a type-2 route for %s with next hop %s reflected via %s",
				firstPodIP, firstWorkerVTEP, underlayRRAddress)

		}, time.Minute, time.Second).Should(Succeed())
	})
})
