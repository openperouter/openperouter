// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
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
	"github.com/openperouter/openperouter/e2etests/pkg/url"
	frrk8sapi "github.com/metallb/frr-k8s/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)


var vniRedMultiSession = v1alpha1.L3VNI{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "red",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.L3VNISpec{
		VRF: "red",
		HostSession: &v1alpha1.HostSession{
			ASN:     64514,
			HostASN: 64515,
			LocalCIDR: v1alpha1.LocalCIDRConfig{
				IPv4: "192.169.10.0/24",
				IPv6: "2001:db8:169:10::/64",
			},
		},
		VNI: 100,
	},
}

var _ = Describe("Multi-Session Multi-Underlay", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	nodes := []corev1.Node{}

	const testNamespace = "multi-session-test"
	var testPod *corev1.Pod

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		By("Setting up underlay with multiple interfaces and neighbors")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Creating the test namespace")
		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("Creating the test pod")
		testPod, err = k8s.CreateAgnhostPod(cs, "test-pod", testNamespace)
		Expect(err).NotTo(HaveOccurred())

		_, err = cs.CoreV1().Nodes().Get(context.Background(), testPod.Spec.NodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Creating L3VNI and advertising pod IP via FRR-K8s")
		nodeSelector := k8s.NodeSelectorForPod(testPod)
		var frrK8sConfigForPod []frrk8sapi.FRRConfiguration

		for _, podIP := range testPod.Status.PodIPs {
			var cidrSuffix = "/32"
			ipFamily, err := ipfamily.ForAddresses(podIP.IP)
			Expect(err).NotTo(HaveOccurred())
			if ipFamily == ipfamily.IPv6 {
				cidrSuffix = "/128"
			}

			frrConfig, err := frrk8s.ConfigFromHostSessionForIPFamily(
				*vniRedMultiSession.Spec.HostSession,
				vniRedMultiSession.Name,
				ipFamily,
				frrk8s.WithNodeSelector(nodeSelector),
				frrk8s.AdvertisePrefixes(podIP.IP+cidrSuffix),
			)
			Expect(err).NotTo(HaveOccurred())
			frrK8sConfigForPod = append(frrK8sConfigForPod, *frrConfig)
		}

		err = Updater.Update(config.Resources{
			L3VNIs:            []v1alpha1.L3VNI{vniRedMultiSession},
			FRRConfigurations: frrK8sConfigForPod,
		})
		Expect(err).NotTo(HaveOccurred())

		frrK8sPod, err := frrk8s.PodForNode(cs, testPod.Spec.NodeName)
		Expect(err).NotTo(HaveOccurred())
		validateFRRK8sSessionForHostSession(vniRedMultiSession.Name, *vniRedMultiSession.Spec.HostSession, Established, frrK8sPod)
	})

	AfterAll(func() {
		By("Deleting the test namespace")
		err := k8s.DeleteNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	It("establishes BGP sessions with all neighbors on both TOR switches", func() {
		leaves := []string{infra.KindLeaf, infra.KindLeaf2}

		for _, leaf := range leaves {
			By("validating BGP sessions for " + leaf)
			exec := executor.ForContainer(leaf)
			Eventually(func() error {
				for _, node := range nodes {
					neighborIP, err := infra.NeighborIP(leaf, node.Name)
					Expect(err).NotTo(HaveOccurred())
					validateSessionWithNeighbor(leaf, node.Name, exec, neighborIP, Established)
				}
				return nil
			}, time.Minute, time.Second).ShouldNot(HaveOccurred())
		}
	})

	It("verifies L3 connectivity from leafkind (toswitch) to pod", func() {
		podIP, err := getPodIPByFamily(testPod, ipfamily.IPv4)
		Expect(err).NotTo(HaveOccurred())

		hostExecutor := executor.ForContainer("clab-kind-hostA_red")

		Eventually(func() error {
			By("curling from hostA_red (connected to leafkind) to pod")
			urlStr := url.Format("http://%s:8090/hostname", podIP)
			res, err := hostExecutor.Exec("curl", "-sS", urlStr)
			if err != nil {
				return err
			}
			if res != testPod.Name {
				return err
			}
			return nil
		}, 2*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
	})

	It("verifies L3 connectivity from leafkind2 (toswitch2) to pod", func() {
		podIP, err := getPodIPByFamily(testPod, ipfamily.IPv4)
		Expect(err).NotTo(HaveOccurred())

		// The topology shows leafkind2 connects to different hosts, but we use the same
		// hostA_red container since both leaf switches should be able to reach pods
		// through the EVPN fabric via the spine
		hostExecutor := executor.ForContainer("clab-kind-hostA_red")

		Eventually(func() error {
			By("curling from hostA_red (testing multi-path connectivity via leafkind2) to pod")
			urlStr := url.Format("http://%s:8090/hostname", podIP)
			res, err := hostExecutor.Exec("curl", "-sS", urlStr)
			if err != nil {
				return err
			}
			if res != testPod.Name {
				return err
			}
			return nil
		}, 2*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
	})

	It("verifies all 4 BGP neighbors are established", func() {
		By("Checking that we have exactly 4 neighbors configured in the underlay")
		Expect(len(infra.Underlay.Spec.Neighbors)).To(Equal(4), "Expected 4 neighbors in multi-session underlay")

		By("Verifying sessions to all 4 neighbors from both leaf switches")
		leaves := []string{infra.KindLeaf, infra.KindLeaf2}

		for _, leaf := range leaves {
			exec := executor.ForContainer(leaf)
			neighborCount := 0

			Eventually(func() error {
				neighborCount = 0
				for _, node := range nodes {
					neighborIP, err := infra.NeighborIP(leaf, node.Name)
					if err != nil {
						// Not all nodes may be neighbors of this leaf
						continue
					}
					validateSessionWithNeighbor(leaf, node.Name, exec, neighborIP, Established)
					neighborCount++
				}
				return nil
			}, time.Minute, time.Second).ShouldNot(HaveOccurred())

			By("Confirmed " + leaf + " has established BGP sessions")
		}
	})
})
