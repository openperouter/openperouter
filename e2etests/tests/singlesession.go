// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"

	frrk8sapi "github.com/metallb/frr-k8s/api/v1beta1"
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
	"k8s.io/utils/ptr"
)

var singleSessionUnderlay = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay-single",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:  64514,
		Nics: []string{"toswitch1"},
		Neighbors: []v1alpha1.Neighbor{
			{
				ASN:     ptr.To(int64(64512)),
				Address: new("192.168.11.2"),
			},
		},
		TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
			CIDRs: []string{"100.65.0.0/24"},
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
			HostASN: ptr.To(int64(64515)),
			LocalCIDR: v1alpha1.LocalCIDRConfig{
				IPv4: ptr.To("192.169.10.0/24"),
				IPv6: ptr.To("2001:db8:169:10::/64"),
			},
		},
		VNI: 100,
	},
}

var l2vniRedSingleSession = v1alpha1.L2VNI{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "red110",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.L2VNISpec{
		VRF:          ptr.To("red"),
		VNI:          110,
		L2GatewayIPs: []string{"192.171.24.1/24"},
		HostMaster: &v1alpha1.HostMaster{
			Type: "linux-bridge",
			LinuxBridge: &v1alpha1.LinuxBridgeConfig{
				AutoCreate: ptr.To(true),
			},
		},
	},
}

var _ = Describe("Single Session Baseline", Ordered, func() {
	var cs clientset.Interface
	nodes := []corev1.Node{}

	const testNamespace = "single-session-test"
	var testPod *corev1.Pod

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
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

		By("Enabling redistribute connected on leaf switches for L3 connectivity")
		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())

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
			L2VNIs:            []v1alpha1.L2VNI{l2vniRedSingleSession},
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

		By("Restoring leaf switch configuration")
		Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
		Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

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
	})

	It("verifies L2 and L3 connectivity", func() {
		By("Verifying BGP session with TOR")
		exec := executor.ForContainer(infra.KindLeaf)
		Eventually(func() error {
			for _, node := range nodes {
				neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
				Expect(err).NotTo(HaveOccurred())
				validateSessionWithNeighbor(
					exec,
					validationParameters{
						fromName:    infra.KindLeaf,
						toName:      node.Name,
						neighborIP:  neighborIP,
						established: Established,
					},
				)
			}
			return nil
		}, time.Minute, time.Second).ShouldNot(HaveOccurred())

		By("Verifying L3 connectivity from external host to pod via host session")
		podIP, err := getPodIPByFamily(testPod, ipfamily.IPv4)
		Expect(err).NotTo(HaveOccurred())

		hostExecutor := executor.ForContainer("clab-kind-hostA_red")
		Eventually(func() error {
			urlStr := url.Format("http://%s:8090/hostname", podIP)
			res, err := hostExecutor.Exec("curl", "-sS", urlStr)
			if err != nil {
				return err
			}
			if res != testPod.Name {
				return fmt.Errorf("expected hostname %q, got %q", testPod.Name, res)
			}
			return nil
		}, 2*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

		By("Creating NAD for L2 connectivity")
		l2GatewayIP := "192.171.24.1/24"
		firstPodIP := "192.171.24.2/24"
		secondPodIP := "192.171.24.3/24"
		nad, err := k8s.CreateMacvlanNad("l2-nad", testNamespace, "br-hs-110", []string{l2GatewayIP})
		Expect(err).NotTo(HaveOccurred())

		By("Ensuring at least 2 nodes are available")
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes for L2 pod-to-pod testing")

		By("Creating first L2 pod on first node")
		firstL2Pod, err := k8s.CreateAgnhostPod(cs, "l2-pod-1", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{firstPodIP}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("Creating second L2 pod on second node")
		secondL2Pod, err := k8s.CreateAgnhostPod(cs, "l2-pod-2", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{secondPodIP}),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("Removing default gateways from L2 pods")
		Expect(removeGatewayFromPod(firstL2Pod)).To(Succeed())
		Expect(removeGatewayFromPod(secondL2Pod)).To(Succeed())

		By("Verifying L2 connectivity: first pod to second pod")
		firstPodExecutor := executor.ForPod(firstL2Pod.Namespace, firstL2Pod.Name, "agnhost")
		secondPodIPAddr := discardAddressLength(secondPodIP)
		canPingFromPod(firstPodExecutor, secondPodIPAddr)

		By("Verifying L3 connectivity: first pod to hostARed")
		canPingFromPod(firstPodExecutor, infra.HostARedIPv4)
	})
})
