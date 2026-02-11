// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("North/south traffic after FRR container restart", Ordered, func() {
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
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routers, err = openperouter.Get(cs, HostMode)
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
	})

	const testNamespace = "test-namespace"

	It("should recover north/south traffic after FRR container restart", func() {
		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.L2GatewayIPs = []string{"192.171.24.1/24"}

		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{
				vniRed,
			},
			L2VNIs: []v1alpha1.L2VNI{
				*l2VniRedWithGateway,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{"192.171.24.1/24"})
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
			Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			err = k8s.DeleteNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		By("creating the pods")
		clientPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{"192.171.24.2/24"}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(clientPod)).To(Succeed())

		hostARedExecutor := executor.ForContainer("clab-kind-hostA_red")

		By("verifying north south traffic works before FRR restart")
		firstPodIP := "192.171.24.2"
		const port = "8090"
		hostPort := net.JoinHostPort(firstPodIP, port)
		urlStr := url.Format("http://%s/clientip", hostPort)
		Eventually(func(g Gomega) string {
			By(fmt.Sprintf("trying to hit %s from hostA_red", firstPodIP))
			res, err := hostARedExecutor.Exec("curl", "-sS", urlStr)
			g.Expect(err).ToNot(HaveOccurred(), "curl %s failed: %s", hostPort, res)
			clientIP, _, err := net.SplitHostPort(res)
			g.Expect(err).ToNot(HaveOccurred())
			return clientIP
		}).
			WithTimeout(5*time.Second).
			WithPolling(time.Second).
			Should(Equal(infra.HostARedIPv4), "hostA_red should be able to reach clientPod")

		By("identifying the router pod on clientPod's node")
		routerPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{clientPod.Spec.NodeName: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).To(HaveLen(1))
		routerPod := routerPods[0]
		frrExec := executor.ForPod(openperouter.Namespace, routerPod.Name, "frr")

		By("killing the FRR container entrypoint process")
		psOut, err := frrExec.Exec("pgrep", "-f", "/sbin/tini -- /usr/lib/frr/docker-start")

		Expect(err).NotTo(HaveOccurred(), "failed to find FRR entrypoint PID")
		pids := strings.Split(strings.TrimSpace(psOut), "\n")
		Expect(pids).NotTo(BeEmpty(), "FRR entrypoint PID should not be empty")
		frrPID := strings.TrimSpace(pids[0])
		var output string
		output, err = frrExec.Exec("kill", frrPID)
		Expect(err).NotTo(HaveOccurred(), "failed to kill FRR process %q: %v", frrPID, output)

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
		nodeName := routerPod.Spec.NodeName
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)

		By("verifying north/south traffic still works after FRR restart")
		Eventually(func() error {
			By(fmt.Sprintf("trying to hit %s from hostA_red after restart", firstPodIP))
			_, err := hostARedExecutor.Exec("curl", "-sS", "--max-time", "3", urlStr)
			return err
		}).
			WithTimeout(30*time.Second).
			WithPolling(time.Second).
			Should(Succeed(), "hostA_red should still be able to reach clientPod after FRR restart")
	})
})
