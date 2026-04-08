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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var singleSessionUnderlay = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay-single",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:  64514,
		Nics: []string{"toswitch"},
		Neighbors: []v1alpha1.Neighbor{
			{
				ASN:     64512,
				Address: "192.168.11.2",
			},
		},
		EVPN: &v1alpha1.EVPNConfig{
			VTEPCIDR: "100.65.0.0/24",
		},
	},
}

var vniRedSingleSession = v1alpha1.L3VNI{
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
			},
		},
		VNI: 100,
	},
}

var _ = Describe("Single Session Baseline", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	nodes := []corev1.Node{}

	const testNamespace = "single-session-test"
	var testPod *corev1.Pod
	var podNode *corev1.Node

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		By("Setting up underlay with single interface and single neighbor")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				singleSessionUnderlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Creating the test namespace")
		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("Creating the test pod")
		testPod, err = k8s.CreateAgnhostPod(cs, "test-pod", testNamespace)
		Expect(err).NotTo(HaveOccurred())

		podNode, err = cs.CoreV1().Nodes().Get(context.Background(), testPod.Spec.NodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("Creating L3VNI and advertising pod IP via FRR-K8s")
		nodeSelector := k8s.NodeSelectorForPod(testPod)
		var frrK8sConfigForPod []config.FRRConfiguration

		for _, podIP := range testPod.Status.PodIPs {
			var cidrSuffix = "/32"
			ipFamily, err := ipfamily.ForAddresses(podIP.IP)
			Expect(err).NotTo(HaveOccurred())
			if ipFamily == ipfamily.IPv6 {
				cidrSuffix = "/128"
			}

			frrConfig, err := frrk8s.ConfigFromHostSessionForIPFamily(
				*vniRedSingleSession.Spec.HostSession,
				vniRedSingleSession.Name,
				ipFamily,
				frrk8s.WithNodeSelector(nodeSelector),
				frrk8s.AdvertisePrefixes(podIP.IP+cidrSuffix),
			)
			Expect(err).NotTo(HaveOccurred())
			frrK8sConfigForPod = append(frrK8sConfigForPod, *frrConfig)
		}

		err = Updater.Update(config.Resources{
			L3VNIs:            []v1alpha1.L3VNI{vniRedSingleSession},
			FRRConfigurations: frrK8sConfigForPod,
		})
		Expect(err).NotTo(HaveOccurred())

		frrK8sPod, err := frrk8s.PodForNode(cs, testPod.Spec.NodeName)
		Expect(err).NotTo(HaveOccurred())
		validateFRRK8sSessionForHostSession(vniRedSingleSession.Name, *vniRedSingleSession.Spec.HostSession, Established, frrK8sPod)
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

	It("establishes BGP session with TOR", func() {
		exec := executor.ForContainer(infra.KindLeaf)
		Eventually(func() error {
			for _, node := range nodes {
				neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
				Expect(err).NotTo(HaveOccurred())
				validateSessionWithNeighbor(infra.KindLeaf, node.Name, exec, neighborIP, Established)
			}
			return nil
		}, time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	It("verifies L3 connectivity from external host to pod", func() {
		podIP, err := getPodIPByFamily(testPod, ipfamily.IPv4)
		Expect(err).NotTo(HaveOccurred())

		hostExecutor := executor.ForContainer("clab-kind-hostA_red")

		Eventually(func() error {
			By("curling from hostA_red to pod")
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
})
