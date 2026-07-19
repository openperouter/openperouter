// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"
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
)

var _ = Describe("Systemd Router Restart Resiliency", Label("systemdmode"), Ordered, func() {
	var cs clientset.Interface
	nodes := []corev1.Node{}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		_, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the underlay to be removed from all nodes")
		for _, node := range nodes {
			Eventually(func(g Gomega) {
				isConfigured, err := openperouter.UnderlayConfigured(node.Name)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isConfigured).To(BeFalse())
			}, 2*time.Minute, time.Second).Should(Succeed())
		}
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	validateTORSessions := func() {
		leaves := []string{infra.KindLeaf, infra.KindLeaf2}
		for _, leaf := range leaves {
			exec := executor.ForContainer(leaf)
			Eventually(func() error {
				for _, node := range nodes {
					neighborIP, err := infra.NeighborIP(leaf, node.Name)
					Expect(err).NotTo(HaveOccurred())
					validateSessionWithNeighbor(exec, validationParameters{
						fromName:    leaf,
						toName:      node.Name,
						neighborIP:  neighborIP,
						established: Established,
					})
				}
				return nil
			}, time.Minute, time.Second).ShouldNot(HaveOccurred())
		}
	}

	restartRouterOnNode := func(node corev1.Node) {
		By("restarting FRR container via systemd on node " + node.Name)
		nodeExec := executor.ForContainer(node.Name)

		pidBefore, err := nodeExec.Exec("systemctl", "show", "--property=MainPID", "--value", "routerpod-pod.service")
		Expect(err).NotTo(HaveOccurred())
		pidBefore = strings.TrimSpace(pidBefore)

		_, err = nodeExec.Exec("systemctl", "restart", "routerpod-pod.service")
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			output, err := nodeExec.Exec("systemctl", "is-active", "routerpod-pod.service")
			if err != nil {
				return err
			}
			if strings.TrimSpace(output) != "active" {
				return fmt.Errorf("service is not active: %s", output)
			}
			pidAfter, err := nodeExec.Exec("systemctl", "show", "--property=MainPID", "--value", "routerpod-pod.service")
			if err != nil {
				return err
			}
			if strings.TrimSpace(pidAfter) == pidBefore {
				return fmt.Errorf("PID has not changed, still %s", pidBefore)
			}
			return nil
		}, 30*time.Second, time.Second).Should(Succeed())
	}

	It("recovers BGP sessions after FRR restart", func() {
		By("validating initial sessions with TOR switches")
		validateTORSessions()

		By("restarting FRR on all nodes")
		for _, node := range nodes {
			restartRouterOnNode(node)
		}

		By("validating sessions re-established after restart")
		validateTORSessions()
	})

})

var _ = Describe("Systemd: Named netns and kernel objects survive FRR container kill", Label("systemdmode"), Ordered, func() {
	var cs clientset.Interface
	var nodes []corev1.Node

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
		Spec:       v1alpha1.L3VNISpec{VRF: "red", VNI: 100},
	}

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "red110", Namespace: openperouter.Namespace},
		Spec: v1alpha1.L2VNISpec{
			VRF: new("red"),
			VNI: 110,
			HostMaster: &v1alpha1.HostMaster{
				Type:        "LinuxBridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{AutoCreate: new(true)},
			},
		},
	}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		_, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())

		err = Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{l2VniRed},
		})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for interfaces in named netns on all nodes")
		for _, node := range nodes {
			for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
				Eventually(func(g Gomega) {
					g.Expect(openperouter.NamedNetnsHasInterfaceType(node.Name, ifType)).To(BeTrue())
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
			}
		}
	})

	AfterAll(func() {
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(Updater.CleanAll()).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	It("should preserve named netns and kernel objects when FRR container is killed", func() {
		Expect(nodes).NotTo(BeEmpty())
		nodeName := nodes[0].Name
		nodeExec := executor.ForContainer(nodeName)

		By("verifying named netns exists before crash")
		Expect(openperouter.NamedNetnsExists(nodeName)).To(BeTrue())

		By("verifying kernel objects exist before crash")
		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			Expect(openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)).To(BeTrue(), "interface type %s must exist before crash", ifType)
		}

		By("killing FRR container via podman")
		_, err := nodeExec.Exec("podman", "kill", "frr")
		Expect(err).NotTo(HaveOccurred())

		By("immediately asserting named netns and kernel objects survived")
		Expect(openperouter.NamedNetnsExists(nodeName)).To(BeTrue(), "named netns must survive FRR container kill")

		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			Expect(openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)).To(BeTrue(), "interface type %s must survive FRR container kill", ifType)
		}

		By("waiting for routerpod service to restart")
		Eventually(func() error {
			output, err := nodeExec.Exec("systemctl", "is-active", "routerpod-pod.service")
			if err != nil {
				return err
			}
			if strings.TrimSpace(output) != "active" {
				return fmt.Errorf("service is not active: %s", output)
			}
			return nil
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			executor.ForContainer(infra.KindLeaf),
			validationParameters{
				fromName:    infra.KindLeaf,
				toName:      nodeName,
				neighborIP:  neighborIP,
				established: Established,
			},
		)
	})
})

