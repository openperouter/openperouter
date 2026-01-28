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
		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(ginkgo.GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

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

		removeLeafPrefixes(infra.LeafAConfig)
		removeLeafPrefixes(infra.LeafBConfig)
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
				for exec := range routers.GetExecutors() {
					evpn, err := frr.EVPNInfo(exec)
					if err != nil {
						return fmt.Errorf("failed to get EVPN info from %s: %w", exec.Name(), err)
					}
					if !evpn.ContainsType2MACIPRouteForVNI(podIPOnly, vtepIPOnly, l2VNI) {
						return fmt.Errorf("type 2 MAC+IP route for %s not found in router %s", podIPOnly, exec.Name())
					}
				}
				return nil
			}, 3*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

			By("Verifying Type 2 routes persist for 90 seconds (route NOT withdrawn)")
			// without BridgeRefresher, the neighbor entry would go
			// STALE -> DELETE after gc_stale_time, causing the Type 2 route
			// to be withdrawn. With BridgeRefresher, ARP probes keep the neighbor
			// alive and the route persists.
			Consistently(func() error {
				for exec := range routers.GetExecutors() {
					evpn, err := frr.EVPNInfo(exec)
					if err != nil {
						return fmt.Errorf("failed to get EVPN info from %s: %w", exec.Name(), err)
					}
					if !evpn.ContainsType2MACIPRouteForVNI(podIPOnly, vtepIPOnly, l2VNI) {
						return fmt.Errorf(
							"type 2 MAC+IP route for %s was WITHDRAWN from router %s - BridgeRefresher may not be working",
							podIPOnly,
							exec.Name(),
						)
					}
				}
				return nil
			}, 90*time.Second, 10*time.Second).ShouldNot(HaveOccurred())

			By("Type 2 route persisted successfully - BridgeRefresher is working")
		})
	})
})
