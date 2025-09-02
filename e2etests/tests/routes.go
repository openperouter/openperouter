// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	frrk8sapi "github.com/metallb/frr-k8s/api/v1beta1"
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
)

var (
	// NOTE: we can't advertise any ip via EVPN from the leaves, they
	// must be reacheable otherwise FRR will skip them.
	leafAVRFRedPrefixes  = []string{"192.168.20.0/24", "2001:db8:20::/64"}
	leafAVRFBluePrefixes = []string{"192.168.21.0/24", "2001:db8:21::/64"}
	leafBVRFRedPrefixes  = []string{"192.169.20.0/24", "2001:db8:169:20::/64"}
	leafBVRFBluePrefixes = []string{"192.169.21.0/24", "2001:db8:169:21::/64"}
	emptyPrefixes        = []string{}
)

var _ = Describe("Routes between bgp and the fabric", Ordered, func() {
	var cs clientset.Interface
	routerPods := []*corev1.Pod{}

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
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

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routerPods, err = openperouter.RouterPods(cs)
		Expect(err).NotTo(HaveOccurred())

		DumpPods("Router pods", routerPods)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			return openperouter.DaemonsetRolled(cs, routerPods)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	Context("with vnis", func() {
		AfterEach(func() {
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			removeLeafPrefixes(infra.LeafAConfig)
			removeLeafPrefixes(infra.LeafBConfig)
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
					for _, p := range routerPods {
						exec := executor.ForPod(p.Namespace, p.Name, "frr")
						evpn, err := frr.EVPNInfo(exec)
						Expect(err).NotTo(HaveOccurred())
						for _, prefix := range prefixes {
							if mustContain && !evpn.ContainsType5RouteForVNI(prefix, leaf.VTEPIP, int(vni.Spec.VNI)) {
								return fmt.Errorf("type5 route for %s - %s not found in %v in router %s", prefix, leaf.VTEPIP, evpn, p.Name)
							}
							if !mustContain && evpn.ContainsType5RouteForVNI(prefix, leaf.VTEPIP, int(vni.Spec.VNI)) {
								return fmt.Errorf("type5 route for %s - %s found in %v in router %s", prefix, leaf.VTEPIP, evpn, p.Name)
							}
						}
					}
					return nil
				}, 3*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
			}

			By("announcing type 5 routes on VNI 100 from leafA")
			changeLeafPrefixes(infra.LeafAConfig, leafAVRFRedPrefixes, emptyPrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniBlue, !Contains, leafAVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniRed, !Contains, leafBVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniBlue, !Contains, leafBVRFBluePrefixes)

			By("announcing type5 route on VNI 200 from leafA")
			changeLeafPrefixes(infra.LeafAConfig, leafAVRFRedPrefixes, leafAVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniBlue, Contains, leafAVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniRed, !Contains, leafBVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniBlue, !Contains, leafBVRFBluePrefixes)

			By("announcing type5 route on both VNIs from leafB")
			changeLeafPrefixes(infra.LeafBConfig, leafBVRFRedPrefixes, leafBVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniBlue, Contains, leafAVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniBlue, Contains, leafBVRFBluePrefixes)

			By("removing a route from leafA on vni 100")
			changeLeafPrefixes(infra.LeafAConfig, emptyPrefixes, leafAVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniRed, !Contains, leafAVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniBlue, Contains, leafAVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniBlue, Contains, leafBVRFBluePrefixes)

			By("removing a route from leafA on vni 200")
			changeLeafPrefixes(infra.LeafAConfig, emptyPrefixes, emptyPrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniRed, !Contains, leafAVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafAConfig, vniBlue, !Contains, leafAVRFBluePrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes)
			checkRouteFromLeaf(infra.LeafBConfig, vniBlue, Contains, leafBVRFBluePrefixes)
		})

	})

	Context("with vnis and frr-k8s", func() {
		ShouldExist := true
		frrk8sPods := []*corev1.Pod{}
		frrK8sConfigRed, err := frrk8s.ConfigFromVNI(vniRed)
		if err != nil {
			panic(err)
		}
		frrK8sConfigBlue, err := frrk8s.ConfigFromVNI(vniBlue)
		if err != nil {
			panic(err)
		}

		BeforeEach(func() {
			frrk8sPods, err = frrk8s.Pods(cs)
			Expect(err).NotTo(HaveOccurred())

			DumpPods("FRRK8s pods", frrk8sPods)

			err = Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{
					vniRed,
					vniBlue,
				},
				FRRConfigurations: append(frrK8sConfigRed, frrK8sConfigBlue...),
			})
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForVNI(vniRed, Established, frrk8sPods...)
			validateFRRK8sSessionForVNI(vniBlue, Established, frrk8sPods...)
		})

		AfterEach(func() {
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			removeLeafPrefixes(infra.LeafAConfig)
			removeLeafPrefixes(infra.LeafBConfig)
		})

		It("translates EVPN incoming routes as BGP routes", func() {
			checkPrefixesForIPFamily := func(frrk8s *corev1.Pod, prefixes []string, localCIDR string,
				ipFamily string, shouldExist bool, routes frr.BGPRoutes) error {
				if len(prefixes) == 0 || localCIDR == "" {
					return nil
				}

				vniRouterIP, err := openperouter.RouterIPFromCIDR(localCIDR)
				Expect(err).NotTo(HaveOccurred())

				for _, p := range prefixes {
					routeExists := routes.HaveRoute(p, vniRouterIP)
					if shouldExist && !routeExists {
						return fmt.Errorf("%s prefix %s with nexthop %s not found in routes %v for pod %s", ipFamily, p, vniRouterIP, routes, frrk8s.Name)
					}
					if !shouldExist && routeExists {
						return fmt.Errorf("%s prefix %s with nexthop %s found in routes %v for pod %s", ipFamily, p, vniRouterIP, routes, frrk8s.Name)
					}
				}
				return nil
			}

			checkBGPPrefixesForVNI := func(frrk8s *corev1.Pod, vni v1alpha1.L3VNI, prefixes []string, shouldExist bool) {
				exec := executor.ForPod(frrk8s.Namespace, frrk8s.Name, "frr")
				Eventually(func() error {
					ipv4Routes, ipv6Routes, err := frr.BGPRoutesFor(exec)
					Expect(err).NotTo(HaveOccurred())

					ipv4Prefixes, ipv6Prefixes := separateIPFamilies(prefixes)

					if err := checkPrefixesForIPFamily(frrk8s, ipv4Prefixes, vni.Spec.HostSession.LocalCIDR.IPv4, "IPv4", shouldExist, ipv4Routes); err != nil {
						return err
					}

					if err := checkPrefixesForIPFamily(frrk8s, ipv6Prefixes, vni.Spec.HostSession.LocalCIDR.IPv6, "IPv6", shouldExist, ipv6Routes); err != nil {
						return err
					}

					return nil
				}, 4*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
			}

			By("advertising routes from the leaves for VRF Red - VNI 100")
			changeLeafPrefixes(infra.LeafAConfig, leafAVRFRedPrefixes, emptyPrefixes)
			changeLeafPrefixes(infra.LeafBConfig, leafBVRFRedPrefixes, emptyPrefixes)

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForVNI(frrk8s, vniRed, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForVNI(frrk8s, vniRed, leafBVRFRedPrefixes, ShouldExist)
			}

			By("advertising also routes from the leaves for VRF Blue - VNI 200")
			changeLeafPrefixes(infra.LeafAConfig, leafAVRFRedPrefixes, leafAVRFBluePrefixes)
			changeLeafPrefixes(infra.LeafBConfig, leafBVRFRedPrefixes, leafBVRFBluePrefixes)

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForVNI(frrk8s, vniRed, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForVNI(frrk8s, vniRed, leafBVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForVNI(frrk8s, vniBlue, leafAVRFBluePrefixes, ShouldExist)
				checkBGPPrefixesForVNI(frrk8s, vniBlue, leafBVRFBluePrefixes, ShouldExist)
			}

			By("removing routes from the leaves for VRF Blue - VNI 200")
			changeLeafPrefixes(infra.LeafAConfig, leafAVRFRedPrefixes, emptyPrefixes)
			changeLeafPrefixes(infra.LeafBConfig, leafBVRFRedPrefixes, emptyPrefixes)

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForVNI(frrk8s, vniRed, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForVNI(frrk8s, vniRed, leafBVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForVNI(frrk8s, vniBlue, leafAVRFBluePrefixes, !ShouldExist)
				checkBGPPrefixesForVNI(frrk8s, vniBlue, leafBVRFBluePrefixes, !ShouldExist)
			}
		})
	})

	Context("testing e2e integration between a pod and the blue / red hosts", func() {
		const testNamespace = "test-namespace"
		var testPod *corev1.Pod
		var podNode *corev1.Node

		BeforeAll(func() {
			By("setting redistribute connected on leaves")
			redistributeConnectedForLeaf(infra.LeafAConfig)
			redistributeConnectedForLeaf(infra.LeafBConfig)

			By("Creating the test namespace")
			_, err := k8s.CreateNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Creating the test pod")
			testPod, err = k8s.CreateAgnhostPod(cs, "test-pod", testNamespace)
			Expect(err).NotTo(HaveOccurred())

			podNode, err = cs.CoreV1().Nodes().Get(context.Background(), testPod.Spec.NodeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			nodeSelector := k8s.NodeSelectorForPod(testPod)

			advertisePodToVNI := func(pod *corev1.Pod, vni v1alpha1.L3VNI) []frrk8sapi.FRRConfiguration {
				res := []frrk8sapi.FRRConfiguration{}
				for _, podIP := range pod.Status.PodIPs {
					var cidrSuffix = "/32"
					ipFamily, err := ipfamily.ForAddresses(podIP.IP)
					Expect(err).NotTo(HaveOccurred())
					if ipFamily == ipfamily.IPv6 {
						cidrSuffix = "/128"
					}

					config, err := frrk8s.ConfigFromVNIForIPFamily(vni, ipFamily, frrk8s.WithNodeSelector(nodeSelector), frrk8s.AdvertisePrefixes(podIP.IP+cidrSuffix))
					Expect(err).NotTo(HaveOccurred())
					res = append(res, *config)
				}
				return res
			}

			By("Creating the frr-k8s configuration for the node where the test pod runs and advertising all pod ips")

			frrK8sConfigRedForPod := advertisePodToVNI(testPod, vniRed)
			frrK8sConfigBlueForPod := advertisePodToVNI(testPod, vniBlue)

			err = Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{
					vniRed,
					vniBlue,
				},
				FRRConfigurations: append(frrK8sConfigRedForPod, frrK8sConfigBlueForPod...),
			})
			Expect(err).NotTo(HaveOccurred())

			frrK8sPodOnNode, err := frrk8s.PodForNode(cs, testPod.Spec.NodeName)
			Expect(err).NotTo(HaveOccurred())
			validateFRRK8sSessionForVNI(vniRed, Established, frrK8sPodOnNode)
			validateFRRK8sSessionForVNI(vniBlue, Established, frrK8sPodOnNode)
		})

		AfterAll(func() {
			By("Deleting the test namespace")
			err := k8s.DeleteNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			err = Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			removeLeafPrefixes(infra.LeafAConfig)
			removeLeafPrefixes(infra.LeafBConfig)
		})

		AfterEach(func() {
			dumpIfFails(cs)
		})

		DescribeTable("should be able to reach the hosts from the test pod and vice versa", func(
			vni v1alpha1.L3VNI,
			hostName string,
			externalHostIP string,
			ipFamily ipfamily.Family,
		) {

			var localCIDR string
			localCIDR = vni.Spec.HostSession.LocalCIDR.IPv4

			if ipFamily == ipfamily.IPv6 {
				localCIDR = vni.Spec.HostSession.LocalCIDR.IPv6
			}
			hostSide, err := openperouter.HostIPFromCIDRForNode(localCIDR, podNode)
			Expect(err).NotTo(HaveOccurred())

			podIP, err := getPodIPByFamily(testPod, ipFamily)
			Expect(err).NotTo(HaveOccurred())

			podExecutor := executor.ForPod(testPod.Namespace, testPod.Name, "agnhost")
			externalHostExecutor := executor.ForContainer("clab-kind-" + hostName)

			Eventually(func() error {
				By(fmt.Sprintf("trying to hit hosts %s on the %s network", externalHostIP, vni.Name))
				urlStr := url.Format("http://%s:8090/clientip", externalHostIP)
				res, err := podExecutor.Exec("curl", "-sS", urlStr)
				if err != nil {
					return fmt.Errorf("curl %s:8090 failed: %s", externalHostIP, res)
				}
				fmt.Println("res", res)
				clientIP, err := extractClientIP(res)
				Expect(err).NotTo(HaveOccurred())

				if clientIP != hostSide {
					return fmt.Errorf("curl %s:8090 returned %s, expected %s", externalHostIP, clientIP, hostSide)
				}

				urlStr = url.Format("http://%s:8090/hostname", externalHostIP)
				res, err = podExecutor.Exec("curl", "-sS", urlStr)
				if err != nil {
					return fmt.Errorf("curl %s:8090 failed: %s", externalHostIP, res)
				}
				if res != hostName {
					return fmt.Errorf("curl %s:8090 returned %s, expected %s", externalHostIP, res, hostName)
				}

				By(fmt.Sprintf("trying to hit pod %s on the %s network from host %s", podIP, vni.Name, hostName))

				urlStr = url.Format("http://%s:8090/clientip", podIP)
				res, err = externalHostExecutor.Exec("curl", "-sS", urlStr)
				if err != nil {
					return fmt.Errorf("curl from %s to %s:8090 failed: %s", hostName, podIP, res)
				}
				hostClientIP, err := extractClientIP(res)
				Expect(err).NotTo(HaveOccurred())

				if hostClientIP != externalHostIP {
					return fmt.Errorf("curl from %s to %s:8090 returned %s, expected %s", hostName, podIP, clientIP, externalHostIP)
				}
				return nil
			}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		},
			Entry("vni red host A ipv4", vniRed, "hostA_red", infra.HostARedIPv4, ipfamily.IPv4),
			Entry("vni red host B ipv4", vniRed, "hostB_red", infra.HostBRedIPv4, ipfamily.IPv4),
			Entry("vni blue host A ipv4", vniBlue, "hostA_blue", infra.HostABlueIPv4, ipfamily.IPv4),
			Entry("vni blue host B ipv4", vniBlue, "hostB_blue", infra.HostBBlueIPv4, ipfamily.IPv4),
			Entry("vni red host A ipv6", vniRed, "hostA_red", infra.HostARedIPv6, ipfamily.IPv6),
			Entry("vni red host B ipv6", vniRed, "hostB_red", infra.HostBRedIPv6, ipfamily.IPv6),
			Entry("vni blue host A ipv6", vniBlue, "hostA_blue", infra.HostABlueIPv6, ipfamily.IPv6),
			Entry("vni blue host B ipv6", vniBlue, "hostB_blue", infra.HostBBlueIPv6, ipfamily.IPv6),
		)
	})
})

func getPodIPByFamily(pod *corev1.Pod, family ipfamily.Family) (string, error) {
	for _, podIP := range pod.Status.PodIPs {
		ip := net.ParseIP(podIP.IP)
		if ip == nil {
			continue
		}
		if ipfamily.ForAddress(ip) == family {
			return podIP.IP, nil
		}
	}
	return "", fmt.Errorf("no %s IP found for pod %s", family, pod.Name)
}

func extractClientIP(res string) (string, error) {
	res = strings.TrimSpace(res)

	if strings.HasPrefix(res, "[") {
		endBracket := strings.Index(res, "]")
		if endBracket != -1 {
			return res[1:endBracket], nil
		}
	}

	if strings.Contains(res, ":") {
		parts := strings.Split(res, ":")
		return parts[0], nil
	}

	return "", fmt.Errorf("invalid response format: no client IP found in response: %s", res)
}