var _ = Describe("Systemd: Controller auto-recovers when operator deletes named netns", Label("systemdmode"), Ordered, func() {
	var cs clientset.Interface
	var nodes []corev1.Node

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		_, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
			L3VNIs: []v1alpha1.L3VNI{{
				ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
				Spec:       v1alpha1.L3VNISpec{VRF: "red", VNI: 100},
			}},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())

		By("waiting for interfaces in named netns")
		for _, node := range nodes {
			for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
				Eventually(func(g Gomega) {
					g.Expect(openperouter.NamedNetnsHasInterfaceType(node.Name, ifType)).To(BeTrue())
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
			}
		}
	})

	AfterAll(func() {
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(Updater.CleanAll()).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	It("should auto-recover after ip netns delete without manual restart", func() {
		Expect(nodes).NotTo(BeEmpty())
		nodeName := nodes[0].Name

		By("verifying named netns exists")
		Expect(openperouter.NamedNetnsExists(nodeName)).To(BeTrue())

		By("deleting named netns — do NOT manually restart routerpod")
		Expect(openperouter.DeleteNamedNetns(nodeName)).To(Succeed())

		By("waiting for controller to detect missing netns and auto-restart routerpod")
		Eventually(func() (bool, error) {
			return openperouter.NamedNetnsExists(nodeName)
		}, 3*time.Minute, 2*time.Second).Should(BeTrue(), "controller must auto-recreate named netns")

		By("waiting for all interface types to be recreated")
		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			Eventually(func() (bool, error) {
				return openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
			}, 2*time.Minute, 2*time.Second).Should(BeTrue(), "interface type %s must be recreated", ifType)
		}

		By("waiting for routerpod service to become active")
		nodeExec := executor.ForContainer(nodeName)
		Eventually(func() error {
			output, err := nodeExec.Exec("systemctl", "is-active", "routerpod-pod.service")
			if err != nil {
				return err
			}
			if strings.TrimSpace(output) != "active" {
				return fmt.Errorf("service not active: %s", output)
			}
			return nil
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			executor.ForContainer(infra.KindLeaf),
			validationParameters{
				fromName:    infra.KindLeaf,
				toName:      nodeName,
				neighborIP:  neighborIP,
				established: Established,
			},
		)
	})
})

var _ = Describe("Systemd: Data plane continuity during FRR restart", Label("systemdmode"), Ordered, func() {
	var cs clientset.Interface
	var nodes []corev1.Node
	const testNamespace = "test-systemd-dataplane"

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		_, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items
		Expect(len(nodes)).To(BeNumerically(">=", 2), "stretched L2 test requires at least 2 nodes")

		l2VniRedWithGateway := v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{Name: "red110", Namespace: openperouter.Namespace},
			Spec: v1alpha1.L2VNISpec{
				VRF:          new("red"),
				VNI:          110,
				L2GatewayIPs: []string{"192.171.24.1/24"},
				HostMaster:   &v1alpha1.HostMaster{Type: "LinuxBridge", LinuxBridge: &v1alpha1.LinuxBridgeConfig{AutoCreate: new(true)}},
			},
		}

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
			L3VNIs: []v1alpha1.L3VNI{{
				ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
				Spec:       v1alpha1.L3VNISpec{VRF: "red", VNI: 100},
			}},
			L2VNIs: []v1alpha1.L2VNI{l2VniRedWithGateway},
		})
		Expect(err).NotTo(HaveOccurred())

	})

	AfterAll(func() {
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		_ = k8s.DeleteNamespace(cs, testNamespace)
		Expect(Updater.CleanAll()).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs, testNamespace)
	})

	It("should maintain data plane during routerpod restart", func() {
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

		By("creating server pod on node 0")
		_, err = k8s.CreateAgnhostPod(cs, "server", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{serverIP + subnet}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("creating client pod on node 1")
		clientPod, err := k8s.CreateAgnhostPod(cs, "client", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{clientIP + subnet}),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		clientExec := executor.ForPod(clientPod.Namespace, clientPod.Name, "agnhost")
		hostPort := net.JoinHostPort(serverIP, port)
		urlStr := url.Format("http://%s/clientip", hostPort)

		By("waiting for BGP sessions to establish")
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				executor.ForContainer(infra.KindLeaf),
				validationParameters{
					fromName:    infra.KindLeaf,
					toName:      node.Name,
					neighborIP:  neighborIP,
					established: Established,
				},
			)
		}

		By("verifying stretched L2 traffic works before restart")
		Eventually(func() error {
			_, err := clientExec.Exec("curl", "-sS", "--max-time", "2", urlStr)
			return err
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())

		By("starting continuous traffic measurement")
		stopAndCount := measureTrafficLoss(clientExec, urlStr)

		By("restarting routerpod on server's node")
		serverNodeExec := executor.ForContainer(nodes[0].Name)
		_, err = serverNodeExec.Exec("systemctl", "restart", "routerpod-pod.service")
		Expect(err).NotTo(HaveOccurred())

		By("waiting for service to restart and BGP to re-establish")
		Eventually(func() error {
			output, err := serverNodeExec.Exec("systemctl", "is-active", "routerpod-pod.service")
			if err != nil {
				return err
			}
			if strings.TrimSpace(output) != "active" {
				return fmt.Errorf("service not active: %s", output)
			}
			return nil
		}, 2*time.Minute, time.Second).Should(Succeed())

		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodes[0].Name)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			executor.ForContainer(infra.KindLeaf),
			validationParameters{
				fromName:    infra.KindLeaf,
				toName:      nodes[0].Name,
				neighborIP:  neighborIP,
				established: Established,
			},
		)

		By("asserting data plane disruption is within acceptable bounds")
		result := stopAndCount()
		By(fmt.Sprintf("==> %s", result.String()))
		Expect(result.eval()).To(
			Succeed(),
			"curl failures exceeded threshold during routerpod restart (%d/%d failed)",
			result.failCount,
			result.totalCount,
		)
	})
})
