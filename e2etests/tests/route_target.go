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
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var (
	leafAVRFRedV4Prefixes, leafAVRFRedV6Prefixes   = infra.SeparateIPFamilies(leafAVRFRedPrefixes)
	leafAVRFBlueV4Prefixes, leafAVRFBlueV6Prefixes = infra.SeparateIPFamilies(leafAVRFBluePrefixes)
	leafBVRFRedV4Prefixes, leafBVRFRedV6Prefixes   = infra.SeparateIPFamilies(leafBVRFRedPrefixes)
	leafBVRFBlueV4Prefixes, leafBVRFBlueV6Prefixes = infra.SeparateIPFamilies(leafBVRFBluePrefixes)
	redRouteTargets                                = infra.RouteTargets{ImportRTs: []string{"65000:1000"}, ExportRTs: []string{"65000:1000"}}
	blueRouteTargets                               = infra.RouteTargets{ImportRTs: []string{"65000:2000"}, ExportRTs: []string{"65000:2000"}}

	frrk8sRedPrefixes  = []string{"10.100.0.0/24"}
	frrk8sBluePrefixes = []string{"10.200.0.0/24"}
)

var _ = Describe("Routes with RT between bgp and the fabric", Ordered, func() {
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
				HostASN: ptr.To(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: ptr.To("192.169.10.0/24"),
					IPv6: ptr.To("2001:db8:1::/64"),
				},
			},
			VNI:       ptr.To(int64(100)),
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
				HostASN: ptr.To(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: ptr.To("192.169.11.0/24"),
					IPv6: ptr.To("2001:db8:2::/64"),
				},
			},
			VNI:       ptr.To(int64(200)),
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
		frrK8sConfigRed, err := frrk8s.ConfigFromHostSession(*vniRed.Spec.HostSession, vniRed.Name,
			frrk8s.AdvertisePrefixes(frrk8sRedPrefixes...))
		if err != nil {
			panic(err)
		}
		frrK8sConfigBlue, err := frrk8s.ConfigFromHostSession(*vniBlue.Spec.HostSession, vniBlue.Name,
			frrk8s.AdvertisePrefixes(frrk8sBluePrefixes...))
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
			Contains := true
			checkRouteAndRTsFromLeaf := func(leaf infra.Leaf, vni v1alpha1.L3VNI, mustContain bool, prefixes []string, routeTargets []string) {
				By(fmt.Sprintf("checking routes from leaf %s on vni %s, mustContain %v %v", leaf.Name, vni.Name, mustContain, prefixes))
				Eventually(func() error {
					for exec := range routers.GetExecutors() {
						evpn, err := frr.EVPNInfo(exec)
						if err != nil {
							return fmt.Errorf("failed to get EVPN info from %s: %w", exec.Name(), err)
						}
						for _, prefix := range prefixes {
							if mustContain && !evpn.ContainsType5RouteWithRT(prefix, leaf.VTEPIP, int(ptr.Deref(vni.Spec.VNI, 0)), routeTargets) {
								return fmt.Errorf("type5 route for %s - %s not found in %v in router %s", prefix, leaf.VTEPIP, evpn, exec.Name())
							}
							if !mustContain && evpn.ContainsType5RouteWithRT(prefix, leaf.VTEPIP, int(ptr.Deref(vni.Spec.VNI, 0)), routeTargets) {
								return fmt.Errorf("type5 route for %s - %s found in %v in router %s", prefix, leaf.VTEPIP, evpn, exec.Name())
							}
						}
					}
					return nil
				}, 3*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
			}

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

			By("checking routes are propagated as EVPN type 5 routes with correct route-targets")
			checkRouteAndRTsFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes, vniRed.Spec.ImportRTs)
			checkRouteAndRTsFromLeaf(infra.LeafAConfig, vniBlue, !Contains, leafAVRFBluePrefixes, vniBlue.Spec.ImportRTs)
			checkRouteAndRTsFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes, vniRed.Spec.ImportRTs)
			checkRouteAndRTsFromLeaf(infra.LeafBConfig, vniBlue, !Contains, leafBVRFBluePrefixes, vniBlue.Spec.ImportRTs)

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
			Expect(infra.LeafAConfig.Configure(leafAConfig)).To(Succeed())
			Expect(infra.LeafBConfig.Configure(leafBConfig)).To(Succeed())

			By("checking routes are propagated as EVPN type 5 routes with correct route-targets")
			checkRouteAndRTsFromLeaf(infra.LeafAConfig, vniRed, Contains, leafAVRFRedPrefixes, vniRed.Spec.ImportRTs)
			checkRouteAndRTsFromLeaf(infra.LeafAConfig, vniBlue, Contains, leafAVRFBluePrefixes, vniBlue.Spec.ImportRTs)
			checkRouteAndRTsFromLeaf(infra.LeafBConfig, vniRed, Contains, leafBVRFRedPrefixes, vniRed.Spec.ImportRTs)
			checkRouteAndRTsFromLeaf(infra.LeafBConfig, vniBlue, Contains, leafBVRFBluePrefixes, vniBlue.Spec.ImportRTs)

			By("checking routes are propagated via BGP")

			for _, frrk8s := range frrk8sPods {
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafAVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniRed.Spec.HostSession, leafBVRFRedPrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniBlue.Spec.HostSession, leafAVRFBluePrefixes, ShouldExist)
				checkBGPPrefixesForHostSession(frrk8s, *vniBlue.Spec.HostSession, leafBVRFBluePrefixes, ShouldExist)
			}

			By("checking routes advertised by frrk8s are visible on leaves with correct ExportRTs")
			checkPrefixWithRTOnLeaf := func(leaf infra.Leaf, prefixes []string, exportRTs []string) {
				By(fmt.Sprintf("checking frrk8s-originated prefixes on leaf %s %v", leaf.Name, prefixes))
				exec := executor.ForContainer(leaf.Name)
				Eventually(func() error {
					evpn, err := frr.EVPNInfo(exec)
					if err != nil {
						return fmt.Errorf("failed to get EVPN info from leaf %s: %w", leaf.Name, err)
					}
					for _, prefix := range prefixes {
						if !evpn.ContainsType5PrefixWithRT(prefix, exportRTs) {
							return fmt.Errorf("prefix %s with ExportRTs %v not found on leaf %s in %v", prefix, exportRTs, leaf.Name, evpn)
						}
					}
					return nil
				}, 3*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
			}

			checkPrefixWithRTOnLeaf(infra.LeafAConfig, frrk8sRedPrefixes, vniRed.Spec.ExportRTs)
			checkPrefixWithRTOnLeaf(infra.LeafAConfig, frrk8sBluePrefixes, vniBlue.Spec.ExportRTs)
			checkPrefixWithRTOnLeaf(infra.LeafBConfig, frrk8sRedPrefixes, vniRed.Spec.ExportRTs)
			checkPrefixWithRTOnLeaf(infra.LeafBConfig, frrk8sBluePrefixes, vniBlue.Spec.ExportRTs)

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
