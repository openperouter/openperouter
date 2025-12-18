// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
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

var _ = Describe("Routes between bgp and the fabric", Ordered, func() {
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

	const (
		linuxBridgeHostAttachment = "linux-bridge"
		ovsBridgeHostAttachment   = "ovs-bridge"
	)
	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: ptr.To("red"),
			VNI: 110,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		},
	}

	const preExistingOVSBridge = "br-ovs-test"
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

		// Create pre-existing OVS bridges on Kind nodes for testing
		nodes := []string{infra.KindControlPlane, infra.KindWorker}
		for _, nodeName := range nodes {
			exec := executor.ForContainer(nodeName)
			// Create OVS bridge (ignore error if bridge already exists)
			_, err = exec.Exec("ovs-vsctl", "add-br", preExistingOVSBridge)
			Expect(err).NotTo(HaveOccurred())
		}
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

		// Clean up pre-existing OVS bridges
		nodes := []string{infra.KindControlPlane, infra.KindWorker}
		for _, nodeName := range nodes {
			exec := executor.ForContainer(nodeName)
			_, err = exec.Exec("ovs-vsctl", "--if-exists", "del-br", preExistingOVSBridge)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	const testNamespace = "test-namespace"
	var (
		firstPod  *corev1.Pod
		secondPod *corev1.Pod
		nad       nad.NetworkAttachmentDefinition
	)
	type testCase struct {
		firstPodIPs, secondPodIPs, hostARedIPs, hostBRedIPs, l2GatewayIPs []string
		hostMaster                                                        v1alpha1.HostMaster // Allow specifying custom HostMaster config
		nadMaster                                                         string              // Bridge name for NAD (defaults to "br-hs-110")
	}
	DescribeTable("should create two pods connected to the l2 overlay", func(tc testCase) {
		By("setting redistribute connected on leaves")
		redistributeConnectedForLeaf(infra.LeafAConfig)
		redistributeConnectedForLeaf(infra.LeafBConfig)

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		err = Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())

		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.L2GatewayIPs = tc.l2GatewayIPs
		l2VniRedWithGateway.Spec.HostMaster = &tc.hostMaster

		err = Updater.Update(config.Resources{
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

		nad, err = k8s.CreateMacvlanNad("110", testNamespace, tc.nadMaster, tc.l2GatewayIPs)
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

		By("creating the pods")
		firstPod, err = k8s.CreateAgnhostPod(cs, "pod1", testNamespace, k8s.WithNad(nad.Name, testNamespace, tc.firstPodIPs), k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err = k8s.CreateAgnhostPod(cs, "pod2", testNamespace, k8s.WithNad(nad.Name, testNamespace, tc.secondPodIPs), k8s.OnNode(nodes[1].Name))
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
				res, err := exec.Exec("curl", "-sS", urlStr)
				g.Expect(err).ToNot(HaveOccurred(), "curl %s failed: %s", hostPort, res)
				clientIP, _, err := net.SplitHostPort(res)
				g.Expect(err).ToNot(HaveOccurred())
				return clientIP
			}).
				WithTimeout(5*time.Second).
				WithPolling(time.Second).
				Should(Equal(from), "curl should return the expected clientip")
		}

		podExecutor := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
		secondPodExecutor := executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost")
		hostARedExecutor := executor.ForContainer("clab-kind-hostA_red")

		tests := []struct {
			exec    executor.Executor
			from    string
			to      string
			fromIPs []string
			toIPs   []string
		}{
			{exec: podExecutor, from: "firstPod", to: "secondPod", fromIPs: tc.firstPodIPs, toIPs: tc.secondPodIPs},
			{exec: secondPodExecutor, from: "secondPod", to: "firstPod", fromIPs: tc.secondPodIPs, toIPs: tc.firstPodIPs},
			{exec: podExecutor, from: "firstPod", to: "hostARed", fromIPs: tc.firstPodIPs, toIPs: tc.hostARedIPs},
			{exec: podExecutor, from: "firstPod", to: "hostBRed", fromIPs: tc.firstPodIPs, toIPs: tc.hostBRedIPs},
			{exec: secondPodExecutor, from: "secondPod", to: "hostARed", fromIPs: tc.secondPodIPs, toIPs: tc.hostARedIPs},
			{exec: secondPodExecutor, from: "secondPod", to: "hostBRed", fromIPs: tc.secondPodIPs, toIPs: tc.hostBRedIPs},
			{exec: hostARedExecutor, from: "hostARed", to: "firstPod", fromIPs: tc.hostARedIPs, toIPs: tc.firstPodIPs},
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
	},
		Entry("for single stack ipv4", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24"},
			firstPodIPs:  []string{"192.171.24.2/24"},
			secondPodIPs: []string{"192.171.24.3/24"},
			hostARedIPs:  []string{infra.HostARedIPv4},
			hostBRedIPs:  []string{infra.HostBRedIPv4},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		}),
		Entry("for dual stack", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24", "fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"192.171.24.2/24", "fd00:10:245:1::2/64"},
			secondPodIPs: []string{"192.171.24.3/24", "fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv4, infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv4, infra.HostBRedIPv6},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		}),
		Entry("for single stack ipv6", testCase{
			l2GatewayIPs: []string{"fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"fd00:10:245:1::2/64"},
			secondPodIPs: []string{"fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv6},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		}),
		Entry("OVS bridge autocreate for single stack ipv4", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24"},
			firstPodIPs:  []string{"192.171.24.2/24"},
			secondPodIPs: []string{"192.171.24.3/24"},
			hostARedIPs:  []string{infra.HostARedIPv4},
			hostBRedIPs:  []string{infra.HostBRedIPv4},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					AutoCreate: true,
				},
			},
		}),
		Entry("OVS bridge autocreate for dual stack", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24", "fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"192.171.24.2/24", "fd00:10:245:1::2/64"},
			secondPodIPs: []string{"192.171.24.3/24", "fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv4, infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv4, infra.HostBRedIPv6},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					AutoCreate: true,
				},
			},
		}),
		Entry("OVS bridge autocreate for single stack ipv6", testCase{
			l2GatewayIPs: []string{"fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"fd00:10:245:1::2/64"},
			secondPodIPs: []string{"fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv6},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					AutoCreate: true,
				},
			},
		}),
		Entry("OVS bridge existing for single stack ipv4", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24"},
			firstPodIPs:  []string{"192.171.24.2/24"},
			secondPodIPs: []string{"192.171.24.3/24"},
			hostARedIPs:  []string{infra.HostARedIPv4},
			hostBRedIPs:  []string{infra.HostBRedIPv4},
			nadMaster:    preExistingOVSBridge,
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name:       preExistingOVSBridge,
					AutoCreate: false,
				},
			},
		}),
		Entry("OVS bridge existing for dual stack", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24", "fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"192.171.24.2/24", "fd00:10:245:1::2/64"},
			secondPodIPs: []string{"192.171.24.3/24", "fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv4, infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv4, infra.HostBRedIPv6},
			nadMaster:    preExistingOVSBridge,
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name:       preExistingOVSBridge,
					AutoCreate: false,
				},
			},
		}),
		Entry("OVS bridge existing for single stack ipv6", testCase{
			l2GatewayIPs: []string{"fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"fd00:10:245:1::2/64"},
			secondPodIPs: []string{"fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv6},
			nadMaster:    preExistingOVSBridge,
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name:       preExistingOVSBridge,
					AutoCreate: false,
				},
			},
		}),
	)

	It("should create two pods connected to the l2 overlay with vtepInterface", func() {
		const (
			firstPodIP  = "192.171.24.2"
			secondPodIP = "192.171.24.3"
		)

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		By("setting redistribute connected on leaf kind for vtepInterface")
		// This is needed if we use vtepInterface since
		// openperouter is not going to advertise the
		// address there, that address is supposed to be
		// advertised by the network fabric
		redistributeConnectedForLeafKind(nodes)

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.VRF = nil
		l2VniRedWithGateway.Spec.L2GatewayIPs = []string{"192.171.24.1/24"}
		l2VniRedWithGateway.Spec.HostMaster = &v1alpha1.HostMaster{
			Type: linuxBridgeHostAttachment,
			LinuxBridge: &v1alpha1.LinuxBridgeConfig{
				AutoCreate: true,
			},
		}

		underlay := infra.Underlay
		underlay.Spec.EVPN = &v1alpha1.EVPNConfig{
			VTEPInterface: "toswitch",
		}

		oldRouters, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{underlay},
			L2VNIs: []v1alpha1.L2VNI{
				*l2VniRedWithGateway,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the router pods to rollout after changing the underlay")
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(oldRouters, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nad, err = k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{"192.171.24.1/24"})
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			resetLeafKindConfig(nodes)
			dumpIfFails(cs)
			err := Updater.CleanAll()
			Expect(err).NotTo(HaveOccurred())
			err = Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					infra.Underlay,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			err = k8s.DeleteNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())
		})

		By("creating the pods")
		firstPod, err = k8s.CreateAgnhostPod(cs, "pod1", testNamespace, k8s.WithNad(nad.Name, testNamespace, []string{firstPodIP + "/24"}), k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err = k8s.CreateAgnhostPod(cs, "pod2", testNamespace, k8s.WithNad(nad.Name, testNamespace, []string{secondPodIP + "/24"}), k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(firstPod)).To(Succeed())
		Expect(removeGatewayFromPod(secondPod)).To(Succeed())

		By("checking pod to pod reachability across nodes")
		podExecutor := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
		hostPort := net.JoinHostPort(secondPodIP, "8090")
		urlStr := url.Format("http://%s/clientip", hostPort)
		// Longer timeout: pod restart requires veth recreation + BGP EVPN cold-start convergence
		Eventually(func(g Gomega) string {
			res, err := podExecutor.Exec("curl", "-sS", urlStr)
			g.Expect(err).ToNot(HaveOccurred(), "curl %s failed: %s", hostPort, res)
			clientIP, _, err := net.SplitHostPort(res)
			g.Expect(err).ToNot(HaveOccurred())
			return clientIP
		}).
			WithTimeout(40*time.Second).
			WithPolling(time.Second).
			Should(Equal(firstPodIP), "curl should return the expected clientip")
	})
})

