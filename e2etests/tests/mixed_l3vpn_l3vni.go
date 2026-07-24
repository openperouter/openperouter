// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"time"

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Mixed L3VPN and L3VNI coexistence in different VRFs", Ordered, func() {
	var (
		cs    clientset.Interface
		nodes []corev1.Node
	)

	const (
		linuxBridgeHostAttachment = "linux-bridge"
		testNamespace             = "test-mixed-ns"

		l3vpnRDAssignedNumber = 100
		l3vpnL2VNI            = 110
		l3vniVNI              = 200
		l3vniL2VNI            = 210

		l3vpnL2GatewayIP = "192.171.24.1/24"
		firstPodIP       = "192.171.24.2/24"
		secondPodIP      = "192.171.24.3/24"

		l3vniL2GatewayIP = "192.172.24.1/24"
		thirdPodIP       = "192.172.24.2/24"
		fourthPodIP      = "192.172.24.3/24"
	)

	l3vpnRed := v1alpha1.L3VPN{
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
				},
			},
			RDAssignedNumber: l3vpnRDAssignedNumber,
			ExportRTs: []v1alpha1.RouteTarget{
				v1alpha1.RouteTarget(fmt.Sprintf("64514:%d", l3vpnRDAssignedNumber)),
			},
			ImportRTs: []v1alpha1.RouteTarget{
				v1alpha1.RouteTarget(fmt.Sprintf("64520:%d", l3vpnRDAssignedNumber)),
			},
		},
	}

	l2vniForL3VPN := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("red%d", l3vpnL2VNI),
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain: l3vpnRoutingDomain("red"),
			VNI:           l3vpnL2VNI,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
			GatewayIPs: []string{l3vpnL2GatewayIP},
		},
	}

	l3vniBlue := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blue",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "blue",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: new(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: new("192.169.11.0/24"),
				},
			},
			VNI: l3vniVNI,
		},
	}

	l2vniForL3VNI := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("blue%d", l3vniL2VNI),
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain: l3vniRoutingDomain("blue"),
			VNI:           l3vniL2VNI,
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
			GatewayIPs: []string{l3vniL2GatewayIP},
		},
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		cs = k8sclient.New()

		By("creating the underlay")
		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.UnderlayEVPNandSRv6,
			},
		})).To(Succeed())

		_, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		By("getting the nodes")
		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		By("resetting the leaf kind nodes")
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())

		By("setting redistribute connected on leaves")
		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafSRV6Config.RedistributeConnected()).To(Succeed())

		By("cleaning all but the underlay")
		Expect(Updater.CleanButUnderlay()).To(Succeed())
	})

	AfterAll(func() {
		dumpIfFails(cs, testNamespace)
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
		Expect(Updater.CleanButUnderlay()).To(Succeed())
		Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())

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

	It("should support traffic in different VRFs", func() {
		By("deriving the FRR K8s configuration from the hostsessions")
		frrK8sConfigRed, err := frrk8s.ConfigFromHostSession(*l3vpnRed.Spec.HostSession, l3vpnRed.Name)
		Expect(err).NotTo(HaveOccurred())
		frrK8sConfigBlue, err := frrk8s.ConfigFromHostSession(*l3vniBlue.Spec.HostSession, l3vniBlue.Name)
		Expect(err).NotTo(HaveOccurred())

		By("creating L3VPN, L3VNI, and L2VNIs alongside FRR K8s configuration for red and blue peers")
		Expect(Updater.Update(config.Resources{
			L3VPNs:            []v1alpha1.L3VPN{l3vpnRed},
			L3VNIs:            []v1alpha1.L3VNI{l3vniBlue},
			L2VNIs:            []v1alpha1.L2VNI{l2vniForL3VNI, l2vniForL3VPN},
			FRRConfigurations: append(frrK8sConfigRed, frrK8sConfigBlue...),
		})).To(Succeed())

		By("creating the namespace")
		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("creating the MacVLAN Network Attachment Definitions for L3VPN")
		nadMasterL3VPN := fmt.Sprintf("br-hs-%d", l3vpnL2VNI)
		netAttachDefL3VPN, err := k8s.CreateMacvlanNad(
			fmt.Sprintf("%d", l3vpnL2VNI), testNamespace, nadMasterL3VPN, []string{l3vpnL2GatewayIP})
		Expect(err).NotTo(HaveOccurred())

		By("creating the MacVLAN Network Attachment Definitions for L3VNI")
		nadMasterL3VNI := fmt.Sprintf("br-hs-%d", l3vniL2VNI)
		netAttachDefL3VNI, err := k8s.CreateMacvlanNad(
			fmt.Sprintf("%d", l3vniL2VNI), testNamespace, nadMasterL3VNI, []string{l3vniL2GatewayIP})
		Expect(err).NotTo(HaveOccurred())

		By("creating the pods connected to L3VPN")
		firstPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace,
			k8s.WithNad(netAttachDefL3VPN.Name, testNamespace, []string{firstPodIP}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err := k8s.CreateAgnhostPod(cs, "pod2", testNamespace,
			k8s.WithNad(netAttachDefL3VPN.Name, testNamespace, []string{secondPodIP}),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("creating the pods connected to L3VNI")
		thirdPod, err := k8s.CreateAgnhostPod(cs, "pod3", testNamespace,
			k8s.WithNad(netAttachDefL3VNI.Name, testNamespace, []string{thirdPodIP}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		fourthPod, err := k8s.CreateAgnhostPod(cs, "pod4", testNamespace,
			k8s.WithNad(netAttachDefL3VNI.Name, testNamespace, []string{fourthPodIP}),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface for pods connected to L3VPN")
		Expect(removeGatewayFromPod(firstPod)).To(Succeed())
		Expect(removeGatewayFromPod(secondPod)).To(Succeed())

		By("removing the default gateway via the primary interface for pods connected to L3VNI")
		Expect(removeGatewayFromPod(thirdPod)).To(Succeed())
		Expect(removeGatewayFromPod(fourthPod)).To(Succeed())

		By("getting the pod executors for pods connected to L3VPN")
		firstPodExecutor := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
		secondPodExecutor := executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost")

		By("getting the pod executors for pods connected to L3VNI")
		thirdPodExecutor := executor.ForPod(thirdPod.Namespace, thirdPod.Name, "agnhost")
		fourthPodExecutor := executor.ForPod(fourthPod.Namespace, fourthPod.Name, "agnhost")

		By("checking east/west reachability between pods via L3VPN's L2VNI")
		from := discardAddressLength(firstPodIP)
		to := discardAddressLength(secondPodIP)
		checkPodIsReachable(firstPodExecutor, from, to)
		checkPodIsReachable(secondPodExecutor, to, from)

		By("checking east/west reachability between pods via L3VNI's L2VNI")
		from = discardAddressLength(thirdPodIP)
		to = discardAddressLength(fourthPodIP)
		checkPodIsReachable(thirdPodExecutor, from, to)
		checkPodIsReachable(fourthPodExecutor, to, from)

		By("checking north/south reachability via L3VPN to hostSRV6Red")
		from = discardAddressLength(firstPodIP)
		to = infra.HostSRV6RedIPv4
		checkPodIsReachable(firstPodExecutor, from, to)

		By("checking north/south reachability via L3VNI to hostABlue")
		from = discardAddressLength(thirdPodIP)
		to = infra.HostABlueIPv4
		checkPodIsReachable(thirdPodExecutor, from, to)
	})
})
