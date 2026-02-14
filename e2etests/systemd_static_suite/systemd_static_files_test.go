// SPDX-License-Identifier:Apache-2.0

package systemd_static

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// These tests assume that the cluster was started with the static files available under
// the local testdata dir.
var _ = Describe("Static configuration", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	// Used as a reference, same that we have under testdata
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
					IPv4: "192.170.10.0/24",
					IPv6: "2001:db9:1::/64",
				},
			},
			VNI: 100,
		},
	}

	BeforeAll(func() {
		cs = k8sclient.New()
		var err error
		routers, err = openperouter.Get(cs, true) // hostmode = true
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(GinkgoWriter)
	})

	Context("with vnis", func() {
		AfterEach(func() {
			Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
			Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
		})

		It("receives type 5 routes from the fabric", func() {
			emptyPrefixes := []string{}
			emptyRouteTargets := infra.RouteTargets{}
			leafAVRFRedPrefixes := []string{"192.168.20.0/24", "2001:db8:20::/64"}

			By("announcing type 5 routes on VNI 100 from leafA")
			Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, leafAVRFRedPrefixes, emptyPrefixes, emptyRouteTargets, emptyRouteTargets)).To(Succeed())
			checkRouteFromLeaf(infra.LeafAConfig, routers, vniRed, mustContain, leafAVRFRedPrefixes)

			By("removing a route from leafA on vni 100")
			Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes, emptyRouteTargets, emptyRouteTargets)).To(Succeed())
		})
	})
	// TODO Create vni blue with the api server
})
