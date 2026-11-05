// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/onsi/ginkgo/v2"
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
	"github.com/openperouter/openperouter/e2etests/pkg/url"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Routes between bgp and the fabric with Underlay in ipv4", GroutSupport, Ordered, func() {
	var (
		cs      clientset.Interface
		routers openperouter.Routers
	)

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
		linuxBridgeHostAttachment = "LinuxBridge"
		ovsBridgeHostAttachment   = "OVSBridge"
	)
	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain: l3vniRoutingDomain("red"),
			VNI:           110,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
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
				infra.Underlay(infra.Default),
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Create pre-existing OVS bridges on cluster nodes for testing
		ovsNodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		for _, node := range ovsNodes {
			exec := executor.ForNode(node.Name)
			_, err = exec.Exec("ovs-vsctl", "--may-exist", "add-br", preExistingOVSBridge)
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for all router pods to be ready after removing the underlay")
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		// Clean up pre-existing OVS bridges
		ovsNodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		for _, node := range ovsNodes {
			exec := executor.ForNode(node.Name)
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
	AfterEach(func() {
		dumpIfFails(cs, testNamespace)
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafBConfig.Reset()).To(Succeed())
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
		err = k8s.DeleteNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("should create two pods connected to the l2 overlay", func(tc testCase) {
		By("setting redistribute connected on leaves")
		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		err = Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())

		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.GatewayIPs = tc.l2GatewayIPs
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

		By("creating the pods")
		firstPod, err = k8s.CreateAgnhostPod(cs, "pod1", testNamespace, k8s.WithNad(nad.Name, testNamespace, tc.firstPodIPs), k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err = k8s.CreateAgnhostPod(cs, "pod2", testNamespace, k8s.WithNad(nad.Name, testNamespace, tc.secondPodIPs), k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(firstPod)).To(Succeed())
		Expect(removeGatewayFromPod(secondPod)).To(Succeed())

		By("waiting for BGP sessions to establish on both nodes before traffic check")
		leafExec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validate.SessionWithNeighbor(
				leafExec,
				validate.SessionParameters{
					FromName:    infra.KindLeaf,
					ToName:      node.Name,
					NeighborIP:  neighborIP,
					Established: Established,
				},
			)
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
				checkPodIsReachable(test.exec, from, to)
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
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
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
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
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
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		}),
		Entry("OVS bridge managed for single stack ipv4", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24"},
			firstPodIPs:  []string{"192.171.24.2/24"},
			secondPodIPs: []string{"192.171.24.3/24"},
			hostARedIPs:  []string{infra.HostARedIPv4},
			hostBRedIPs:  []string{infra.HostBRedIPv4},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		}),
		Entry("OVS bridge managed for dual stack", testCase{
			l2GatewayIPs: []string{"192.171.24.1/24", "fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"192.171.24.2/24", "fd00:10:245:1::2/64"},
			secondPodIPs: []string{"192.171.24.3/24", "fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv4, infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv4, infra.HostBRedIPv6},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		}),
		Entry("OVS bridge managed for single stack ipv6", testCase{
			l2GatewayIPs: []string{"fd00:10:245:1::1/64"},
			firstPodIPs:  []string{"fd00:10:245:1::2/64"},
			secondPodIPs: []string{"fd00:10:245:1::3/64"},
			hostARedIPs:  []string{infra.HostARedIPv6},
			hostBRedIPs:  []string{infra.HostBRedIPv6},
			nadMaster:    "br-hs-110",
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
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
					Name:      new(preExistingOVSBridge),
					Lifecycle: v1alpha1.BridgeLifecycleExternal,
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
					Name:      new(preExistingOVSBridge),
					Lifecycle: v1alpha1.BridgeLifecycleExternal,
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
					Name:      new(preExistingOVSBridge),
					Lifecycle: v1alpha1.BridgeLifecycleExternal,
				},
			},
		}),
	)

	// A VIP managed by keepalived moves between hosts behind different VTEPs while
	// both hosts stay up: the old owner only drops the address, and the new owner
	// announces it with gratuitous ARPs and sends no other traffic from it.
	Context("VIP mobility across VTEPs", func() {
		const (
			l2GatewayIP = "192.171.24.1/24"
			oldOwnerIP  = "192.171.24.2/24"
			newOwnerIP  = "192.171.24.3/24"
			oldNodeIP   = "192.171.24.4/24"
			newNodeIP   = "192.171.24.5/24"
			oldOwnerMAC = "02:00:00:00:24:02"
			newOwnerMAC = "02:00:00:00:24:03"
			vip         = "192.171.24.10"
			vipCIDR     = vip + "/32"
			// keepalived announces a new master with a burst of five gratuitous
			// ARPs (garp_master_repeat).
			garpBurst = 5
			// Longer than the 30s neighbor lifetime grout is configured with.
			unassignedTimeout  = time.Minute
			convergenceTimeout = 3 * time.Minute
		)

		var (
			oldOwner, newOwner         *corev1.Pod
			oldOwnerExec, newOwnerExec executor.Executor
			observers                  map[string]executor.Executor
			expectObserversReach       func(g Gomega, ownerName, ownerMAC string)
		)

		BeforeEach(func() {
			l2VniRedWithGateway := l2VniRed.DeepCopy()
			l2VniRedWithGateway.Spec.GatewayIPs = []string{l2GatewayIP}
			Expect(Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{vniRed},
				L2VNIs: []v1alpha1.L2VNI{*l2VniRedWithGateway},
			})).To(Succeed())

			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes")
			oldNode, newNode := nodes[0].Name, nodes[1].Name

			_, err = k8s.CreateNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			nadObj, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{l2GatewayIP})
			Expect(err).NotTo(HaveOccurred())

			createPod := func(name, node string, network k8s.PodModifier) *corev1.Pod {
				GinkgoHelper()
				pod, err := k8s.CreateAgnhostPod(cs, name, testNamespace,
					network,
					k8s.OnNode(node),
					k8s.WithCapabilities("NET_RAW"))
				Expect(err).NotTo(HaveOccurred())
				return pod
			}

			By(fmt.Sprintf("creating the old VIP owner and an observer on %s, the new VIP owner and an observer on %s",
				oldNode, newNode))
			oldOwner = createPod("owner-old", oldNode,
				k8s.WithNadMAC(nadObj.Name, testNamespace, []string{oldOwnerIP}, oldOwnerMAC))
			newOwner = createPod("owner-new", newNode,
				k8s.WithNadMAC(nadObj.Name, testNamespace, []string{newOwnerIP}, newOwnerMAC))
			oldNodeObserver := createPod("observer-old", oldNode,
				k8s.WithNad(nadObj.Name, testNamespace, []string{oldNodeIP}))
			newNodeObserver := createPod("observer-new", newNode,
				k8s.WithNad(nadObj.Name, testNamespace, []string{newNodeIP}))

			oldOwnerExec = executor.ForPod(testNamespace, oldOwner.Name, "agnhost")
			newOwnerExec = executor.ForPod(testNamespace, newOwner.Name, "agnhost")
			observers = map[string]executor.Executor{
				oldNodeObserver.Name: executor.ForPod(testNamespace, oldNodeObserver.Name, "agnhost"),
				newNodeObserver.Name: executor.ForPod(testNamespace, newNodeObserver.Name, "agnhost"),
			}

			By(fmt.Sprintf("assigning the VIP %s to %s", vip, oldOwner.Name))
			Expect(addVIP(oldOwnerExec, vipCIDR)).To(Succeed())
			Expect(sendGARP(oldOwnerExec, vip, garpBurst)).To(Succeed())

			By(fmt.Sprintf("checking the observers reach the VIP on %s", oldOwner.Name))
			Eventually(func(g Gomega) {
				expectObserversReach(g, oldOwner.Name, oldOwnerMAC)
			}, convergenceTimeout, time.Second).Should(Succeed())
		})

		// The pods forward IP traffic, so once the old owner learns the new
		// owner's MAC it relays traffic sent to it for the VIP. Reaching the
		// new owner then does not prove that the VTEPs moved the VIP: every ARP
		// reply for the VIP, including those the VTEPs proxy, must carry the
		// owner's MAC.
		expectObserversReach = func(g Gomega, ownerName, ownerMAC string) {
			GinkgoHelper()
			for name, observer := range observers {
				g.Expect(arpingMACs(observer, vip)).To(HaveEach(ownerMAC), "VIP ARP replies seen by %s", name)
				g.Expect(vipOwner(observer, vip)).To(Equal(ownerName), "VIP owner seen by %s", name)
			}
		}

		It("moves the VIP when the new owner claims it right after the old owner releases it", func() {
			By(fmt.Sprintf("moving the VIP from %s to %s", oldOwner.Name, newOwner.Name))
			Expect(delVIP(oldOwnerExec, vipCIDR)).To(Succeed())
			Expect(addVIP(newOwnerExec, vipCIDR)).To(Succeed())

			By(fmt.Sprintf("checking the observers reach the VIP on %s", newOwner.Name))
			Eventually(func(g Gomega) {
				g.Expect(sendGARP(newOwnerExec, vip, garpBurst)).To(Succeed())
				expectObserversReach(g, newOwner.Name, newOwnerMAC)
			}, convergenceTimeout, time.Second).Should(Succeed())
		})

		It("moves the VIP when the new owner claims it after it stayed unassigned", func() {
			By(fmt.Sprintf("releasing the VIP from %s", oldOwner.Name))
			Expect(delVIP(oldOwnerExec, vipCIDR)).To(Succeed())

			// The old owner stays up and keeps trying to reach the VIP, as a
			// bootstrap node waiting for the API does after handing it over.
			By(fmt.Sprintf("checking the VIP is unreachable from %s while it is unassigned", oldOwner.Name))
			Consistently(func() error {
				_, err := vipOwner(oldOwnerExec, vip)
				return err
			}, unassignedTimeout, time.Second).Should(HaveOccurred())

			By(fmt.Sprintf("assigning the VIP to %s", newOwner.Name))
			Expect(addVIP(newOwnerExec, vipCIDR)).To(Succeed())
			Expect(sendGARP(newOwnerExec, vip, garpBurst)).To(Succeed())

			By(fmt.Sprintf("checking the observers reach the VIP on %s", newOwner.Name))
			Eventually(func(g Gomega) {
				g.Expect(sendGARP(newOwnerExec, vip, garpBurst)).To(Succeed())
				expectObserversReach(g, newOwner.Name, newOwnerMAC)
			}, convergenceTimeout, time.Second).Should(Succeed())
		})
	})
})