func removeGatewayFromPod(pod *corev1.Pod) error {
	exec := executor.ForPod(pod.Namespace, pod.Name, "agnhost")

	// Detect IP family from pod status IPs
	var podIPs []string
	for _, podIP := range pod.Status.PodIPs {
		podIPs = append(podIPs, podIP.IP)
	}

	family, err := ipfamily.ForAddresses(podIPs...)
	if err != nil {
		return fmt.Errorf("failed to detect IP family for pod %s: %w", pod.Name, err)
	}

	// Remove IPv4 default route if IPv4 or dual-stack
	if family == ipfamily.IPv4 || family == ipfamily.DualStack {
		output, err := exec.Exec("ip", "route", "del", "default", "dev", "eth0")
		if err != nil {
			return fmt.Errorf("failed to remove ipv4 gateway from pod %s: %s: %w", pod.Name, output, err)
		}
	}

	// Remove IPv6 default route if IPv6 or dual-stack
	// IPv6 ECMP route deletion requires the next hop IP, not just the device
	if family == ipfamily.IPv6 || family == ipfamily.DualStack {
		nextHopIPv6, err := findNextHopIPv6(exec, "default", "eth0")
		if err != nil {
			return fmt.Errorf("failed to find IPv6 next hop for pod %s: %w", pod.Name, err)
		}
		output, err := exec.Exec("ip", "-6", "route", "del", "default", "via", nextHopIPv6, "dev", "eth0")
		if err != nil {
			return fmt.Errorf("failed to remove ipv6 gateway from pod %s: %s: %w", pod.Name, output, err)
		}
	}

	return nil
}

func findNextHopIPv6(exec executor.Executor, destination, device string) (string, error) {
	output, err := exec.Exec("ip", "-6", "route", "show", destination)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(fmt.Sprintf(`via +([0-9a-fA-F:]+) dev %s`, device))
	match := re.FindStringSubmatch(output)
	if len(match) == 0 {
		return "", fmt.Errorf("cannot extract ipv6 default gateway for dev eth0 from output: %s", output)
	}
	return strings.TrimSpace(match[1]), nil
}

func discardAddressLength(address string) string {
	return strings.Split(address, "/")[0]
}
