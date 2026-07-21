// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
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
	"github.com/openperouter/openperouter/e2etests/pkg/networklayerprotocol"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("IPv6 VTEP", Ordered, func() {
	const (
		testNamespace             = "test-ipv6-vtep"
		linuxBridgeHostAttachment = "linux-bridge"
	)

	var (
		cs      clientset.Interface
		routers openperouter.Routers
		nodes   []corev1.Node
	)

	dualStackUnderlay := v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "underlay",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN: 64514,
			Interfaces: []v1alpha1.UnderlayInterface{
				{
					Type:          "NetworkDevice",
					NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"},
				},
				{
					Type:          "NetworkDevice",
					NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch2"},
				},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64512)),
					Address: new("192.168.11.2"),
				},
				{
					ASN:     new(int64(64513)),
					Address: new("192.168.12.2"),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24", "fd00:64::/120"},
			},
		},
	}

	l3VniV4 := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red-v4",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: 100,
		},
	}

	l2VniV4 := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red-v4-110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain: l3vniRoutingDomain("red-v4"),
			VNI:           110,
			GatewayIPs:    []string{"192.171.24.1/24"},
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: new(true),
				},
			},
		},
	}

	l3VniV6 := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "green-v6",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF:                   "green",
			VNI:                   300,
			UnderlayAddressFamily: new("ipv6"),
		},
	}

	l2VniV6 := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "green-v6-310",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			RoutingDomain:         l3vniRoutingDomain("green-v6"),
			VNI:                   310,
			UnderlayAddressFamily: new("ipv6"),
			GatewayIPs:            []string{"192.173.24.1/24"},
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
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routers.Dump(ginkgo.GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{dualStackUnderlay},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs, testNamespace)
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafBConfig.Reset()).To(Succeed())
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
		err = k8s.DeleteNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())
	})

	BeforeEach(func() {
		var err error
		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())

		By("setting redistribute connected on leaves")
		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())

		By("configuring leafkind switches with IPv6 unicast address family")
		leafKindConfig := infra.LeafKindConfiguration{
			BGPAddressFamilies: []networklayerprotocol.NLP{
				{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
				{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
			},
		}
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, leafKindConfig)).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, leafKindConfig)).To(Succeed())
	})

	setupOverlay := func(l3vnis []v1alpha1.L3VNI, l2vnis []v1alpha1.L2VNI) {
		GinkgoHelper()

		err := Updater.Update(config.Resources{
			L3VNIs: l3vnis,
			L2VNIs: l2vnis,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())
	}

	waitForBGPSessions := func() {
		GinkgoHelper()
		leafExec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				leafExec,
				validationParameters{
					fromName:    infra.KindLeaf,
					toName:      node.Name,
					neighborIP:  neighborIP,
					established: Established,
				},
			)
		}
	}

	createPodsOnVNI := func(l2vni v1alpha1.L2VNI, firstName, secondName string, firstPodIPs, secondPodIPs []string) (*corev1.Pod, *corev1.Pod) {
		GinkgoHelper()

		nadMaster := fmt.Sprintf("br-hs-%d", l2vni.Spec.VNI)
		nad, err := k8s.CreateMacvlanNad(fmt.Sprintf("%d", l2vni.Spec.VNI), testNamespace, nadMaster, l2vni.Spec.GatewayIPs)
		Expect(err).NotTo(HaveOccurred())

		firstPod, err := k8s.CreateAgnhostPod(cs, firstName, testNamespace,
			k8s.WithNad(nad.Name, testNamespace, firstPodIPs),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())
		secondPod, err := k8s.CreateAgnhostPod(cs, secondName, testNamespace,
			k8s.WithNad(nad.Name, testNamespace, secondPodIPs),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		Expect(removeGatewayFromPod(firstPod)).To(Succeed())
		Expect(removeGatewayFromPod(secondPod)).To(Succeed())

		return firstPod, secondPod
	}

	verifyPodToPod := func(l2vni v1alpha1.L2VNI, firstPodIPs, secondPodIPs []string) {
		GinkgoHelper()

		firstPod, secondPod := createPodsOnVNI(l2vni, "pod1", "pod2", firstPodIPs, secondPodIPs)

		waitForBGPSessions()

		firstPodExec := executor.ForPod(firstPod.Namespace, firstPod.Name, "agnhost")
		secondPodExec := executor.ForPod(secondPod.Namespace, secondPod.Name, "agnhost")

		By(fmt.Sprintf("trying to hit %s from %s", discardAddressLength(secondPodIPs[0]), discardAddressLength(firstPodIPs[0])))
		checkPodIsReachable(firstPodExec, discardAddressLength(firstPodIPs[0]), discardAddressLength(secondPodIPs[0]))

		By("checking reachability from secondPod to firstPod")
		checkPodIsReachable(secondPodExec, discardAddressLength(secondPodIPs[0]), discardAddressLength(firstPodIPs[0]))
	}

	Context("should establish pod-to-pod connectivity over L2VNI", func() {
		It("with IPv4 VTEP", func() {
			setupOverlay([]v1alpha1.L3VNI{l3VniV4}, []v1alpha1.L2VNI{l2VniV4})
			verifyPodToPod(l2VniV4, []string{"192.171.24.2/24"}, []string{"192.171.24.3/24"})
		})

		It("with IPv6 VTEP", func() {
			setupOverlay([]v1alpha1.L3VNI{l3VniV6}, []v1alpha1.L2VNI{l2VniV6})
			verifyPodToPod(l2VniV6, []string{"192.173.24.2/24"}, []string{"192.173.24.3/24"})
		})
	})

	It("should not affect IPv4 VNI traffic when IPv6 VNI is active", func() {
		setupOverlay(
			[]v1alpha1.L3VNI{l3VniV4, l3VniV6},
			[]v1alpha1.L2VNI{l2VniV4, l2VniV6},
		)

		v4FirstPodIPs := []string{"192.171.24.2/24"}
		v4SecondPodIPs := []string{"192.171.24.3/24"}
		v6FirstPodIPs := []string{"192.173.24.2/24"}
		v6SecondPodIPs := []string{"192.173.24.3/24"}

		By("creating pods for IPv4 VTEP VNI")
		v4Pod1, v4Pod2 := createPodsOnVNI(l2VniV4, "v4-pod1", "v4-pod2", v4FirstPodIPs, v4SecondPodIPs)

		By("creating pods for IPv6 VTEP VNI")
		v6Pod1, v6Pod2 := createPodsOnVNI(l2VniV6, "v6-pod1", "v6-pod2", v6FirstPodIPs, v6SecondPodIPs)

		waitForBGPSessions()

		By("verifying IPv4 VTEP pod-to-pod reachability")
		v4Pod1Exec := executor.ForPod(v4Pod1.Namespace, v4Pod1.Name, "agnhost")
		v4Pod2Exec := executor.ForPod(v4Pod2.Namespace, v4Pod2.Name, "agnhost")
		checkPodIsReachable(v4Pod1Exec, discardAddressLength(v4FirstPodIPs[0]), discardAddressLength(v4SecondPodIPs[0]))
		checkPodIsReachable(v4Pod2Exec, discardAddressLength(v4SecondPodIPs[0]), discardAddressLength(v4FirstPodIPs[0]))

		By("verifying IPv6 VTEP pod-to-pod reachability")
		v6Pod1Exec := executor.ForPod(v6Pod1.Namespace, v6Pod1.Name, "agnhost")
		v6Pod2Exec := executor.ForPod(v6Pod2.Namespace, v6Pod2.Name, "agnhost")
		checkPodIsReachable(v6Pod1Exec, discardAddressLength(v6FirstPodIPs[0]), discardAddressLength(v6SecondPodIPs[0]))
		checkPodIsReachable(v6Pod2Exec, discardAddressLength(v6SecondPodIPs[0]), discardAddressLength(v6FirstPodIPs[0]))

		By("verifying IPv4 VTEP pod-to-host reachability")
		hostAExec := executor.ForContainer("clab-kind-hostA_red")
		checkPodIsReachable(v4Pod1Exec, discardAddressLength(v4FirstPodIPs[0]), infra.HostARedIPv4)
		checkPodIsReachable(v4Pod1Exec, discardAddressLength(v4FirstPodIPs[0]), infra.HostBRedIPv4)
		checkPodIsReachable(hostAExec, infra.HostARedIPv4, discardAddressLength(v4FirstPodIPs[0]))
	})
})
