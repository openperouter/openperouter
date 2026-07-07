// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
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
	"github.com/openperouter/openperouter/e2etests/pkg/url"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("SRV6 routes between bgp and the fabric", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	const (
		ShouldExist    = true
		ShouldNotExist = false
	)

	// l3vpnRed uses explicitly defined export route targets and throws in some
	// dummy RTs for safe measure.
	l3vpnRed := v1alpha1.L3VPN{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VPNSpec{
			VRF: "red",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: ptr.To(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: ptr.To("192.169.10.0/24"),
					IPv6: ptr.To("2001:db8:1::/64"),
				},
			},
			RDAssignedNumber: 100,
			ExportRTs: []v1alpha1.RouteTarget{
				"64514:100",
				"11111:100",
			},
			ImportRTs: []v1alpha1.RouteTarget{
				"64520:100",
				"22222:100",
			},
		},
	}

	// l3vpnBlue uses auto-derived export route targets.
	l3vpnBlue := v1alpha1.L3VPN{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blue",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VPNSpec{
			VRF: "blue",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: ptr.To(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: ptr.To("192.169.11.0/24"),
					IPv6: ptr.To("2001:db8:2::/64"),
				},
			},
			RDAssignedNumber: 200,
			ImportRTs: []v1alpha1.RouteTarget{
				"64520:200",
			},
		},
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		cs = k8sclient.New()

		// Create the SRv6 underlay.
		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.UnderlaySRv6,
			},
		})).To(Succeed())

		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routers.Dump(ginkgo.GinkgoWriter)

		By("Making sure that the leaf configuration is ready for SRv6 before running the first SRv6 test")
		Expect(infra.LeafSRV6Config.Reset()).To(Succeed())

		// In theory, this is not needed for these tests. However, do this here for consistency, as we want to avoid
		// pollution from other tests.
		By("resetting the leaf kind nodes")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
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

	Context("with L3VPNs", func() {
		AfterEach(func() {
			dumpIfFails(cs)
			Expect(Updater.CleanButUnderlay()).To(Succeed())

			By("Making sure that the leaf configuration is reset for SRv6 after the test")
			Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
		})

		BeforeEach(func() {
			By("Creating the red and blue L3VPN Custom Resources")
			Expect(Updater.Update(config.Resources{
				L3VPNs: []v1alpha1.L3VPN{
					l3vpnRed,
					l3vpnBlue,
				},
			})).To(Succeed())
		})

		It("receives L3VPN routes from the fabric", func() {
			checkRouteFromLeaf := func(leaf infra.Leaf, l3vpn v1alpha1.L3VPN, mustContain bool, prefixes []string) {
				By(fmt.Sprintf("checking routes from leaf %s on vni %s, mustContain %v %v", leaf.Name, l3vpn.Name, mustContain, prefixes))
				Eventually(func() error {
					for exec := range routers.GetExecutors() {
						l3vpnIPv4, err := frr.L3VPNInfo(exec, ipfamily.IPv4)
						if err != nil {
							return fmt.Errorf("failed to get L3VPN info for IPv4 from %s: %w", exec.Name(), err)
						}
						l3vpnIPv6, err := frr.L3VPNInfo(exec, ipfamily.IPv6)
						if err != nil {
							return fmt.Errorf("failed to get L3VPN info for IPv6 from %s: %w", exec.Name(), err)
						}
						for _, prefix := range prefixes {
							switch af := ipfamily.ForCIDRString(prefix); af {
							case ipfamily.IPv4:
								if mustContain != l3vpnIPv4.ContainsBGPRouteForL3VPN(
									prefix,
									leaf.RouterID,
									l3vpn.Spec.ImportRTs) {
									return fmt.Errorf("l3vpn route for %s - from router with routerID %s not found in %v in router %s", prefix, leaf.RouterID, l3vpnIPv4, exec.Name())
								}
							case ipfamily.IPv6:
								if mustContain != l3vpnIPv6.ContainsBGPRouteForL3VPN(
									prefix,
									leaf.RouterID,
									l3vpn.Spec.ImportRTs) {
									return fmt.Errorf("l3vpn route for %s - from router with routerID %s not found in %v in router %s", prefix, leaf.RouterID, l3vpnIPv6, exec.Name())
								}
							default:
								return fmt.Errorf("unsupported IP address family for prefix %s, family: %s", prefix, af)
							}
						}
					}
					return nil
				}, 3*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
			}

			Contains := true

			By("announcing L3VPN routes on VNI 100 from leafSRV6")
			Expect(infra.LeafSRV6Config.ChangePrefixes(emptyPrefixes, leafSRV6VRFRedPrefixes, emptyPrefixes)).To(Succeed())
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnRed, Contains, leafSRV6VRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnBlue, !Contains, leafSRV6VRFBluePrefixes)

			By("announcing L3VPN routes on VNI 200 from leafSRV6")
			Expect(infra.LeafSRV6Config.ChangePrefixes(emptyPrefixes, leafSRV6VRFRedPrefixes, leafSRV6VRFBluePrefixes)).To(Succeed())
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnRed, Contains, leafSRV6VRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnBlue, Contains, leafSRV6VRFBluePrefixes)

			By("removing a route from leafSRV6 on vni 100")
			Expect(infra.LeafSRV6Config.ChangePrefixes(emptyPrefixes, emptyPrefixes, leafSRV6VRFBluePrefixes)).To(Succeed())
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnRed, !Contains, leafSRV6VRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnBlue, Contains, leafSRV6VRFBluePrefixes)

			By("removing a route from leafSRV6 on vni 200")
			Expect(infra.LeafSRV6Config.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnRed, !Contains, leafSRV6VRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafSRV6Config, l3vpnBlue, !Contains, leafSRV6VRFBluePrefixes)
		})

	})

	Context("with L3VPNs and frr-k8s", func() {
		frrk8sPods := []*corev1.Pod{}

		BeforeEach(func() {
			By("generating the FRRK8sConfigurations")
			frrK8sConfigRed, err := frrk8s.ConfigFromHostSession(*l3vpnRed.Spec.HostSession, l3vpnRed.Name)
			Expect(err).NotTo(HaveOccurred())
			frrK8sConfigBlue, err := frrk8s.ConfigFromHostSession(*l3vpnBlue.Spec.HostSession, l3vpnBlue.Name)
			Expect(err).NotTo(HaveOccurred())

			frrk8sPods, err = frrk8s.Pods(cs)
			Expect(err).NotTo(HaveOccurred())

			DumpPods("FRRK8s pods", frrk8sPods)

			By("creating the L3VPNs and FRRK8sConfigurations")
			err = Updater.Update(config.Resources{
				L3VPNs: []v1alpha1.L3VPN{
					l3vpnRed,
					l3vpnBlue,
				},
				FRRConfigurations: append(frrK8sConfigRed, frrK8sConfigBlue...),
			})
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForHostSession(l3vpnRed.Name, *l3vpnRed.Spec.HostSession, Established, frrk8sPods...)
			validateFRRK8sSessionForHostSession(l3vpnBlue.Name, *l3vpnBlue.Spec.HostSession, Established, frrk8sPods...)
		})

		AfterEach(func() {
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
		})

		It("translates L3VPN incoming routes as BGP routes", func() {
			By("advertising routes from the leaves for VRF red - VNI 100")
			Expect(infra.LeafSRV6Config.ChangePrefixes(emptyPrefixes, leafSRV6VRFRedPrefixes, emptyPrefixes)).To(Succeed())

			By("checking VRF red routes are propagated via BGP hostsession to FRR")
			for _, frrk8s := range frrk8sPods {
				ginkgo.GinkgoWriter.Printf("checking on FRR-K8S pod %s: for HostSession of L3VPN %s, "+
					"looking for prefixes %v, should exist %t", frrk8s.Name, l3vpnRed.Name, leafSRV6VRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *l3vpnRed.Spec.HostSession, leafSRV6VRFRedPrefixes, ShouldExist)
			}

			By("additionally advertising routes from the leaves for VRF blue - VNI 200")
			Expect(infra.LeafSRV6Config.ChangePrefixes(emptyPrefixes, leafSRV6VRFRedPrefixes, leafSRV6VRFBluePrefixes)).To(Succeed())

			By("checking VRF red and blue routes are propagated via BGP hostsession to FRR")
			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, *l3vpnRed.Spec.HostSession, leafSRV6VRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *l3vpnBlue.Spec.HostSession, leafSRV6VRFBluePrefixes, ShouldExist)
			}

			By("removing routes from the leaves for VRF blue - VNI 200")
			Expect(infra.LeafSRV6Config.ChangePrefixes(emptyPrefixes, leafSRV6VRFRedPrefixes, emptyPrefixes)).To(Succeed())

			By("checking VRF red routes are propagated via BGP")
			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, *l3vpnRed.Spec.HostSession, leafSRV6VRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *l3vpnBlue.Spec.HostSession, leafSRV6VRFBluePrefixes, ShouldNotExist)
			}
		})
	})

	Context("testing end to end integration between a pod and the blue / red hosts", func() {
		const testNamespace = "test-namespace"
		var testPod *corev1.Pod
		var podNode *corev1.Node

		BeforeAll(func() {
			By("setting redistribute connected on leaves")
			Expect(infra.LeafSRV6Config.RedistributeConnected()).To(Succeed())

			By("Creating the test namespace")
			_, err := k8s.CreateNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Creating the test pod")
			testPod, err = k8s.CreateAgnhostPod(cs, "test-pod", testNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Getting the pod's node")
			podNode, err = cs.CoreV1().Nodes().Get(context.Background(), testPod.Spec.NodeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			nodeSelector := k8s.NodeSelectorForPod(testPod)

			By("Creating the frr-k8s configuration for the node where the test pod runs and advertising all pod ips")
			frrK8sConfigRedForPod := advertisePodToVNI(testPod, l3vpnRed.Name, l3vpnRed.Spec.HostSession, nodeSelector)
			frrK8sConfigBlueForPod := advertisePodToVNI(testPod, l3vpnBlue.Name, l3vpnBlue.Spec.HostSession, nodeSelector)

			Expect(Updater.Update(config.Resources{
				L3VPNs: []v1alpha1.L3VPN{
					l3vpnRed,
					l3vpnBlue,
				},
				FRRConfigurations: append(frrK8sConfigRedForPod, frrK8sConfigBlueForPod...),
			})).To(Succeed())

			frrK8sPodOnNode, err := frrk8s.PodForNode(cs, testPod.Spec.NodeName)
			Expect(err).NotTo(HaveOccurred())
			validateFRRK8sSessionForHostSession(l3vpnRed.Name, *l3vpnRed.Spec.HostSession, Established, frrK8sPodOnNode)
			validateFRRK8sSessionForHostSession(l3vpnBlue.Name, *l3vpnBlue.Spec.HostSession, Established, frrK8sPodOnNode)
		})

		AfterAll(func() {
			By("Deleting the test namespace")
			Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())
			Expect(Updater.CleanButUnderlay()).To(Succeed())
			Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
		})

		AfterEach(func() {
			dumpIfFails(cs)
		})

		DescribeTable("should be able to reach the hosts from the test pod and vice versa", func(
			l3vpn v1alpha1.L3VPN,
			hostName string,
			externalHostIP string,
		) {
			ipFamily, err := ipfamily.ForAddresses(externalHostIP)
			Expect(err).NotTo(HaveOccurred())

			localCIDR := ptr.Deref(l3vpn.Spec.HostSession.LocalCIDR.IPv4, "")
			if ipFamily == ipfamily.IPv6 {
				localCIDR = ptr.Deref(l3vpn.Spec.HostSession.LocalCIDR.IPv6, "")
			}

			// hostSide is the IP address that was assigned to the host on the localCIDR.
			// E.g.: OpenPERouter: pe-100 (192.169.10.1) <-> Host OS: host-100 (192.169.10.3)
			// Traffic leaving the OpenPERouter will be SNATted to the host IP address, meaning that leafSRV6 is
			// expected to see e.g. 192.169.10.3.
			By(fmt.Sprintf("Getting the HostIP address on CIDR %s", localCIDR))
			hostSide, err := openperouter.HostIPFromCIDRForNode(localCIDR, podNode)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("Getting pod %s's IP address for AF %s", testPod.Name, ipFamily))
			podIP, err := getPodIPByFamily(testPod, ipFamily)
			Expect(err).NotTo(HaveOccurred())

			podExecutor := executor.ForPod(testPod.Namespace, testPod.Name, "agnhost")
			externalHostExecutor := executor.ForContainer("clab-kind-" + hostName)

			Eventually(func() error {
				By(fmt.Sprintf("from %s: trying to hit host %s on the %s network and getting the client IP",
					testPod.Name, externalHostIP, l3vpn.Name))
				urlStr := url.Format("http://%s:8090/clientip", externalHostIP)
				res, err := podExecutor.Exec("curl", "-sS", urlStr)
				if err != nil {
					return fmt.Errorf("curl %s:8090 failed: %s", externalHostIP, res)
				}
				clientIP, err := extractClientIP(res)
				Expect(err).NotTo(HaveOccurred())

				By(fmt.Sprintf("checking that detected clientIP %s and hostSide IP %s are equal", clientIP, hostSide))
				if clientIP != hostSide {
					return fmt.Errorf("curl %s:8090 returned %s, expected %s", externalHostIP, clientIP, hostSide)
				}

				By(fmt.Sprintf("from %s: trying to hit host %s on the %s network and getting the hostname",
					testPod.Name, externalHostIP, l3vpn.Name))
				urlStr = url.Format("http://%s:8090/hostname", externalHostIP)
				res, err = podExecutor.Exec("curl", "-sS", urlStr)
				if err != nil {
					return fmt.Errorf("curl %s:8090 failed: %s", externalHostIP, res)
				}
				By(fmt.Sprintf("checking that detected hostname %s and expected hostname %s are equal", res, hostName))
				if res != hostName {
					return fmt.Errorf("curl %s:8090 returned %s, expected %s", externalHostIP, res, hostName)
				}

				By(fmt.Sprintf("from %s: trying to hit pod %s (%s) on the %s network from host %s",
					hostName, testPod.Name, podIP, l3vpn.Name, hostName))
				urlStr = url.Format("http://%s:8090/clientip", podIP)
				res, err = externalHostExecutor.Exec("curl", "-sS", urlStr)
				if err != nil {
					return fmt.Errorf("curl from %s to %s:8090 failed: %s", hostName, podIP, res)
				}
				hostClientIP, err := extractClientIP(res)
				Expect(err).NotTo(HaveOccurred())

				By(fmt.Sprintf("from %s: checking that detected clientIP %s and externalHostIP %s are equal",
					hostName, clientIP, externalHostIP))
				if hostClientIP != externalHostIP {
					return fmt.Errorf("curl from %s to %s:8090 returned %s, expected %s", hostName, podIP, clientIP, externalHostIP)
				}
				return nil
			}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		},
			Entry("l3vpn red host SRV6 ipv4", l3vpnRed, "hostSRV6_red", infra.HostSRV6RedIPv4),
			Entry("l3vpn blue host SRV6 ipv4", l3vpnBlue, "hostSRV6_blue", infra.HostSRV6BlueIPv4),
			Entry("l3vpn red host SRV6 ipv6", l3vpnRed, "hostSRV6_red", infra.HostSRV6RedIPv6),
			Entry("l3vpn blue host SRV6 ipv6", l3vpnBlue, "hostSRV6_blue", infra.HostSRV6BlueIPv6),
		)
	})
})

