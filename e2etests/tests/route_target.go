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
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	leafAVRFRedV4Prefixes  = []string{"192.168.20.0/24"}
	leafAVRFRedV6Prefixes  = []string{"2001:db8:20::/64"}
	leafAVRFBlueV4Prefixes = []string{"192.168.21.0/24"}
	leafAVRFBlueV6Prefixes = []string{"2001:db8:21::/64"}
	leafBVRFRedV4Prefixes  = []string{"192.169.20.0/24"}
	leafBVRFRedV6Prefixes  = []string{"2001:db8:169:20::/64"}
	leafBVRFBlueV4Prefixes = []string{"192.169.21.0/24"}
	leafBVRFBlueV6Prefixes = []string{"2001:db8:169:21::/64"}
	redRouteTargets        = infra.RouteTargets{ImportRTs: []string{"65000:1000"}, ExportRTs: []string{"65000:1000"}}
	blueRouteTargets       = infra.RouteTargets{ImportRTs: []string{"65000:2000"}, ExportRTs: []string{"65000:2000"}}
)

var _ = Describe("Routes between bgp and the fabric", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

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
			VNI:       100,
			ExportRTs: redRouteTargets.ExportRTs,
			ImportRTs: redRouteTargets.ImportRTs,
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
			VNI:       200,
			ExportRTs: blueRouteTargets.ExportRTs,
			ImportRTs: blueRouteTargets.ImportRTs,
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
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	Context("with vnis_rt and frr-k8s", func() {
		ShouldExist := true
		frrk8sPods := []*corev1.Pod{}
		frrK8sConfigRed, err := frrk8s.ConfigFromHostSession(*vniRed.Spec.HostSession, vniRed.Name)
		if err != nil {
			panic(err)
		}
		frrK8sConfigBlue, err := frrk8s.ConfigFromHostSession(*vniBlue.Spec.HostSession, vniBlue.Name)
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

			validateFRRK8sSessionForHostSession(vniRed.Name, *vniRed.Spec.HostSession, Established, frrk8sPods...)
			validateFRRK8sSessionForHostSession(vniBlue.Name, *vniBlue.Spec.HostSession, Established, frrk8sPods...)
		})

		AfterEach(func() {
			dumpIfFails(cs)
			err := Updater.CleanButUnderlay()
			Expect(err).NotTo(HaveOccurred())
			Expect(infra.LeafAConfig.Configure(infra.EmptyLeafConfig)).To(Succeed())
			Expect(infra.LeafBConfig.Configure(infra.EmptyLeafConfig)).To(Succeed())
		})

		It("translates EVPN incoming routes as BGP routes", func() {
			By("advertising routes from the leaves for VRF Red - VNI 100")
			leafAConfig := infra.LeafConfiguration{
				Red: infra.Addresses{
					IPV4:         leafAVRFRedV4Prefixes,
					IPV6:         leafAVRFRedV6Prefixes,
					RouteTargets: redRouteTargets,
				},
			}
			leafBConfig := infra.LeafConfiguration{
				Red: infra.Addresses{
					IPV4:         leafBVRFRedV4Prefixes,
					IPV6:         leafBVRFRedV6Prefixes,
					RouteTargets: redRouteTargets,
				},
			}
			Expect(infra.LeafAConfig.Configure(leafAConfig)).To(Succeed())
			Expect(infra.LeafBConfig.Configure(leafBConfig)).To(Succeed())

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafBVRFRedPrefixes, ShouldExist)
			}

			By("advertising also routes from the leaves for VRF Blue - VNI 200")
			leafAConfig.Blue = infra.Addresses{
				IPV4:         leafAVRFBlueV4Prefixes,
				IPV6:         leafAVRFBlueV6Prefixes,
				RouteTargets: blueRouteTargets,
			}
			leafBConfig.Blue = infra.Addresses{
				IPV4:         leafBVRFBlueV4Prefixes,
				IPV6:         leafBVRFBlueV6Prefixes,
				RouteTargets: blueRouteTargets,
			}
			fmt.Printf("Leaf A config: %+v\n", leafAConfig)
			fmt.Printf("Leaf B config: %+v\n", leafBConfig)
			Expect(infra.LeafAConfig.Configure(leafAConfig)).To(Succeed())
			Expect(infra.LeafBConfig.Configure(leafBConfig)).To(Succeed())

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafBVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniBlue.Spec.HostSession, leafAVRFBluePrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniBlue.Spec.HostSession, leafBVRFBluePrefixes, ShouldExist)
			}

			By("removing routes from the leaves for VRF Blue - VNI 200")
			leafAConfig.Blue = infra.Addresses{}
			leafBConfig.Blue = infra.Addresses{}
			Expect(infra.LeafAConfig.Configure(leafAConfig)).To(Succeed())
			Expect(infra.LeafBConfig.Configure(leafBConfig)).To(Succeed())

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafBVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniBlue.Spec.HostSession, leafAVRFBluePrefixes, !ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniBlue.Spec.HostSession, leafBVRFBluePrefixes, !ShouldExist)
			}
		})
	})
})
