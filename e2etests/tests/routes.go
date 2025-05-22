// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"strings"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var (
	// NOTE: we can't advertise any ip via EVPN from the leaves, they
	// must be reacheable otherwise FRR will skip them.
	leafAVRFRedPrefixes  = []string{"192.168.20.0/24"}
	leafAVRFBluePrefixes = []string{"192.168.21.0/24"}
	leafBVRFRedPrefixes  = []string{"192.169.20.0/24"}
	leafBVRFBluePrefixes = []string{"192.169.21.0/24"}
	emptyPrefixes        = []string{}
)

var _ = Describe("Routes between bgp and the fabric", Ordered, func() {
	var cs clientset.Interface
	routerPods := []*corev1.Pod{}

	vniRed := v1alpha1.VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.VNISpec{
			ASN:       64514,
			VNI:       100,
			LocalCIDR: "192.169.10.0/24",
			HostASN:   ptr.To(uint32(64515)),
		},
	}

	vniBlue := v1alpha1.VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blue",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.VNISpec{
			ASN:       64514,
			VNI:       200,
			LocalCIDR: "192.169.11.0/24",
			HostASN:   ptr.To(uint32(64515)),
		},
	}

	changeLeafPrefixes := func(leaf infra.Leaf, redPrefixes, bluePrefixes []string) {
		leafConfiguration := infra.LeafConfiguration{
			Leaf: leaf,
			Red: infra.Addresses{
				IPV4: redPrefixes,
			},
			Blue: infra.Addresses{
				IPV4: bluePrefixes,
			},
		}
		config, err := infra.LeafConfigToFRR(leafConfiguration)
		Expect(err).NotTo(HaveOccurred())
		err = leaf.ReloadConfig(config)
		Expect(err).NotTo(HaveOccurred())
	}

	removeLeafPrefixes := func(leaf infra.Leaf) {
		changeLeafPrefixes(leaf, []string{}, []string{})
	}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routerPods, err = openperouter.RouterPods(cs)
		Expect(err).NotTo(HaveOccurred())

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

	BeforeEach(func() {
		removeLeafPrefixes(infra.LeafAConfig)
		removeLeafPrefixes(infra.LeafBConfig)
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
		removeLeafPrefixes(infra.LeafAConfig)
		removeLeafPrefixes(infra.LeafBConfig)
	})

	Context("with vnis", func() {
		BeforeEach(func() {
			err := Updater.Update(config.Resources{
				VNIs: []v1alpha1.VNI{
					vniRed,
					vniBlue,
				},
			})
			Expect(err).NotTo(HaveOccurred())

		})

		It("receives type 5 routes from the fabric", func() {
			Contains := true
			checkRouteFromLeaf := func(leaf infra.Leaf, vni v1alpha1.VNI, mustContain bool, prefixes []string) {
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
			err := Updater.Update(config.Resources{
				VNIs: []v1alpha1.VNI{
					vniRed,
					vniBlue,
				},
				FRRConfigurations: []frrk8sv1beta1.FRRConfiguration{
					frrK8sConfigRed,
					frrK8sConfigBlue,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForVNI(vniRed, Established, frrk8sPods...)
			validateFRRK8sSessionForVNI(vniBlue, Established, frrk8sPods...)
		})

		It("translates EVPN incoming routes as BGP routes", func() {
			checkBGPPrefixesForVNI := func(frrk8s *corev1.Pod, vni v1alpha1.VNI, prefixes []string, shouldExist bool) {
				exec := executor.ForPod(frrk8s.Namespace, frrk8s.Name, "frr")
				Eventually(func() error {
					routes, err := frr.BGPRoutesFor(exec)
					Expect(err).NotTo(HaveOccurred())

					vniRouterIP, err := openperouter.RouterIPFromCIDR(vni.Spec.LocalCIDR)
					Expect(err).NotTo(HaveOccurred())

					for _, p := range prefixes {
						routeExists := routes.HaveRoute(p, vniRouterIP)
						if shouldExist && !routeExists {
							return fmt.Errorf("prefix %s with nexthop %s not found in routes %v for pod %s", p, vniRouterIP, routes, frrk8s.Name)
						}
						if !shouldExist && routeExists {
							return fmt.Errorf("prefix %s with nexthop %s found in routes %v for pod %s", p, vniRouterIP, routes, frrk8s.Name)
						}
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

		redistributeConnectedForLeaf := func(leaf infra.Leaf) {
			leafConfiguration := infra.LeafConfiguration{
				Leaf: leaf,
				Red: infra.Addresses{
					RedistributeConnected: true,
				},
				Blue: infra.Addresses{
					RedistributeConnected: true,
				},
			}
			config, err := infra.LeafConfigToFRR(leafConfiguration)
			Expect(err).NotTo(HaveOccurred())
			err = leaf.ReloadConfig(config)
			Expect(err).NotTo(HaveOccurred())
		}

		BeforeEach(func() {
			By("setting redistribute connected on leaves")
			redistributeConnectedForLeaf(infra.LeafAConfig)
			redistributeConnectedForLeaf(infra.LeafBConfig)

			By("Creating the test namespace")
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
			_, err := cs.CoreV1().Namespaces().Create(context.Background(), &ns, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				_, err := cs.CoreV1().Namespaces().Get(context.Background(), testNamespace, metav1.GetOptions{})
				return err
			}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

			By("Creating the test pod")
			testPod, err = k8s.CreateAgnhostPod(cs, "test-pod", testNamespace)
			Expect(err).NotTo(HaveOccurred())
			testPodName := testPod.Name

			By("Waiting for the test pod to be ready")
			Eventually(func() error {
				var err error
				testPod, err = cs.CoreV1().Pods(testNamespace).Get(context.Background(), testPodName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if !k8s.PodIsReady(testPod) {
					return fmt.Errorf("pod %s is not ready", testPod.Name)
				}
				return nil
			}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

			podNode, err = cs.CoreV1().Nodes().Get(context.Background(), testPod.Spec.NodeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			nodeSelector := k8s.NodeSelectorForPod(testPod)

			By("Creating the frr-k8s configuration for the node where the test pod runs and advertising the pod ip")
			frrK8sConfigRedForPod, err := frrk8s.ConfigFromVNI(vniRed, frrk8s.WithNodeSelector(nodeSelector), frrk8s.AdvertisePrefixes(testPod.Status.PodIP+"/32"))
			Expect(err).NotTo(HaveOccurred())
			frrK8sConfigBlueForPod, err := frrk8s.ConfigFromVNI(vniBlue, frrk8s.WithNodeSelector(nodeSelector), frrk8s.AdvertisePrefixes(testPod.Status.PodIP+"/32"))
			Expect(err).NotTo(HaveOccurred())

			err = Updater.Update(config.Resources{
				VNIs: []v1alpha1.VNI{
					vniRed,
					vniBlue,
				},
				FRRConfigurations: []frrk8sv1beta1.FRRConfiguration{
					frrK8sConfigRedForPod,
					frrK8sConfigBlueForPod,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			frrK8sPodOnNode, err := frrk8s.PodForNode(cs, testPod.Spec.NodeName)
			validateFRRK8sSessionForVNI(vniRed, Established, frrK8sPodOnNode)
			validateFRRK8sSessionForVNI(vniBlue, Established, frrK8sPodOnNode)

		})

		AfterEach(func() {
			By("Deleting the test namespace")
			err := cs.CoreV1().Namespaces().Delete(context.Background(), testNamespace, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				_, err := cs.CoreV1().Namespaces().Get(context.Background(), testNamespace, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, time.Minute, time.Second).Should(BeTrue())
		})

		It("should be able to reach the hosts from the test pod and vice versa", func() {
			tests := []struct {
				vni            v1alpha1.VNI
				hostName       string
				externalHostIP string
			}{
				{vniRed, "hostA_red", infra.HostARedIP},
				{vniRed, "hostB_red", infra.HostBRedIP},
				{vniBlue, "hostA_blue", infra.HostABlueIP},
				{vniBlue, "hostB_blue", infra.HostBBlueIP},
			}
			for _, test := range tests {
				hostSide, err := openperouter.HostIPFromCIDRForNode(test.vni.Spec.LocalCIDR, podNode)
				Expect(err).NotTo(HaveOccurred())

				exec := executor.ForPod(testPod.Namespace, testPod.Name, "agnhost")
				externalHostExecutor := executor.ForContainer("clab-kind-" + test.hostName)

				Eventually(func() error {
					By(fmt.Sprintf("trying to hit hosts %s on the %s network", test.externalHostIP, test.vni.Name))
					url := fmt.Sprintf("http://%s:8090/clientip", test.externalHostIP)
					res, err := exec.Exec("curl", "-sS", url)
					if err != nil {
						return fmt.Errorf("curl %s:8090 failed: %s", test.externalHostIP, res)
					}
					clientIP := strings.Split(res, ":")[0]
					if clientIP != hostSide {
						return fmt.Errorf("curl %s:8090 returned %s, expected %s", test.externalHostIP, clientIP, hostSide)
					}

					url = fmt.Sprintf("http://%s:8090/hostname", test.externalHostIP)
					res, err = exec.Exec("curl", "-sS", url)
					if err != nil {
						return fmt.Errorf("curl %s:8090 failed: %s", test.externalHostIP, res)
					}
					if res != test.hostName {
						return fmt.Errorf("curl %s:8090 returned %s, expected %s", test.externalHostIP, res, test.hostName)
					}
					res, err = externalHostExecutor.Exec("curl", "-sS", fmt.Sprintf("http://%s:8090/clientip", testPod.Status.PodIP))
					if err != nil {
						return fmt.Errorf("curl from %s to %s:8090 failed: %s", test.hostName, testPod.Status.PodIP, res)
					}
					hostClientIP := strings.Split(res, ":")[0]
					if hostClientIP != test.externalHostIP {
						return fmt.Errorf("curl from %s to %s:8090 returned %s, expected %s", test.hostName, testPod.Status.PodIP, clientIP, test.externalHostIP)
					}
					return nil
				}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
			}
		})
	})
})
