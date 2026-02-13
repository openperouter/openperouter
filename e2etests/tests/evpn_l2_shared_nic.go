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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("L2 connectivity with shared NIC mode", Ordered, Label("shared-nic"), func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	var nodes []corev1.Node

	underlaySharedNic := v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "underlay",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN:     64514,
			Nics:    []string{"toswitch"},
			NicMode: v1alpha1.NicModeShared,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     64512,
					Address: "192.168.11.2",
				},
			},
			EVPN: &v1alpha1.EVPNConfig{
				VTEPInterface: "ul-pe",
			},
		},
	}

	BeforeAll(func() {
		cs = k8sclient.New()
		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())

		By("enabling redistribute connected on leafkind, leafA, and leafB")
		redistributeConnectedForLeafKind(nodes)
		redistributeConnectedForLeaf(infra.LeafAConfig)
		redistributeConnectedForLeaf(infra.LeafBConfig)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				underlaySharedNic,
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("resetting leafkind config")
		resetLeafKindConfig(nodes)

		By("removing prefixes from leafA and leafB")
		Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
		Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())

		By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	It("should provide L2 connectivity for single stack ipv4", func() {
		const testNamespace = "shared-nic-test-namespace"

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

		l2GatewayIPs := []string{"192.171.24.1/24"}
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
				L2GatewayIPs: l2GatewayIPs,
			},
		}

		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{
				vniRed,
			},
			L2VNIs: []v1alpha1.L2VNI{
				l2VniRed,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			err = k8s.DeleteNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
			Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
		})

		firstPodIPs := []string{"192.171.24.2/24"}
		secondPodIPs := []string{"192.171.24.3/24"}

		testNad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", l2GatewayIPs)
		Expect(err).NotTo(HaveOccurred())

		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		By("creating the pods")
		firstPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace, k8s.WithNad(testNad.Name, testNamespace, firstPodIPs), k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err := k8s.CreateAgnhostPod(cs, "pod2", testNamespace, k8s.WithNad(testNad.Name, testNamespace, secondPodIPs), k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(firstPod)).To(Succeed())
		Expect(removeGatewayFromPod(secondPod)).To(Succeed())

		checkPodIsReacheable := func(exec executor.Executor, from, to string) {
			GinkgoHelper()
			const port = "8090"
			hostPort := net.JoinHostPort(to, port)
			urlStr := url.Format("http://%s/clientip", hostPort)
			Eventually(func(g Gomega) string {
				By(fmt.Sprintf("trying to hit %s from %s", to, from))
				res, err := exec.Exec("curl", "--connect-timeout", "3", "-sS", urlStr)
				g.Expect(err).ToNot(HaveOccurred(), "curl %s failed: %s", hostPort, res)
				clientIP, _, err := net.SplitHostPort(res)
				g.Expect(err).ToNot(HaveOccurred())
				return clientIP
			}).
				WithTimeout(40*time.Second).
				WithPolling(time.Second).
				Should(Equal(from), "curl should return the expected clientip")
		}

		podExecutor := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
		secondPodExecutor := executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost")
		hostARedExecutor := executor.ForContainer("clab-kind-hostA_red")

		hostARedIPs := []string{infra.HostARedIPv4}
		hostBRedIPs := []string{infra.HostBRedIPv4}

		tests := []struct {
			exec    executor.Executor
			from    string
			to      string
			fromIPs []string
			toIPs   []string
		}{
			{exec: podExecutor, from: "firstPod", to: "secondPod", fromIPs: firstPodIPs, toIPs: secondPodIPs},
			{exec: secondPodExecutor, from: "secondPod", to: "firstPod", fromIPs: secondPodIPs, toIPs: firstPodIPs},
			{exec: podExecutor, from: "firstPod", to: "hostARed", fromIPs: firstPodIPs, toIPs: hostARedIPs},
			{exec: podExecutor, from: "firstPod", to: "hostBRed", fromIPs: firstPodIPs, toIPs: hostBRedIPs},
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
				checkPodIsReacheable(test.exec, from, to)
			}
		}
	})
})
