// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("IS-IS routes", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	BeforeAll(func() {
		By("Cleaning all resources")
		Expect(Updater.CleanAll()).To(Succeed())
		By("Waiting for all underlays to be deleted")
		Eventually(Updater.IsUnderlayClean).ProbeEvery(5 * time.Second).WithTimeout(120 * time.Second).Should(Succeed())

		cs = k8sclient.New()

		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(slices.Collect(routers.GetExecutors()))).To(BeNumerically(">=", 2))
		routers.Dump(GinkgoWriter)
	})

	Context("point-to-point links", func() {
		const tunnelEndpointCIDRv6 = "2001:db8:1234:5678::/64"
		var nodes []corev1.Node

		BeforeAll(func() {
			nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			nodes = nodesItems.Items
		})

		BeforeEach(func() {
			By("cleaning all resources")
			Expect(Updater.CleanAll()).To(Succeed())
			Eventually(Updater.IsUnderlayClean).ProbeEvery(5 * time.Second).WithTimeout(120 * time.Second).Should(Succeed())

			By("configuring leafkind1 with IS-IS point-to-point on the node links")
			Expect(infra.LeafKind1Config.EnableISISPointToPoint(nodes)).To(Succeed())
		})

		AfterEach(func() {
			dumpIfFails(cs)
			By("cleaning all resources")
			Expect(Updater.CleanAll()).To(Succeed())
			Eventually(Updater.IsUnderlayClean).ProbeEvery(5 * time.Second).WithTimeout(120 * time.Second).Should(Succeed())

			By("restoring the leafkind1 configuration")
			Expect(infra.LeafKind1Config.DisableISISPointToPoint(nodes)).To(Succeed())
		})

		It("exchanges routes only when both ends run point-to-point", func() {
			_, routeCIDR, err := net.ParseCIDR(tunnelEndpointCIDRv6)
			Expect(err).NotTo(HaveOccurred())
			routersCount := len(slices.Collect(routers.GetExecutors()))

			By("enabling point-to-point on the node links")
			p2pUnderlay := infra.Underlay(infra.ISISOnly)
			p2pUnderlay.Spec.Interfaces = []v1alpha1.UnderlayInterface{
				{
					Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
					NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toleafkind1"},
				},
			}
			p2pUnderlay.Spec.ISIS.Interfaces = []v1alpha1.ISISInterface{
				{
					Name:     "toleafkind1",
					IPFamily: new(v1alpha1.IPFamilyIPv6),
					Features: []v1alpha1.ISISInterfaceFeature{"point-to-point"},
				},
			}

			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{p2pUnderlay},
			})).To(Succeed())

			By("checking that each router learns the other tunnel endpoints through leafkind1")
			Eventually(isisRoutesPerRouter).WithArguments(routers, routeCIDR).
				WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).
				Should(HaveEach(HaveLen(routersCount - 1)))

			By("disabling point-to-point on the node links")
			p2pUnderlay.Spec.ISIS.Interfaces[0].Features = nil
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{p2pUnderlay},
			})).To(Succeed())

			By("checking that the circuit type mismatch with leafkind1 withdraws the routes")
			Eventually(isisRoutesPerRouter).WithArguments(routers, routeCIDR).
				WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).
				Should(HaveEach(BeEmpty()))
		})
	})

	Context("topology tests", func() {
		const (
			tunnelEndpointCIDRv4 = "100.65.0.0/24"
			tunnelEndpointCIDRv6 = "2001:db8:1234:5678::/64"
		)

		var (
			routeCIDRs map[ipfamily.Family]*net.IPNet
		)

		BeforeEach(func() {
			var err error
			routeCIDRs = map[ipfamily.Family]*net.IPNet{}
			_, routeCIDRs[ipfamily.IPv4], err = net.ParseCIDR(tunnelEndpointCIDRv4)
			Expect(err).NotTo(HaveOccurred())
			_, routeCIDRs[ipfamily.IPv6], err = net.ParseCIDR(tunnelEndpointCIDRv6)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			dumpIfFails(cs)
			By("Cleaning all resources")
			Expect(Updater.CleanAll()).To(Succeed())
			By("Waiting for all underlays to be deleted")
			Eventually(Updater.IsUnderlayClean).ProbeEvery(5 * time.Second).WithTimeout(120 * time.Second).Should(Succeed())
		})

		DescribeTable("advertises correct topology LSPs", func(multiTopology bool) {
			underlay := infra.Underlay(infra.ISISOnly)
			if multiTopology {
				underlay.Spec.ISIS.Features = []v1alpha1.ISISFeature{
					v1alpha1.MultiTopology,
				}
			}
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
			})).To(Succeed())

			expectedConnectedCount := 1
			expectedISISCount := len(slices.Collect(routers.GetExecutors())) - expectedConnectedCount

			By("checking that the route table contains the expected number of routes for each tunnel endpoint")
			Eventually(func() error {
				for exec := range routers.GetExecutors() {
					for _, af := range []ipfamily.Family{ipfamily.IPv4, ipfamily.IPv6} {
						if err := checkRoutesCountInCIDR(exec, routeCIDRs[af], frr.Connected, af, expectedConnectedCount); err != nil {
							return fmt.Errorf("unexpected state of connected routes on router %s (af: %s), err: %w",
								exec.Name(), af, err)
						}
						if err := checkRoutesCountInCIDR(exec, routeCIDRs[af], frr.ISIS, af, expectedISISCount); err != nil {
							return fmt.Errorf("unexpected state of IS-IS routes on router %s (af: %s), err: %w",
								exec.Name(), af, err)
						}
					}
				}
				return nil
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).ShouldNot(HaveOccurred())

			expectedISISCount = len(slices.Collect(routers.GetExecutors()))

			By("checking that the IS-IS database contains the expected number of entries matching the desired topology behavior")
			Eventually(func() error {
				for exec := range routers.GetExecutors() {
					expectedTopologies := map[ipfamily.Family]bool{
						ipfamily.IPv4: true,          // IPv4 always has extIPReach entries with mtId == "Extended".
						ipfamily.IPv6: multiTopology, // IPv6 multi topology is the variable factor that we're interested in.
					}
					expectedCounts := map[ipfamily.Family]int{
						ipfamily.IPv4: expectedISISCount,
						ipfamily.IPv6: expectedISISCount,
					}
					if err := checkISISDatabase(exec, routeCIDRs, expectedTopologies, expectedCounts); err != nil {
						return fmt.Errorf("unexpected state of IS-IS database on router %s, err: %w", exec.Name(), err)
					}
				}
				return nil
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).ShouldNot(HaveOccurred())
		},
			Entry("multi-topology", true),
			Entry("single-topology", false),
		)
	})
})

