// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
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
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/internal/ipfamily"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("BridgeRefresher E2E - Type 2 Route Persistence", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	const (
		linuxBridgeHostAttachment = "linux-bridge"
		testNamespace             = "bridgerefresh-test"
		l2VNI                     = 110
		l3VNI                     = 100
		l2GatewayIP               = "192.171.24.1/24"
		l2GatewayIPOnly           = "192.171.24.1"
		silentPodIP               = "192.171.24.10/24"
	)

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: l3VNI,
		},
	}

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF:          ptr.To("red"),
			VNI:          l2VNI,
			L2GatewayIPs: []string{l2GatewayIP},
			HostMaster: &v1alpha1.HostMaster{
				Type: linuxBridgeHostAttachment,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: true,
				},
			},
		},
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		cs = k8sclient.New()

		err := Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(ginkgo.GinkgoWriter)

		By("setting redistribute connected on leaves")
		redistributeConnectedForLeaf(infra.LeafAConfig)
		redistributeConnectedForLeaf(infra.LeafBConfig)
	})

	AfterAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
		Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
	})

	Context("Type 2 Route Persistence with Silent Workload", func() {
		var silentPod *corev1.Pod
		var testNad nad.NetworkAttachmentDefinition

		BeforeEach(func() {
			By("Creating L3 VNI and L2 VNI with gateway IPs")
			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{
					vniRed,
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VniRed,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Creating test namespace")
			_, err = k8s.CreateNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Creating macvlan NAD for VNI 110")
			testNad, err = k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{l2GatewayIP})
			Expect(err).NotTo(HaveOccurred())

			By("Getting cluster nodes")
			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).NotTo(BeEmpty(), "Expected at least 1 node")

			By("Creating silent workload pod attached to NAD")
			silentPod, err = k8s.CreateSilentPod(
				cs,
				"silent-pod",
				testNamespace,
				k8s.WithNad(testNad.Name, testNamespace, []string{silentPodIP}),
				k8s.OnNode(nodes[0].Name),
			)
			Expect(err).NotTo(HaveOccurred())

			By("Pinging gateway once to establish neighbor entry")
			podExec := executor.ForPod(testNamespace, silentPod.Name, "busybox")
			// Ping the gateway IP once via the net1 interface (macvlan attached interface)
			// This establishes the neighbor entry that BridgeRefresher will then keep alive
			out, err := podExec.Exec("ping", "-c", "1", "-I", "net1", l2GatewayIPOnly)
			Expect(err).NotTo(HaveOccurred(), "ping to gateway failed: %s", out)
		})

		AfterEach(func() {
			dumpIfFails(cs)

			By("Deleting test namespace")
			Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())

			By("Cleaning VNI resources")
			Expect(Updater.CleanButUnderlay()).To(Succeed())
		})

		It("should maintain Type 2 MAC+IP route for silent workload", func() {
			podNode, err := cs.CoreV1().Nodes().Get(context.Background(), silentPod.Spec.NodeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			vtepIP, err := openperouter.VtepIPForNode(infra.Underlay.Spec.EVPN.VTEPCIDR, podNode)
			Expect(err).NotTo(HaveOccurred())

			vtepIPOnly := ipfamily.StripCIDRMask(vtepIP)
			podIPOnly := ipfamily.StripCIDRMask(silentPodIP)

			By("Verifying Type 2 MAC+IP route appears initially")
			Eventually(func() error {
				return checkType2RouteExists(cs, podIPOnly, vtepIPOnly, l2VNI)
			}, 3*time.Minute, 5*time.Second).
				ShouldNot(HaveOccurred(), "should initially contain type 2 MAC+IP route")

			By("Verifying Type 2 routes persist for 90 seconds (route NOT withdrawn)")
			// without BridgeRefresher, the neighbor entry would go
			// STALE -> DELETE after gc_stale_time, causing the Type 2 route
			// to be withdrawn. With BridgeRefresher, ICMP pings keep the neighbor
			// alive and the route persists.
			Consistently(func() error {
				return checkType2RouteExists(cs, podIPOnly, vtepIPOnly, l2VNI)
			}, 90*time.Second, 10*time.Second).
				ShouldNot(HaveOccurred(), "should not WITHDRAWN type 2 MAC+IP route, BridgeRefresher may not be working")

			By("Type 2 route persisted successfully - BridgeRefresher is working")
		})
	})

	// This test validates the corner case where a pod migrates from one node to another while having a stale route
	// to its ip on the local router. The stale router is reflected in a corresponding NOARP route
	// on other routers, leading to a deadlock situation where the route advertised belongs to the wrong
	// node and arp coming from the right node are ignored because frr installs the route with NOARP.

	// In order to have a solid reproducer we need to ping the local gateway so the neighbor table is filled, and
	// then trigger the migration to the other node.
	Context("Pod migrates to a different node", func() {
		type migrationTestCase struct {
			l2GatewayIP     string
			migratingPodIP  string
			stationaryPodIP string
		}

		DescribeTable("should maintain connectivity after pod migrates to another node",
			func(tc migrationTestCase) {
				l2GatewayIPOnly := ipfamily.StripCIDRMask(tc.l2GatewayIP)
				migratingPodIPOnly := ipfamily.StripCIDRMask(tc.migratingPodIP)
				stationaryPodIPOnly := ipfamily.StripCIDRMask(tc.stationaryPodIP)

				l2VniRedForTC := l2VniRed.DeepCopy()
				l2VniRedForTC.Spec.L2GatewayIPs = []string{tc.l2GatewayIP}

				By("Creating L3 VNI and L2 VNI with gateway IPs")
				err := Updater.Update(config.Resources{
					L3VNIs: []v1alpha1.L3VNI{
						vniRed,
					},
					L2VNIs: []v1alpha1.L2VNI{
						*l2VniRedForTC,
					},
				})
				Expect(err).NotTo(HaveOccurred())

				By("Creating test namespace")
				_, err = k8s.CreateNamespace(cs, testNamespace)
				Expect(err).NotTo(HaveOccurred())

				By("Creating macvlan NAD for VNI 110")
				testNad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{tc.l2GatewayIP})
				Expect(err).NotTo(HaveOccurred())

				By("Getting cluster nodes")
				nodes, err := k8s.GetNodes(cs)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes")

				DeferCleanup(func() {
					dumpIfFails(cs)

					By("Deleting test namespace")
					Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())

					By("Cleaning VNI resources")
					Expect(Updater.CleanButUnderlay()).To(Succeed())
				})

				By("Creating stationary pod on node B")
				stationaryPod, err := k8s.CreateAgnhostPod(
					cs,
					"stationary-pod",
					testNamespace,
					k8s.WithNad(testNad.Name, testNamespace, []string{tc.stationaryPodIP}),
					k8s.OnNode(nodes[1].Name),
				)
				Expect(err).NotTo(HaveOccurred())

				By("Creating migrating pod on node A")
				migratingPod, err := k8s.CreateAgnhostPod(
					cs,
					"migrating-pod",
					testNamespace,
					k8s.WithNad(testNad.Name, testNamespace, []string{tc.migratingPodIP}),
					k8s.OnNode(nodes[0].Name),
				)
				Expect(err).NotTo(HaveOccurred())

				By("Removing default gateway via primary interface on both pods")
				Expect(removeGatewayFromPod(stationaryPod)).To(Succeed())
				Expect(removeGatewayFromPod(migratingPod)).To(Succeed())

				By("Verifying migrating pod can ping the local gateway")
				migratingExec := executor.ForPod(testNamespace, migratingPod.Name, "agnhost")
				canPingFromPod(migratingExec, l2GatewayIPOnly)

				By("Verifying stationary pod can reach migrating pod before migration")
				stationaryExec := executor.ForPod(testNamespace, stationaryPod.Name, "agnhost")
				checkPodIsReachable(stationaryExec, stationaryPodIPOnly, migratingPodIPOnly)

				By("Verifying Type 2 MAC+IP route exists for migrating pod on node A")
				migratingPodNode, err := cs.CoreV1().Nodes().Get(context.Background(), migratingPod.Spec.NodeName, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				vtepIP, err := openperouter.VtepIPForNode(infra.Underlay.Spec.EVPN.VTEPCIDR, migratingPodNode)
				Expect(err).NotTo(HaveOccurred())
				vtepIPOnly := ipfamily.StripCIDRMask(vtepIP)

				Eventually(func() error {
					return checkType2RouteExists(cs, migratingPodIPOnly, vtepIPOnly, l2VNI)
				}, 3*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

				// This is important to trigger the deadlock situation described in https://github.com/FRRouting/frr/issues/14156
				By("Waiting for neighbor entry to go STALE on the router on the same node")
				Eventually(func() error {
					return checkNeighborStale(cs, migratingPodIPOnly, l2VNI, migratingPod.Spec.NodeName)
				}, 3*time.Minute, 2*time.Second).ShouldNot(HaveOccurred())

				By("Deleting migrating pod (simulating pod eviction/migration)")
				err = cs.CoreV1().Pods(testNamespace).Delete(
					context.Background(),
					migratingPod.Name,
					metav1.DeleteOptions{},
				)
				Expect(err).NotTo(HaveOccurred())

				By("Waiting for migrating pod to be fully deleted")
				Eventually(func() bool {
					_, err := cs.CoreV1().Pods(testNamespace).Get(
						context.Background(),
						migratingPod.Name,
						metav1.GetOptions{},
					)
					return err != nil
				}, 2*time.Minute, time.Second).Should(BeTrue())

				By("Recreating migrating pod on node B (the other node) with same IP")
				migratingPod, err = k8s.CreateAgnhostPod(
					cs,
					"migrating-pod-new",
					testNamespace,
					k8s.WithNad(testNad.Name, testNamespace, []string{tc.migratingPodIP}),
					k8s.OnNode(nodes[1].Name),
				)
				Expect(err).NotTo(HaveOccurred())

				By("Removing default gateway on recreated pod")
				Expect(removeGatewayFromPod(migratingPod)).To(Succeed())

				By("Verifying migrated pod can ping the local gateway on new node")
				newMigratingExec := executor.ForPod(testNamespace, migratingPod.Name, "agnhost")
				canPingFromPod(newMigratingExec, l2GatewayIPOnly)

				By("Verifying stationary pod can reach migrated pod on new node")
				checkPodIsReachable(stationaryExec, stationaryPodIPOnly, migratingPodIPOnly)
			},
			Entry("ipv4", migrationTestCase{
				l2GatewayIP:     "192.171.24.1/24",
				migratingPodIP:  "192.171.24.10/24",
				stationaryPodIP: "192.171.24.11/24",
			}),
			Entry("ipv6", migrationTestCase{
				l2GatewayIP:     "fd00:10:245:1::1/64",
				migratingPodIP:  "fd00:10:245:1::10/64",
				stationaryPodIP: "fd00:10:245:1::11/64",
			}),
		)
	})
})

