// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/internal/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("SRV6 routes between bgp and the fabric", Ordered, func() {
	var (
		cs           clientset.Interface
		routers      openperouter.Routers
		firstPod     *corev1.Pod
		secondPod    *corev1.Pod
		netAttachDef nad.NetworkAttachmentDefinition
		nodes        []corev1.Node
	)

	const (
		linuxBridgeHostAttachment = "linux-bridge"
		ovsBridgeHostAttachment   = "ovs-bridge"
		rdAssignedNumber          = 100
		vniID                     = 110
		preExistingOVSBridge      = "br-ovs-test"
		testNamespace             = "test-namespace"
	)

	type testCase struct {
		firstPodIPs, secondPodIPs, hostSRV6RedIPs, l2GatewayIPs []string
		hostMaster                                              v1alpha1.HostMaster // Allow specifying custom HostMaster config
		nadMaster                                               string              // Bridge name for NAD (defaults to "br-hs-110")
	}

	// l3vpnRed uses explicitly defined export route targets and throws in some
	// dummy RTs for safe measure.
	vniRed := v1alpha1.L3VPN{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VPNSpec{
			VRF: "red",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: new(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: new("192.169.10.0/24"),
					IPv6: new("2001:db8:1::/64"),
				},
			},
			RDAssignedNumber: rdAssignedNumber,
			ExportRTs: []v1alpha1.RouteTarget{
				v1alpha1.RouteTarget(fmt.Sprintf("64514:%d", rdAssignedNumber)),
				v1alpha1.RouteTarget(fmt.Sprintf("11111:%d", rdAssignedNumber)),
			},
			ImportRTs: []v1alpha1.RouteTarget{
				v1alpha1.RouteTarget(fmt.Sprintf("64520:%d", rdAssignedNumber)),
				v1alpha1.RouteTarget(fmt.Sprintf("22222:%d", rdAssignedNumber)),
			},
		},
	}

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("red%d", vniID),
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: new("red"),
			VNI: vniID,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
		},
	}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()

		By("creating the underlay")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.UnderlayEVPNandSRv6,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routers.Dump(ginkgo.GinkgoWriter)

		By("getting the nodes")
		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		for _, node := range nodes {
			By(fmt.Sprintf("adding bridge %s on node %s", preExistingOVSBridge, node.Name))
			exec := executor.ForContainer(node.Name)
			_, err = exec.Exec("ovs-vsctl", "add-br", preExistingOVSBridge)
			Expect(err).NotTo(HaveOccurred())
		}

		// In theory, this is not needed for these tests. However, do this here for consistency, as we want to avoid
		// pollution from other tests.
		By("resetting the leaf kind nodes")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
	})

	AfterAll(func() {
		for _, node := range nodes {
			By(fmt.Sprintf("deleting bridge %s from node %s", preExistingOVSBridge, node.Name))
			exec := executor.ForContainer(node.Name)
			_, err := exec.Exec("ovs-vsctl", "--if-exists", "del-br", preExistingOVSBridge)
			Expect(err).NotTo(HaveOccurred())
		}

		By("cleaning all resources")
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

	BeforeEach(func() {
		By("setting redistribute connected on leaves")
		Expect(infra.LeafSRV6Config.RedistributeConnected()).To(Succeed())

		By("cleaning all but the underlay")
		Expect(Updater.CleanButUnderlay()).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
		Expect(Updater.CleanButUnderlay()).To(Succeed())
		Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())
	})

	// This test tests that L2VNI and L3VPN work at the same time.
	// For East/West traffic:
	// Pods can ping each other across net1 which is a macvlan interface that is connected to br-hs-110 inside the host
	// network namespace. From there, the packet travels to host-110 (still inside the host netns) which is a veth that
	// is connected to pe-110 inside the OpenPERouter network namespace. Traffic is bridged via br-pe-110 to the VXLAN
	// interface vni110 from where it is sent to the other kubernetes nodes.
	// For North/South traffic, we have 2 scenarios:
	// Scenario a):
	// This test adds an L3VPN with a hostsession with FRR for L3 connectivity with the hosts behind the leaf nodes
	// inside VRF red.
	// Traffic will leave via the default interface of the pod (eth0), will hit the global host namespace, where FRR-K8S
	// will have injected a route via host-100. host-100 is a veth that's connected to pe-100 which resides inside
	// the OpenPERouter and which is attached to VRF red. From there, the packet is then SNATed and travels to
	// the cluster external hosts inside VRF red via SRv6.
	// Scenario b):
	// We set l2GatewayIPs in the l2vni and remove the default route via eth0. Instead, we add a default route via
	// the pod's secondary interface (net1) to the L2 gateway. Traffic frames leave via net1, br-hs-110, host-110,
	// pe-110 to br-pe-110. The l2Gateway is on br-pe-110. Once a packet hits the l2Gateway, it is routed via SRv6 to
	// the destination host.
	// Diagram:
	// openperouter|
	// ------------|
	//
	//            vrf red
	//               |-----------------------|
	//                           |           |
	//                           |           |
	//                           |           |
	//                           |         br-pe-110 (192.170.1.1)
	//                           |           |   |
	//                           |           |   |
	//                        pe-100    vni110   pe-110
	//                        (veth)             (veth)
	//                           |               |
	// --------                  |               |        -----------
	// host|                     |               |
	// ----|                     |               |
	//                       host-100          host-110
	//                     192.169.10.x          |
	//                     BGP with frr          |
	//                                         br-hs-110
	//                                           |
	// --------                                  |        -----------
	// pod|                                      |
	// ---|                                      |
	//                                         net1
	//                                        (macvlan)
	DescribeTable("should create two pods connected to the l2 overlay that can reach each other via L2VNI "+
		"and hosts via L3VPN", func(tc testCase) {
		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.L2GatewayIPs = tc.l2GatewayIPs
		l2VniRedWithGateway.Spec.HostMaster = &tc.hostMaster

		By("configuring FRR to peer with OpenPERouter")
		frrK8sConfigRed, err := frrk8s.ConfigFromHostSession(*vniRed.Spec.HostSession, vniRed.Name)
		Expect(err).NotTo(HaveOccurred())

		By("creating L3VPN and L2VNI")
		err = Updater.Update(config.Resources{
			L3VPNs: []v1alpha1.L3VPN{
				vniRed,
			},
			L2VNIs: []v1alpha1.L2VNI{
				*l2VniRedWithGateway,
			},
			FRRConfigurations: frrK8sConfigRed,
		})
		Expect(err).NotTo(HaveOccurred())

		By("creating the namespace")
		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("creating the MacVLAN Network Attachment Definition")
		netAttachDef, err = k8s.CreateMacvlanNad(fmt.Sprintf("%d", vniID), testNamespace, tc.nadMaster, tc.l2GatewayIPs)
		Expect(err).NotTo(HaveOccurred())

		By("creating the pods")
		firstPod, err = k8s.CreateAgnhostPod(cs, "pod1", testNamespace, k8s.WithNad(netAttachDef.Name, testNamespace, tc.firstPodIPs), k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err = k8s.CreateAgnhostPod(cs, "pod2", testNamespace, k8s.WithNad(netAttachDef.Name, testNamespace, tc.secondPodIPs), k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		// We can reach the host either via the pod's eth0 (passing through the host<->pe veth),
		// or via the secondary interface directly connected to the perouter namespace. Therefore, with l2GatewayIPs
		// present, we need to remove the gateway via eth0.
		if len(tc.l2GatewayIPs) > 0 {
			By("removing the default gateway via the primary interface")
			Expect(removeGatewayFromPod(firstPod)).To(Succeed())
			Expect(removeGatewayFromPod(secondPod)).To(Succeed())
		}

		By("Getting the pod's node")
		firstPodNode, err := cs.CoreV1().Nodes().Get(context.Background(), firstPod.Spec.NodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		secondPodNode, err := cs.CoreV1().Nodes().Get(context.Background(), secondPod.Spec.NodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		// hostSide is the IP address that was assigned to the host on the localCIDR.
		// E.g.: OpenPERouter: pe-100 (192.169.10.1) <-> Host OS: host-100 (192.169.10.3)
		// Traffic leaving the OpenPERouter will be SNATted to the host IP address, meaning that leafSRV6 is
		// expected to see e.g. 192.169.10.3.
		localCIDRV4 := ptr.Deref(vniRed.Spec.HostSession.LocalCIDR.IPv4, "")
		localCIDRV6 := ptr.Deref(vniRed.Spec.HostSession.LocalCIDR.IPv6, "")

		By(fmt.Sprintf("Getting the HostIP address on CIDRs %s and %s", localCIDRV4, localCIDRV6))
		firstPodHostSideV4, err := openperouter.HostIPFromCIDRForNode(localCIDRV4, firstPodNode)
		Expect(err).NotTo(HaveOccurred())
		firstPodHostSideV6, err := openperouter.HostIPFromCIDRForNode(localCIDRV6, firstPodNode)
		Expect(err).NotTo(HaveOccurred())
		secondPodHostSideV4, err := openperouter.HostIPFromCIDRForNode(localCIDRV4, secondPodNode)
		Expect(err).NotTo(HaveOccurred())
		secondPodHostSideV6, err := openperouter.HostIPFromCIDRForNode(localCIDRV6, secondPodNode)
		Expect(err).NotTo(HaveOccurred())
		// With l2GatewayIPs, we expect to see the pod IPs. Otherwise, we pass through the host
		// and we expect to see the host's IPs (NAT).
		expectedIPsForPod := func(podCIDRs []string, podName string, l2GatewayIPs []string) []string {
			if len(l2GatewayIPs) > 0 {
				return podCIDRs
			}
			hostIPs := []string{}
			switch podName {
			case "firstPod":
				for _, cidr := range podCIDRs {
					switch ipfamily.ForCIDRString(cidr) {
					case ipfamily.IPv4:
						hostIPs = append(hostIPs, firstPodHostSideV4)
					case ipfamily.IPv6:
						hostIPs = append(hostIPs, firstPodHostSideV6)
					default:
						ginkgo.Fail(fmt.Sprintf("invalid IP family for CIDR %s", cidr))
					}
				}
			case "secondPod":
				for _, cidr := range podCIDRs {
					switch ipfamily.ForCIDRString(cidr) {
					case ipfamily.IPv4:
						hostIPs = append(hostIPs, secondPodHostSideV4)
					case ipfamily.IPv6:
						hostIPs = append(hostIPs, secondPodHostSideV6)
					default:
						ginkgo.Fail(fmt.Sprintf("invalid IP family for CIDR %s", cidr))
					}
				}
			default:
				ginkgo.Fail(fmt.Sprintf("invalid pod name %s", podName))
			}
			return hostIPs
		}

		podExecutor := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
		secondPodExecutor := executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost")
		hostSRV6RedExecutor := executor.ForContainer("clab-kind-hostSRV6_red")

		tests := []struct {
			exec        executor.Executor
			from        string
			to          string
			fromIPs     []string
			toIPs       []string
			expectedIPs []string
		}{
			{
				exec: podExecutor, from: "firstPod", to: "secondPod",
				fromIPs: tc.firstPodIPs, toIPs: tc.secondPodIPs,
				expectedIPs: tc.firstPodIPs,
			},
			{
				exec: secondPodExecutor, from: "secondPod", to: "firstPod",
				fromIPs: tc.secondPodIPs, toIPs: tc.firstPodIPs,
				expectedIPs: tc.secondPodIPs,
			},
			{
				exec: podExecutor, from: "firstPod", to: "hostSRV6Red",
				fromIPs: tc.firstPodIPs, toIPs: tc.hostSRV6RedIPs,
				expectedIPs: expectedIPsForPod(tc.firstPodIPs, "firstPod", tc.l2GatewayIPs),
			},
			{
				exec: secondPodExecutor, from: "secondPod", to: "hostSRV6Red",
				fromIPs: tc.secondPodIPs, toIPs: tc.hostSRV6RedIPs,
				expectedIPs: expectedIPsForPod(tc.secondPodIPs, "secondPod", tc.l2GatewayIPs),
			},
		}

		// Reaching the pods from the host is only possible when traffic passes via the l2GatewayIPs.
		if len(tc.l2GatewayIPs) > 0 {
			tests = append(tests, struct {
				exec        executor.Executor
				from        string
				to          string
				fromIPs     []string
				toIPs       []string
				expectedIPs []string
			}{
				exec: hostSRV6RedExecutor, from: "hostSRV6Red", to: "firstPod",
				fromIPs: tc.hostSRV6RedIPs, toIPs: tc.firstPodIPs,
				expectedIPs: tc.hostSRV6RedIPs,
			})
		}

		for _, test := range tests {
			By(fmt.Sprintf("checking reachability from %s to %s", test.from, test.to))
			Expect(test.fromIPs).To(HaveLen(len(test.toIPs)))
			for i, fromIP := range test.fromIPs {
				from := discardAddressLength(fromIP)
				to := discardAddressLength(test.toIPs[i])
				expected := discardAddressLength(test.expectedIPs[i])
				checkPodIsReachableWithExpected(test.exec, from, to, expected, 60)
			}
		}
	},
		Entry("for single stack ipv4 without gatewayIPs (traffic via L3 host interface)", testCase{
			firstPodIPs:    []string{"192.171.24.2/24"},
			secondPodIPs:   []string{"192.171.24.3/24"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv4},
			nadMaster:      fmt.Sprintf("br-hs-%d", vniID),
			hostMaster: v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: ptr.To(true),
				},
			},
		}),
		Entry("for single stack ipv4", testCase{
			l2GatewayIPs:   []string{"192.171.24.1/24"},
			firstPodIPs:    []string{"192.171.24.2/24"},
			secondPodIPs:   []string{"192.171.24.3/24"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv4},
			nadMaster:      fmt.Sprintf("br-hs-%d", vniID),
			hostMaster: v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
		}),
		Entry("for dual stack", testCase{
			l2GatewayIPs:   []string{"192.171.24.1/24", "fd00:10:245:1::1/64"},
			firstPodIPs:    []string{"192.171.24.2/24", "fd00:10:245:1::2/64"},
			secondPodIPs:   []string{"192.171.24.3/24", "fd00:10:245:1::3/64"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv4, infra.HostSRV6RedIPv6},
			nadMaster:      fmt.Sprintf("br-hs-%d", vniID),
			hostMaster: v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
		}),
		Entry("for single stack ipv6", testCase{
			l2GatewayIPs:   []string{"fd00:10:245:1::1/64"},
			firstPodIPs:    []string{"fd00:10:245:1::2/64"},
			secondPodIPs:   []string{"fd00:10:245:1::3/64"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv6},
			nadMaster:      fmt.Sprintf("br-hs-%d", vniID),
			hostMaster: v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
		}),
		Entry("OVS bridge autocreate for single stack ipv4", testCase{
			l2GatewayIPs:   []string{"192.171.24.1/24"},
			firstPodIPs:    []string{"192.171.24.2/24"},
			secondPodIPs:   []string{"192.171.24.3/24"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv4},
			nadMaster:      fmt.Sprintf("br-hs-%d", vniID),
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					AutoCreate: new(true),
				},
			},
		}),
		Entry("OVS bridge autocreate for dual stack", testCase{
			l2GatewayIPs:   []string{"192.171.24.1/24", "fd00:10:245:1::1/64"},
			firstPodIPs:    []string{"192.171.24.2/24", "fd00:10:245:1::2/64"},
			secondPodIPs:   []string{"192.171.24.3/24", "fd00:10:245:1::3/64"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv4, infra.HostSRV6RedIPv6},
			nadMaster:      fmt.Sprintf("br-hs-%d", vniID),
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					AutoCreate: new(true),
				},
			},
		}),
		Entry("OVS bridge autocreate for single stack ipv6", testCase{
			l2GatewayIPs:   []string{"fd00:10:245:1::1/64"},
			firstPodIPs:    []string{"fd00:10:245:1::2/64"},
			secondPodIPs:   []string{"fd00:10:245:1::3/64"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv6},
			nadMaster:      fmt.Sprintf("br-hs-%d", vniID),
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					AutoCreate: new(true),
				},
			},
		}),
		Entry("OVS bridge existing for single stack ipv4", testCase{
			l2GatewayIPs:   []string{"192.171.24.1/24"},
			firstPodIPs:    []string{"192.171.24.2/24"},
			secondPodIPs:   []string{"192.171.24.3/24"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv4},
			nadMaster:      preExistingOVSBridge,
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name:       ptr.To(preExistingOVSBridge),
					AutoCreate: new(false),
				},
			},
		}),
		Entry("OVS bridge existing for dual stack", testCase{
			l2GatewayIPs:   []string{"192.171.24.1/24", "fd00:10:245:1::1/64"},
			firstPodIPs:    []string{"192.171.24.2/24", "fd00:10:245:1::2/64"},
			secondPodIPs:   []string{"192.171.24.3/24", "fd00:10:245:1::3/64"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv4, infra.HostSRV6RedIPv6},
			nadMaster:      preExistingOVSBridge,
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name:       ptr.To(preExistingOVSBridge),
					AutoCreate: new(false),
				},
			},
		}),
		Entry("OVS bridge existing for single stack ipv6", testCase{
			l2GatewayIPs:   []string{"fd00:10:245:1::1/64"},
			firstPodIPs:    []string{"fd00:10:245:1::2/64"},
			secondPodIPs:   []string{"fd00:10:245:1::3/64"},
			hostSRV6RedIPs: []string{infra.HostSRV6RedIPv6},
			nadMaster:      preExistingOVSBridge,
			hostMaster: v1alpha1.HostMaster{
				Type: ovsBridgeHostAttachment,
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name:       ptr.To(preExistingOVSBridge),
					AutoCreate: new(false),
				},
			},
		}),
	)
})