// isisRoutesPerRouter returns, for each router, the IS-IS IPv6 host routes
// within routeCIDR.
func isisRoutesPerRouter(routers openperouter.Routers, routeCIDR *net.IPNet) (map[string][]string, error) {
	res := map[string][]string{}
	for exec := range routers.GetExecutors() {
		prefixes, err := routesInCIDR(exec, routeCIDR, frr.ISIS, ipfamily.IPv6)
		if err != nil {
			return nil, fmt.Errorf("failed to get routes on router %s: %w", exec.Name(), err)
		}
		res[exec.Name()] = prefixes
	}
	return res, nil
}

// checkRoutesCountInCIDR returns an error if the number of routes returned by routesInCIDR
// doesn't match expectedCount.
func checkRoutesCountInCIDR(
	exec openperouter.RouterExecutor,
	routeCIDR *net.IPNet,
	protocol frr.RouteProtocol,
	af ipfamily.Family,
	expectedCount int,
) error {
	prefixes, err := routesInCIDR(exec, routeCIDR, protocol, af)
	if err != nil {
		return err
	}
	if len(prefixes) != expectedCount {
		return fmt.Errorf("expected count is %d but got %d routes, routes: %v",
			expectedCount, len(prefixes), prefixes)
	}
	return nil
}

// routesInCIDR returns the prefixes of selected, dest-selected host routes (/32 for IPv4, /128 for IPv6)
// that fall within routeCIDR.
func routesInCIDR(
	exec openperouter.RouterExecutor,
	routeCIDR *net.IPNet,
	protocol frr.RouteProtocol,
	af ipfamily.Family,
) ([]string, error) {
	prefixes := []string{}

	routes, err := frr.GetRoutes(exec, protocol, af)
	if err != nil {
		return nil, err
	}
	for _, route := range routes {
		for _, routeEntry := range route {
			if !routeEntry.Selected {
				continue
			}
			if !routeEntry.DestSelected {
				continue
			}
			searchForPrefixLen := 32
			if af == ipfamily.IPv6 {
				searchForPrefixLen = 128
			}
			if routeEntry.PrefixLen != searchForPrefixLen {
				continue
			}
			ip, _, err := net.ParseCIDR(routeEntry.Prefix)
			if err != nil {
				return nil, err
			}
			if !routeCIDR.Contains(ip) {
				continue
			}
			prefixes = append(prefixes, routeEntry.Prefix)
		}
	}
	return prefixes, nil
}