func checkType2RouteExists(cs clientset.Interface, podIP, vtepIP string, vni int) error {
	currentRouters, err := openperouter.Get(cs, HostMode)
	if err != nil {
		return err
	}
	for exec := range currentRouters.GetExecutors() {
		evpn, err := frr.EVPNInfo(exec)
		if err != nil {
			return fmt.Errorf("failed to get EVPN info from %s: %w", exec.Name(), err)
		}
		if !evpn.ContainsType2MACIPRouteForVNI(podIP, vtepIP, vni) {
			return fmt.Errorf("type 2 MAC+IP route for %s via VTEP %s not found in router %s", podIP, vtepIP, exec.Name())
		}
	}
	return nil
}

func checkNeighborStale(cs clientset.Interface, podIP string, vni int, nodeName string) error {
	bridgeDev := fmt.Sprintf("br-pe-%d", vni)
	currentRouters, err := openperouter.Get(cs, HostMode)
	if err != nil {
		return err
	}
	exec, err := openperouter.ExecutorForNode(currentRouters, nodeName)
	if err != nil {
		return err
	}
	out, err := exec.Exec("ip", "neigh", "show", podIP, "dev", bridgeDev)
	if err != nil {
		return fmt.Errorf("failed to check neighbor on router %s: %w", exec.Name(), err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return fmt.Errorf("no neighbor entry for %s on %s in router %s", podIP, bridgeDev, exec.Name())
	}
	if strings.Contains(out, "STALE") || strings.Contains(out, "FAILED") {
		return nil
	}
	return fmt.Errorf("neighbor %s on %s in router %s is not STALE yet: %s", podIP, bridgeDev, exec.Name(), out)
}
