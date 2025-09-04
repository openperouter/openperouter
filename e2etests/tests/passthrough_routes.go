// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	// NOTE: we can't advertise any ip via EVPN from the leaves, they
	// must be reacheable otherwise FRR will skip them.
	leafADefaultPrefixes = []string{"192.170.20.0/24"}
	leafBDefaultPrefixes = []string{"192.170.21.0/24"}
)

var _ = Describe("Routes between bgp and the fabric", Ordered, func() {
	var cs clientset.Interface
	routerPods := []*corev1.Pod{}

	passhtrough := v1alpha1.L3Passthrough{
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

	Context("with passthrough and frr-k8s", func() {
		ShouldExist := true
		frrk8sPods := []*corev1.Pod{}
		frrK8sConfigRed, err := frrk8s.ConfigFromHostSession(passhtrough.Spec.HostSession, passhtrough.Name)
		if err != nil {
			panic(err)
		}
		frrK8sConfigBlue, err := frrk8s.ConfigFromHostSession(passhtrough.Spec.HostSession, passhtrough.Name)
		if err != nil {
			panic(err)
		}

		BeforeEach(func() {
			frrk8sPods, err = frrk8s.Pods(cs)
			Expect(err).NotTo(HaveOccurred())

			DumpPods("FRRK8s pods", frrk8sPods)

			err = Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{
					passhtrough,
				},
				FRRConfigurations: append(frrK8sConfigRed, frrK8sConfigBlue...),
			})
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForHostSession(passhtrough.Name, passhtrough.Spec.HostSession, Established, frrk8sPods...)
		})

		AfterEach(func() {
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			removeLeafPrefixes(infra.LeafAConfig)
			removeLeafPrefixes(infra.LeafBConfig)
		})

		It("translates BGP incoming routes as BGP routes", func() {

			By("advertising routes from both leaves")
			changeLeafPrefixes(infra.LeafAConfig, leafADefaultPrefixes, emptyPrefixes, emptyPrefixes)
			changeLeafPrefixes(infra.LeafBConfig, leafBDefaultPrefixes, emptyPrefixes, emptyPrefixes)

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, passhtrough.Spec.HostSession, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, passhtrough.Spec.HostSession, leafBVRFRedPrefixes, ShouldExist)
			}

			By("removing routes from the leaf B")
			changeLeafPrefixes(infra.LeafAConfig, leafADefaultPrefixes, emptyPrefixes, emptyPrefixes)
			changeLeafPrefixes(infra.LeafBConfig, emptyPrefixes, emptyPrefixes, emptyPrefixes)

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, passhtrough.Spec.HostSession, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, passhtrough.Spec.HostSession, leafBVRFRedPrefixes, !ShouldExist)
			}
		})
	})
	/*
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

						config, err := frrk8s.ConfigFromHostSessionForIPFamily(*vni.Spec.HostSession, vni.Name, ipFamily, frrk8s.WithNodeSelector(nodeSelector), frrk8s.AdvertisePrefixes(podIP.IP+cidrSuffix))
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
				validateFRRK8sSessionForHostSession(vniRed.Name, *vniRed.Spec.HostSession, Established, frrK8sPodOnNode)
				validateFRRK8sSessionForHostSession(vniBlue.Name, *vniBlue.Spec.HostSession, Established, frrK8sPodOnNode)
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
	*/
})