var _ = Describe("Disconnected L2VNI east/west traffic", GroutSupport, Ordered, func() {
	const (
		testNamespace             = "test-disconnected-l2"
		linuxBridgeHostAttachment = "LinuxBridge"
		firstPodIP                = "192.171.30.2"
		secondPodIP               = "192.171.30.3"
	)

	var cs clientset.Interface

	l2vniDisconnected := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "disconnected",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: 300,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		},
	}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay(infra.Default),
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
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

	var (
		nodes       []corev1.Node
		l2VNIs      []v1alpha1.L2VNI
		firstL2VNI  v1alpha1.L2VNI
		secondL2VNI v1alpha1.L2VNI
		firstPod    *corev1.Pod
		secondPod   *corev1.Pod
	)

	BeforeEach(func() {
		var err error
		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2))

	})

	JustBeforeEach(func() {
		var err error
		err = Updater.Update(config.Resources{
			L2VNIs: l2VNIs,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nadObj, err := k8s.CreateMacvlanNad("300", testNamespace, "br-hs-300", []string{})
		Expect(err).NotTo(HaveOccurred())

		By("creating two pods on different nodes")
		firstPod, err = k8s.CreateAgnhostPod(cs, "pod1", testNamespace,
			k8s.WithNad(nadObj.Name, testNamespace, []string{firstPodIP + "/24"}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err = k8s.CreateAgnhostPod(cs, "pod2", testNamespace,
			k8s.WithNad(nadObj.Name, testNamespace, []string{secondPodIP + "/24"}),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(firstPod)).To(Succeed())
		Expect(removeGatewayFromPod(secondPod)).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
		err = k8s.DeleteNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("without route targets", func() {
		BeforeEach(func() {
			l2VNIs = []v1alpha1.L2VNI{l2vniDisconnected}
		})

		It("should allow pod-to-pod L2 connectivity without a VRF", func() {
			By("checking bidirectional L2 reachability")
			canPingFromPod(executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost"), secondPodIP)
			canPingFromPod(executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost"), firstPodIP)
		})
	})

	Context("with RD assigned number", func() {
		const rdAssignedNumber int32 = 42000

		BeforeEach(func() {
			skipMACIPType2OnGrout()
			By("configuring the L2VNI RD assigned number")
			configured := l2vniDisconnected.DeepCopy()
			configured.Spec.RDAssignedNumber = new(rdAssignedNumber)
			l2VNIs = []v1alpha1.L2VNI{*configured}
		})

		It("advertises both Type-2 routes under the configured RD on the leaf", func() {
			routers, err := openperouter.Get(cs, HostMode)
			Expect(err).To(Succeed())
			leaf := executor.ForContainer(infra.KindLeaf)
			ips := []string{firstPodIP, secondPodIP}
			routerIDs := make([]string, 2)
			vteps := make([]string, 2)
			rts := []v1alpha1.RouteTarget{"64514:300"}
			By("discovering each originating router ID and VTEP")
			for i := range 2 {
				origin, err := routers.ExecutorForNode(nodes[i].Name)
				Expect(err).To(Succeed())
				Eventually(func(g Gomega) {
					info, err := frr.EVPNInfo(origin)
					g.Expect(err).To(Succeed())
					g.Expect(info.BgpLocalRouterId).NotTo(BeEmpty())
					routerIDs[i] = info.BgpLocalRouterId
				}, time.Minute, time.Second).Should(Succeed())
				cidr, err := openperouter.GetVtepIPv4ForNode(infra.Underlay(infra.Default).Spec.TunnelEndpoint, &nodes[i])
				Expect(err).To(Succeed())
				vteps[i] = ipfamily.StripCIDRMask(cidr)
			}
			Expect(routerIDs[0]).NotTo(Equal(routerIDs[1]))
			By("checking both Type-2 routes on the leaf under their originating router RDs")
			Eventually(func(g Gomega) {
				info, err := frr.EVPNInfo(leaf)
				g.Expect(err).To(Succeed())
				for i := range 2 {
					rd := fmt.Sprintf("%s:%d", routerIDs[i], rdAssignedNumber)
					g.Expect(info.ContainsRouteWithRD(rd, 2, ips[i], vteps[i], rts)).To(BeTrue(),
						"pod %s should be under RD %s on receiving leaf", ips[i], rd)
				}
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("with route targets", func() {
		BeforeEach(func() {
			skipMACIPType2OnGrout()
			firstL2VNI = l2VNIForNode(
				l2vniDisconnected, "disconnected-first", nodes[0].Name, "64514:300", "64514:301",
			)
			secondL2VNI = l2VNIForNode(
				l2vniDisconnected, "disconnected-second", nodes[1].Name, "64514:301", "64514:300",
			)
			l2VNIs = []v1alpha1.L2VNI{firstL2VNI, secondL2VNI}
		})

		It("should retain configured route targets on imported type-2 routes", func() {
			By("checking imported type-2 routes retain the configured route target")
			routers, err := openperouter.Get(cs, HostMode)
			Expect(err).NotTo(HaveOccurred())
			firstRouter, err := routers.ExecutorForNode(nodes[0].Name)
			Expect(err).NotTo(HaveOccurred())
			secondRouter, err := routers.ExecutorForNode(nodes[1].Name)
			Expect(err).NotTo(HaveOccurred())

			firstVTEPCIDR, err := openperouter.GetVtepIPv4ForNode(infra.Underlay(infra.Default).Spec.TunnelEndpoint, &nodes[0])
			Expect(err).NotTo(HaveOccurred())
			secondVTEPCIDR, err := openperouter.GetVtepIPv4ForNode(infra.Underlay(infra.Default).Spec.TunnelEndpoint, &nodes[1])
			Expect(err).NotTo(HaveOccurred())
			firstVTEP := ipfamily.StripCIDRMask(firstVTEPCIDR)
			secondVTEP := ipfamily.StripCIDRMask(secondVTEPCIDR)

			Eventually(func(g Gomega) {
				firstEVPN, err := frr.EVPNInfo(firstRouter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(firstEVPN.ContainsType2MACIPRouteWithRT(
					secondPodIP,
					secondVTEP,
					firstL2VNI.Spec.ImportRTs,
				)).To(BeTrue(), "first node should import the second pod's type-2 route with its configured route target")

				secondEVPN, err := frr.EVPNInfo(secondRouter)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(secondEVPN.ContainsType2MACIPRouteWithRT(
					firstPodIP,
					firstVTEP,
					secondL2VNI.Spec.ImportRTs,
				)).To(BeTrue(), "second node should import the first pod's type-2 route with its configured route target")
			}, time.Minute, time.Second).Should(Succeed())

			By("checking bidirectional L2 reachability")
			canPingFromPod(executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost"), secondPodIP)
			canPingFromPod(executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost"), firstPodIP)
		})
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

func l2VNIForNode(
	l2vni v1alpha1.L2VNI,
	name, nodeName string,
	exportRT, importRT v1alpha1.RouteTarget,
) v1alpha1.L2VNI {
	configured := l2vni.DeepCopy()
	configured.Name = name
	configured.Spec.NodeSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"kubernetes.io/hostname": nodeName,
		},
	}
	configured.Spec.ExportRTs = []v1alpha1.RouteTarget{exportRT}
	configured.Spec.ImportRTs = []v1alpha1.RouteTarget{importRT}
	return *configured
}

// skipMACIPType2OnGrout skips specs expecting MAC+IPv4 Type-2 routes on a
// disconnected L2VNI. grout only learns ARP/NDP neighbors whose address falls
// in a connected subnet of the receiving interface (DPDK/grout@3f2f68e865e8).
// The disconnected L2VNI has no gateway IP, so only the pods' IPv6 link-local
// addresses are learned and their IPv4 addresses never reach EVPN.
//
// See https://github.com/openperouter/openperouter/issues/940
func skipMACIPType2OnGrout() {
	if GroutMode {
		Skip("grout does not learn IPv4 neighbors on L2VNIs without a gateway IP")
	}
}

func addVIP(exec executor.Executor, vipCIDR string) error {
	out, err := exec.Exec("ip", "addr", "add", vipCIDR, "dev", "net1")
	if err != nil {
		return fmt.Errorf("failed to add VIP %s: %s: %w", vipCIDR, out, err)
	}
	return nil
}

func delVIP(exec executor.Executor, vipCIDR string) error {
	out, err := exec.Exec("ip", "addr", "del", vipCIDR, "dev", "net1")
	if err != nil {
		return fmt.Errorf("failed to delete VIP %s: %s: %w", vipCIDR, out, err)
	}
	return nil
}

// sendGARP announces the VIP the way keepalived does: ARP requests with the
// VIP as both sender and target, one per second.
func sendGARP(exec executor.Executor, vip string, count int) error {
	out, err := exec.Exec("arping", "-U", "-c", strconv.Itoa(count), "-I", "net1", "-s", vip, vip)
	if err != nil {
		return fmt.Errorf("failed to send gratuitous ARP for %s: %s: %w", vip, out, err)
	}
	return nil
}

// vipOwner returns the name of the pod that answers on the VIP.
func vipOwner(exec executor.Executor, vip string) (string, error) {
	hostnameURL := url.Format("http://%s/hostname", net.JoinHostPort(vip, "8090"))
	out, err := exec.Exec("curl", "-sS", "--max-time", "3", hostnameURL)
	if err != nil {
		return "", fmt.Errorf("failed to reach VIP %s: %s: %w", vip, out, err)
	}
	return strings.TrimSpace(out), nil
}

var arpReplyMAC = regexp.MustCompile(`reply from \S+ \[([0-9A-Fa-f:]{17})\]`)

// arpingMACs returns the MAC address of every ARP reply for the VIP. The requests
// stay broadcast (-b) so that every responder, including a VTEP proxying the
// reply from its EVPN neighbor cache, answers each of them.
func arpingMACs(exec executor.Executor, vip string) ([]string, error) {
	out, err := exec.Exec("arping",
		"-b",      // broadcast
		"-c", "2", // count
		"-w", "3", // timeout in seconds
		"-I", "net1", // interface to use
		vip)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve VIP %s: %s: %w", vip, out, err)
	}
	var macs []string
	for _, match := range arpReplyMAC.FindAllStringSubmatch(out, -1) {
		macs = append(macs, strings.ToLower(match[1]))
	}
	return macs, nil
}