// checkISISDatabase counts IS-IS database reachability entries that are host routes (/32 for extIPReach,
// /128 for ipv6Reach) within the given routeCIDRs and matching the expected multi-topology setting,
// then returns an error if any address family's count doesn't match expectedCounts.
func checkISISDatabase(
	exec openperouter.RouterExecutor,
	routeCIDRs map[ipfamily.Family]*net.IPNet,
	expectedMultiTopology map[ipfamily.Family]bool,
	expectedCounts map[ipfamily.Family]int,
) error {
	isisDatabase, err := frr.GetISISDatabaseDetail(exec)
	if err != nil {
		return err
	}

	gotCounts := map[ipfamily.Family]int{}
	extIPReach := isisDatabase.GetExtIPReach()
	ipv6Reach := isisDatabase.GetIPv6Reach()
	for _, extIPReach := range extIPReach {
		if extIPReach.Down {
			continue
		}
		ip, ipNet, err := net.ParseCIDR(extIPReach.IPReach)
		if err != nil {
			return err
		}
		prefixLen, _ := ipNet.Mask.Size()
		if prefixLen != 32 {
			continue
		}
		if !routeCIDRs[ipfamily.IPv4].Contains(ip) {
			continue
		}
		if expectedMultiTopology[ipfamily.IPv4] && extIPReach.MtID == "" {
			continue
		}
		if !expectedMultiTopology[ipfamily.IPv4] && extIPReach.MtID != "" {
			continue
		}
		fmt.Fprintf(GinkgoWriter, "found matching entry %+v\n", extIPReach)
		gotCounts[ipfamily.IPv4]++
	}
	for _, ipv6Reach := range ipv6Reach {
		if ipv6Reach.Down {
			continue
		}
		ip, ipNet, err := net.ParseCIDR(ipv6Reach.Prefix)
		if err != nil {
			return err
		}
		prefixLen, _ := ipNet.Mask.Size()
		if prefixLen != 128 {
			continue
		}
		if !routeCIDRs[ipfamily.IPv6].Contains(ip) {
			continue
		}
		if expectedMultiTopology[ipfamily.IPv6] && ipv6Reach.MtID == "" {
			continue
		}
		if !expectedMultiTopology[ipfamily.IPv6] && ipv6Reach.MtID != "" {
			continue
		}
		fmt.Fprintf(GinkgoWriter, "found matching entry %+v\n", ipv6Reach)
		gotCounts[ipfamily.IPv6]++
	}
	for af, expectedCount := range expectedCounts {
		if gotCounts[af] != expectedCount {
			return fmt.Errorf("expected matching count for af %s is %d but got %d entries, IS-IS database: %+v",
				af, expectedCount, gotCounts[af], isisDatabase)
		}
	}
	return nil
}
