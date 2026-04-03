// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// Test IPv6 and Unnumbered peering in a separate context, and run a handful of tests only for l2vpn, l3vpn and
// passthrough. These tests are basically copies of the tests in other files. The reasons for using a separate context
// are:
// - This reduces the number of overall tests run (by just spotchecking that IPv6 and Unnumbered functionality work).
// - It reduces the number of underlay creations and teardowns.
var _ = Describe("Routes between bgp and the fabric", Ordered, func() {
	underlays := map[ipfamily.Family]v1alpha1.Underlay{
		ipfamily.IPv6:       infra.UnderlayIPv6,
		ipfamily.Unnumbered: infra.UnderlayUnnumbered,
	}

	for af, underlay := range underlays {
		Context(fmt.Sprintf("with Underlay in %s", af), func() {
			var cs clientset.Interface
			var routers openperouter.Routers
			var nodes []corev1.Node

			BeforeAll(func() {
				err := Updater.CleanAll()
				Expect(err).NotTo(HaveOccurred())

				cs = k8sclient.New()

				nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				Expect(err).NotTo(HaveOccurred())
				nodes = nodesItems.Items
				err = infra.UpdateLeafKindConfig(nodes, infra.WithIPFamily(af))

				routers, err = openperouter.Get(cs, HostMode)
				Expect(err).NotTo(HaveOccurred())

				routers.Dump(GinkgoWriter)

				err = Updater.Update(config.Resources{
					Underlays: []v1alpha1.Underlay{
						underlay,
					},
				})
				Expect(err).NotTo(HaveOccurred())
			})

			AfterAll(func() {
				err := infra.UpdateLeafKindConfig(nodes)
				Expect(err).NotTo(HaveOccurred())
				err = Updater.CleanAll()
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

			Context("passthrough", func() {
				passthrough := v1alpha1.L3Passthrough{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "passthrough",
						Namespace: openperouter.Namespace,
					},
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							ASN:     64514,
							HostASN: 64515,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.169.10.0/24",
								IPv6: "2001:db8:1::/64",
							},
						},
					},
				}

				Context("with passthrough and frr-k8s", func() {
					ShouldExist := true
					frrk8sPods := []*corev1.Pod{}
					frrK8sConfigRed, err := frrk8s.ConfigFromHostSession(passthrough.Spec.HostSession, passthrough.Name)
					if err != nil {
						panic(err)
					}
					frrK8sConfigBlue, err := frrk8s.ConfigFromHostSession(passthrough.Spec.HostSession, passthrough.Name)
					if err != nil {
						panic(err)
					}

					BeforeEach(func() {
						frrk8sPods, err = frrk8s.Pods(cs)
						Expect(err).NotTo(HaveOccurred())

						DumpPods("FRRK8s pods", frrk8sPods)

						err = Updater.Update(config.Resources{
							L3Passthrough: []v1alpha1.L3Passthrough{
								passthrough,
							},
							FRRConfigurations: append(frrK8sConfigRed, frrK8sConfigBlue...),
						})
						Expect(err).NotTo(HaveOccurred())

						validateFRRK8sSessionForHostSession(passthrough.Name, passthrough.Spec.HostSession, Established, frrk8sPods...)
					})

					AfterEach(func() {
						dumpIfFails(cs)
						err := Updater.CleanButUnderlay()
						Expect(err).NotTo(HaveOccurred())
						Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
						Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
					})

					It("translates BGP incoming routes as BGP routes", func() {

						By("advertising routes from both leaves")
						Expect(infra.LeafAConfig.ChangePrefixes(leafADefaultPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
						Expect(infra.LeafBConfig.ChangePrefixes(leafBDefaultPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())

						By("checking routes are propagated via BGP")

						for _, frrk8s := range frrk8sPods {
							checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafADefaultPrefixes, ShouldExist)
							checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafBDefaultPrefixes, ShouldExist)
						}

						By("removing routes from the leaf B")
						Expect(infra.LeafAConfig.ChangePrefixes(leafADefaultPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
						Expect(infra.LeafBConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())

						By("checking routes are propagated via BGP")

						for _, frrk8s := range frrk8sPods {
							checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafADefaultPrefixes, ShouldExist)
							checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafBDefaultPrefixes, !ShouldExist)
						}
					})
				})
			})

			Context("l2vpn", func() {
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

				BeforeAll(func() {
					// Create pre-existing OVS bridges on Kind nodes for testing
					nodes := []string{infra.KindControlPlane, infra.KindWorker}
					for _, nodeName := range nodes {
						exec := executor.ForContainer(nodeName)
						// Create OVS bridge (ignore error if bridge already exists)
						_, err := exec.Exec("ovs-vsctl", "add-br", preExistingOVSBridge)
						Expect(err).NotTo(HaveOccurred())
					}
				})

				AfterAll(func() {
					// Clean up pre-existing OVS bridges
					nodes := []string{infra.KindControlPlane, infra.KindWorker}
					for _, nodeName := range nodes {
						exec := executor.ForContainer(nodeName)
						_, err := exec.Exec("ovs-vsctl", "--if-exists", "del-br", preExistingOVSBridge)
						Expect(err).NotTo(HaveOccurred())
					}
				})

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
						dumpIfFails(cs)
						Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
						Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
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
								AutoCreate: true,
							},
						},
					}),
				)
			})

			Context("l3vpn", func() {
				vniRed := v1alpha1.L3VNI{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "red",
						Namespace: openperouter.Namespace,
					},
					Spec: v1alpha1.L3VNISpec{
						VRF: "red",
						HostSession: &v1alpha1.HostSession{
							ASN:     64514,
							HostASN: 64515,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.169.10.0/24",
								IPv6: "2001:db8:1::/64",
							},
						},
						VNI: 100,
					},
				}

				vniBlue := v1alpha1.L3VNI{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "blue",
						Namespace: openperouter.Namespace,
					},
					Spec: v1alpha1.L3VNISpec{
						VRF: "blue",
						HostSession: &v1alpha1.HostSession{
							ASN:     64514,
							HostASN: 64515,
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: "192.169.11.0/24",
								IPv6: "2001:db8:2::/64",
							},
						},
						VNI: 200,
					},
				}

				Context("with vnis", func() {
					AfterEach(func() {
						dumpIfFails(cs)
						err := Updater.CleanButUnderlay()
						Expect(err).NotTo(HaveOccurred())
						Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
						Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
					})

					BeforeEach(func() {
						err := Updater.Update(config.Resources{
							L3VNIs: []v1alpha1.L3VNI{
								vniRed,
								vniBlue,
							},
						})
						Expect(err).NotTo(HaveOccurred())

					})

					It("receives type 5 routes from the fabric", func() {
						Contains := true
						checkRouteFromLeaf := func(leaf infra.Leaf, vni v1alpha1.L3VNI, mustContain bool, prefixes []string) {
							By(fmt.Sprintf("checking routes from leaf %s on vni %s, mustContain %v %v", leaf.Name, vni.Name, mustContain, prefixes))
							Eventually(func() error {
								for exec := range routers.GetExecutors() {
									evpn, err := frr.EVPNInfo(exec)
									if err != nil {
										return fmt.Errorf("failed to get EVPN info from %s: %w", exec.Name(), err)
									}
									for _, prefix := range prefixes {
										if mustContain && !evpn.ContainsType5RouteForVNI(prefix, leaf.VTEPIP, int(vni.Spec.VNI)) {
											return fmt.Errorf("type5 route for %s - %s not found in %v in router %s", prefix, leaf.VTEPIP, evpn, exec.Name())
										}
										if !mustContain && evpn.ContainsType5RouteForVNI(prefix, leaf.VTEPIP, int(vni.Spec.VNI)) {
											return fmt.Errorf("type5 route for %s - %s found in %v in router %s", prefix, leaf.VTEPIP, evpn, exec.Name())
										}
									}
								}
								return nil
							}, 3*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
						}

						By("announcing type 5 routes on VNI 100 from leafA")
						Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, leafAVRFRedPrefixes, emptyPrefixes)).To(Succeed())
						checkRouteFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafAConfig, vniBlue, !Contains, leafAVRFBluePrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniRed, !Contains, leafBVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniBlue, !Contains, leafBVRFBluePrefixes)

						By("announcing type5 route on VNI 200 from leafA")
						Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, leafAVRFRedPrefixes, leafAVRFBluePrefixes)).To(Succeed())
						checkRouteFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafAConfig, vniBlue, Contains, leafAVRFBluePrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniRed, !Contains, leafBVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniBlue, !Contains, leafBVRFBluePrefixes)

						By("announcing type5 route on both VNIs from leafB")
						Expect(infra.LeafBConfig.ChangePrefixes(emptyPrefixes, leafBVRFRedPrefixes, leafBVRFBluePrefixes)).To(Succeed())
						checkRouteFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafAConfig, vniBlue, Contains, leafAVRFBluePrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniBlue, Contains, leafBVRFBluePrefixes)

						By("removing a route from leafA on vni 100")
						Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, leafAVRFBluePrefixes)).To(Succeed())
						checkRouteFromLeaf(infra.LeafAConfig, vniRed, !Contains, leafAVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafAConfig, vniBlue, Contains, leafAVRFBluePrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniBlue, Contains, leafBVRFBluePrefixes)

						By("removing a route from leafA on vni 200")
						Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
						checkRouteFromLeaf(infra.LeafAConfig, vniRed, !Contains, leafAVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafAConfig, vniBlue, !Contains, leafAVRFBluePrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes)
						checkRouteFromLeaf(infra.LeafBConfig, vniBlue, Contains, leafBVRFBluePrefixes)
					})

				})
			})
		})
	}
})
