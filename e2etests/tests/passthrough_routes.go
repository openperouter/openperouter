// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	frrk8sapi "github.com/metallb/frr-k8s/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
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
	leafADefaultPrefixes = []string{"192.170.20.0/24"}
	leafBDefaultPrefixes = []string{"192.170.21.0/24"}
)

var _ = Describe("Routes between bgp and the fabric", Label("passthrough"), Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

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

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(GinkgoWriter)

		if SkipUnderlayPassthrough {
			return
		}
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if SkipUnderlayPassthrough {
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			return
		}

		err := Updater.CleanAll()
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
			Expect(infra.LeafAConfig.ChangePrefixes(leafADefaultPrefixes, emptyPrefixes, emptyPrefixes, emptyRouteTargets, emptyRouteTargets)).To(Succeed())
			Expect(infra.LeafBConfig.ChangePrefixes(leafBDefaultPrefixes, emptyPrefixes, emptyPrefixes, emptyRouteTargets, emptyRouteTargets)).To(Succeed())

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafADefaultPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafBDefaultPrefixes, ShouldExist)
			}

			By("removing routes from the leaf B")
			Expect(infra.LeafAConfig.ChangePrefixes(leafADefaultPrefixes, emptyPrefixes, emptyPrefixes, emptyRouteTargets, emptyRouteTargets)).To(Succeed())
			Expect(infra.LeafBConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes, emptyRouteTargets, emptyRouteTargets)).To(Succeed())

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafADefaultPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, passthrough.Spec.HostSession, leafBDefaultPrefixes, !ShouldExist)
			}
		})
	})

	Context("testing e2e integration between a pod the red default host", func() {
		const testNamespace = "test-namespace"
		var testPod *corev1.Pod
		var podNode *corev1.Node

		BeforeAll(func() {
			By("setting redistribute connected on leaves")
			redistributeConnectedForLeaf(infra.LeafAConfig, emptyRouteTargets, emptyRouteTargets)
			redistributeConnectedForLeaf(infra.LeafBConfig, emptyRouteTargets, emptyRouteTargets)

			By("Creating the test namespace")
			_, err := k8s.CreateNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			By("Creating the test pod")
			testPod, err = k8s.CreateAgnhostPod(cs, "test-pod", testNamespace)
			Expect(err).NotTo(HaveOccurred())

			podNode, err = cs.CoreV1().Nodes().Get(context.Background(), testPod.Spec.NodeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			nodeSelector := k8s.NodeSelectorForPod(testPod)

			advertisePodToPassthrough := func(pod *corev1.Pod, passthrough v1alpha1.L3Passthrough) []frrk8sapi.FRRConfiguration {
				res := []frrk8sapi.FRRConfiguration{}
				for _, podIP := range pod.Status.PodIPs {
					var cidrSuffix = "/32"
					ipFamily, err := ipfamily.ForAddresses(podIP.IP)
					Expect(err).NotTo(HaveOccurred())
					if ipFamily == ipfamily.IPv6 {
						cidrSuffix = "/128"
					}

					config, err := frrk8s.ConfigFromHostSessionForIPFamily(passthrough.Spec.HostSession, passthrough.Name, ipFamily, frrk8s.WithNodeSelector(nodeSelector), frrk8s.AdvertisePrefixes(podIP.IP+cidrSuffix))
					Expect(err).NotTo(HaveOccurred())
					res = append(res, *config)
				}
				return res
			}

			By("Creating the frr-k8s configuration for the node where the test pod runs and advertising all pod ips")

			frrk8sForPassthrough := advertisePodToPassthrough(testPod, passthrough)

			err = Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{
					passthrough,
				},
				FRRConfigurations: frrk8sForPassthrough,
			})
			Expect(err).NotTo(HaveOccurred())

			frrK8sPodOnNode, err := frrk8s.PodForNode(cs, testPod.Spec.NodeName)
			Expect(err).NotTo(HaveOccurred())
			validateFRRK8sSessionForHostSession(passthrough.Name, passthrough.Spec.HostSession, Established, frrK8sPodOnNode)
		})

		AfterAll(func() {
			By("Deleting the test namespace")
			err := k8s.DeleteNamespace(cs, testNamespace)
			Expect(err).NotTo(HaveOccurred())

			err = Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
			Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
		})

		AfterEach(func() {
			dumpIfFails(cs)
		})

		It("host and the pod from each other with the expected ips", func() {
			hostSide, err := openperouter.HostIPFromCIDRForNode(passthrough.Spec.HostSession.LocalCIDR.IPv4, podNode)
			Expect(err).NotTo(HaveOccurred())

			podIP, err := getPodIPByFamily(testPod, ipfamily.IPv4)
			Expect(err).NotTo(HaveOccurred())

			podExecutor := executor.ForPod(testPod.Namespace, testPod.Name, "agnhost")
			externalHostExecutor := executor.ForContainer("clab-kind-hostA_default")

			Eventually(func() error {
				externalHostIP := infra.HostADefaultIPv4
				hostName := "hostA_default"
				By(fmt.Sprintf("trying to hit hosts %s", externalHostIP))
				urlStr := url.Format("http://%s:8090/clientip", externalHostIP)
				res, err := podExecutor.Exec("curl", "-sS", urlStr)
				if err != nil {
					return fmt.Errorf("curl %s:8090 failed: %s", externalHostIP, res)
				}
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

				By(fmt.Sprintf("trying to hit pod %s on the passthrough network from host %s", podIP, hostName))

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
		})
	})

})