var _ = Describe("SRV6 routes between bgp and the fabric with iBGP testing e2e integration between a pod and the red hosts", func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	underlay := infra.UnderlaySRv6.DeepCopy()
	underlay.Spec.ASN = 64520

	l3vpnRed := v1alpha1.L3VPN{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VPNSpec{
			VRF: "red",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: ptr.To(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: ptr.To("192.169.10.0/24"),
					IPv6: ptr.To("2001:db8:1::/64"),
				},
			},
			RDAssignedNumber: 100,
			ExportRTs: []v1alpha1.RouteTarget{
				"64514:100",
			},
			ImportRTs: []v1alpha1.RouteTarget{
				"64520:100",
			},
		},
	}

	const testNamespace = "test-namespace-ibgp"
	var testPod *corev1.Pod
	var podNode *corev1.Node

	BeforeEach(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(ginkgo.GinkgoWriter)

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				*underlay,
			},
		})).To(Succeed())

		By("setting redistribute connected and iBGP next-hop-self force on leaves")
		Expect(infra.LeafSRV6Config.Configure(infra.LeafConfiguration{
			Red:         infra.Addresses{RedistributeConnected: true},
			Default:     infra.Addresses{RedistributeConnected: true},
			PERouterASN: 64520,
		})).To(Succeed())

		// In theory, this is not needed for these tests. However, do this here for consistency, as we want to avoid
		// pollution from other tests.
		By("resetting the leaf kind nodes")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())

		By("Creating the test namespace")
		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("Creating the test pod")
		testPod, err = k8s.CreateAgnhostPod(cs, "test-pod", testNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("Getting the pod's node")
		podNode, err = cs.CoreV1().Nodes().Get(context.Background(), testPod.Spec.NodeName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodeSelector := k8s.NodeSelectorForPod(testPod)

		By("Creating the frr-k8s configuration for the node where the test pod runs and advertising all pod ips")
		frrK8sConfigRedForPod := advertisePodToVNI(testPod, l3vpnRed.Name, l3vpnRed.Spec.HostSession, nodeSelector)

		Expect(Updater.Update(config.Resources{
			L3VPNs: []v1alpha1.L3VPN{
				l3vpnRed,
			},
			FRRConfigurations: frrK8sConfigRedForPod,
		})).To(Succeed())

		frrK8sPodOnNode, err := frrk8s.PodForNode(cs, testPod.Spec.NodeName)
		Expect(err).NotTo(HaveOccurred())
		validateFRRK8sSessionForHostSession(l3vpnRed.Name, *l3vpnRed.Spec.HostSession, Established, frrK8sPodOnNode)
	})

	AfterEach(func() {
		dumpIfFails(cs)

		By("Deleting the test namespace")
		Expect(k8s.DeleteNamespace(cs, testNamespace)).To(Succeed())
		Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
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

	It("should be able to reach the hosts from the test pod and vice versa", func() {
		hostName := "hostSRV6_red"
		externalHostIP := infra.HostSRV6RedIPv4
		ipFamily := ipfamily.IPv4

		localCIDR := ptr.Deref(l3vpnRed.Spec.HostSession.LocalCIDR.IPv4, "")

		// hostSide is the IP address that was assigned to the host on the localCIDR.
		// E.g.: OpenPERouter: pe-100 (192.169.10.1) <-> Host OS: host-100 (192.169.10.3)
		// Traffic leaving the OpenPERouter will be SNATted to the host IP address, meaning that leafSRV6 is
		// expected to see e.g. 192.169.10.3.
		By(fmt.Sprintf("Getting the HostIP address on CIDR %s", localCIDR))
		hostSide, err := openperouter.HostIPFromCIDRForNode(localCIDR, podNode)
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("Getting pod %s's IP address for AF %s", testPod.Name, ipFamily))
		podIP, err := getPodIPByFamily(testPod, ipFamily)
		Expect(err).NotTo(HaveOccurred())

		podExecutor := executor.ForPod(testPod.Namespace, testPod.Name, "agnhost")
		externalHostExecutor := executor.ForContainer("clab-kind-" + hostName)

		Eventually(func() error {
			By(fmt.Sprintf("from %s: trying to hit host %s on the %s network and getting the client IP",
				testPod.Name, externalHostIP, l3vpnRed.Name))
			urlStr := url.Format("http://%s:8090/clientip", externalHostIP)
			res, err := podExecutor.Exec("curl", "-sS", urlStr)
			if err != nil {
				return fmt.Errorf("curl %s:8090 failed: %s", externalHostIP, res)
			}
			clientIP, err := extractClientIP(res)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("checking that detected clientIP %s and hostSide IP %s are equal", clientIP, hostSide))
			if clientIP != hostSide {
				return fmt.Errorf("curl %s:8090 returned %s, expected %s", externalHostIP, clientIP, hostSide)
			}

			By(fmt.Sprintf("from %s: trying to hit host %s on the %s network and getting the hostname",
				testPod.Name, externalHostIP, l3vpnRed.Name))
			urlStr = url.Format("http://%s:8090/hostname", externalHostIP)
			res, err = podExecutor.Exec("curl", "-sS", urlStr)
			if err != nil {
				return fmt.Errorf("curl %s:8090 failed: %s", externalHostIP, res)
			}
			By(fmt.Sprintf("checking that detected hostname %s and expected hostname %s are equal", res, hostName))
			if res != hostName {
				return fmt.Errorf("curl %s:8090 returned %s, expected %s", externalHostIP, res, hostName)
			}

			By(fmt.Sprintf("from %s: trying to hit pod %s (%s) on the %s network from host %s",
				hostName, testPod.Name, podIP, l3vpnRed.Name, hostName))
			urlStr = url.Format("http://%s:8090/clientip", podIP)
			res, err = externalHostExecutor.Exec("curl", "-sS", urlStr)
			if err != nil {
				return fmt.Errorf("curl from %s to %s:8090 failed: %s", hostName, podIP, res)
			}
			hostClientIP, err := extractClientIP(res)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("from %s: checking that detected clientIP %s and externalHostIP %s are equal",
				hostName, clientIP, externalHostIP))
			if hostClientIP != externalHostIP {
				return fmt.Errorf("curl from %s to %s:8090 returned %s, expected %s", hostName, podIP, clientIP, externalHostIP)
			}
			return nil
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
	})
})
