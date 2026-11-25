// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/networklayerprotocol"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

type fabricRoute interface {
	checkFabricTransit(executor.Executor, fabricTransitScenario) (bool, error)
	configureIsolatedLeafkindPeers(fabricTransitScenario, infra.Neighbor, infra.Neighbor)
	leafFamilies(ipfamily.Family) []networklayerprotocol.NLP
	fabricRoutePresent(executor.Executor, fabricTransitScenario) (bool, error)
}

// fabricRouteIdentity holds the prefix and address-family-specific attributes used
// to originate a test route on peer A and identify it on OpenPE and peer B.
// Unicast routes need only a prefix; EVPN and VPN routes also carry their BGP identity.
type fabricRouteIdentity struct {
	prefix      string
	rd          string
	routeTarget string
	ethTag      int
}

type fabricTransitScenario struct {
	underlayFactory func(nodeName string) v1alpha1.Underlay
	peerFamily      ipfamily.Family
	routeFamily     ipfamily.Family
	route           fabricRoute
	learnedRoute    fabricRouteIdentity
}

// BGP topology: leafkind1 (A, AS 64512) -- OpenPE (AS 64514) -- leafkind2 (B, AS 64513).
// Both leaves peer only with the selected OpenPE; spine peering is disabled
// to prevent alternate paths. SRv6 cases run ISIS only toward OpenPE to exchange
// locator reachability; other cases disable ISIS. Leafkind1 originates a route
// that OpenPE must learn without advertising it to leafkind2. EVPN and SRv6 use the permanent
// sh-evpn VRF provisioned by setup.sh: EVPN exports its connected prefix through
// an L3VNI, while SRv6 exports VPN routes using per-leaf locators.
var _ = Describe("Underlay fabric transit protection", Ordered, func() {
	var (
		cs           clientset.Interface
		nodes        []corev1.Node
		selectedNode corev1.Node
	)

	BeforeAll(func() {
		cs = k8sclient.New()
		nodes, selectedNode = fabricTransitNodes(cs)
		Expect(Updater.CleanAll()).To(Succeed())
	})

	AfterAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())
	})

	DescribeTableSubtree("address family and source identity", func(scenario fabricTransitScenario) {
		var (
			routerExecutor        executor.Executor
			leaf2Executor         executor.Executor
			leaf1Neighbor         infra.Neighbor
			leaf2Neighbor         infra.Neighbor
			routerToLeaf1Neighbor infra.Neighbor
			routerToLeaf2Neighbor infra.Neighbor
			underlay              v1alpha1.Underlay
		)

		BeforeEach(func() {
			underlay = scenario.underlayFactory(selectedNode.Name)
			underlay.Spec.NodeSelector = k8s.NodeSelectorForNode(selectedNode)

			Expect(Updater.CleanButUnderlay()).To(Succeed())
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})).To(Succeed())
			routers, err := openperouter.Get(cs, HostMode)
			Expect(err).NotTo(HaveOccurred())
			routerExecutor, err = routers.ExecutorForNode(selectedNode.Name)
			Expect(err).NotTo(HaveOccurred())

			leaf2Executor = executor.ForContainer(infra.KindLeaf2Container.Name)
			leaf1Neighbor, err = infra.NeighborForFamily(infra.KindLeaf, selectedNode.Name, scenario.peerFamily)
			Expect(err).NotTo(HaveOccurred())
			leaf2Neighbor, err = infra.NeighborForFamily(infra.KindLeaf2, selectedNode.Name, scenario.peerFamily)
			Expect(err).NotTo(HaveOccurred())
			routerToLeaf1Neighbor, err = infra.NeighborForFamily(selectedNode.Name, infra.KindLeaf, scenario.peerFamily)
			Expect(err).NotTo(HaveOccurred())
			routerToLeaf2Neighbor, err = infra.NeighborForFamily(selectedNode.Name, infra.KindLeaf2, scenario.peerFamily)
			Expect(err).NotTo(HaveOccurred())

			scenario.route.configureIsolatedLeafkindPeers(scenario, leaf1Neighbor, leaf2Neighbor)
			for _, neighbor := range []string{routerToLeaf1Neighbor.ID, routerToLeaf2Neighbor.ID} {
				validate.SessionWithNeighbor(routerExecutor, validate.SessionParameters{
					FromName: selectedNode.Name, ToName: "fabric peer", NeighborIP: neighbor, Established: true,
				})
			}
		})

		AfterEach(func() {
			dumpIfFails(cs)
			Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
			Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
			Expect(Updater.CleanButUnderlay()).To(Succeed())
			Eventually(func() error {
				routers, err := openperouter.Get(cs, HostMode)
				if err != nil {
					return err
				}
				return openperouter.AreReady(routers)
			}, 2*time.Minute, time.Second).Should(Succeed())
		})

		It("does not propagate routes from external peer A to external peer B", func() {
			checkFabricTransit(scenario, routerExecutor, leaf2Executor, leaf2Neighbor.ID)
		})
	},
		Entry("IPv4 VPN over IPv6", fabricTransitScenario{
			underlayFactory: splitHorizonSRv6Underlay,
			peerFamily:      ipfamily.IPv6,
			routeFamily:     ipfamily.IPv4,
			route:           fabricVPNRoute{},
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.31.0/24", rd: "64512:100", routeTarget: "64512:100"},
		}),
		Entry("IPv6 VPN over IPv6", fabricTransitScenario{
			underlayFactory: splitHorizonSRv6Underlay,
			peerFamily:      ipfamily.IPv6,
			routeFamily:     ipfamily.IPv6,
			route:           fabricVPNRoute{},
			learnedRoute:    fabricRouteIdentity{prefix: "2001:db8:fa31::/48", rd: "64512:100", routeTarget: "64512:100"},
		}),
		Entry("IPv4 unicast numbered", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.Default, "ipv4unicast") },
			peerFamily:      ipfamily.IPv4,
			routeFamily:     ipfamily.IPv4,
			route:           fabricUnicastRoute{},
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.11.0/24"},
		}),
		Entry("IPv6 unicast numbered", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.IPv6, "ipv6unicast") },
			peerFamily:      ipfamily.IPv6,
			routeFamily:     ipfamily.IPv6,
			route:           fabricUnicastRoute{},
			learnedRoute:    fabricRouteIdentity{prefix: "2001:db8:fa11::/48"},
		}),
		Entry("IPv4 unicast unnumbered", fabricTransitScenario{
			underlayFactory: unnumberedTwoLeafUnderlay,
			peerFamily:      ipfamily.Unnumbered,
			routeFamily:     ipfamily.IPv4,
			route:           fabricUnicastRoute{},
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.12.0/24"},
		}),
		Entry("IPv4 unicast listen range", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.ListenRange, "ipv4unicast") },
			peerFamily:      ipfamily.IPv4,
			routeFamily:     ipfamily.IPv4,
			route:           fabricUnicastRoute{},
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.13.0/24"},
		}),
		Entry("IPv4-over-IPv6 listen range", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.ListenRangeIPv6, "ipv4unicast") },
			peerFamily:      ipfamily.IPv6,
			routeFamily:     ipfamily.IPv4,
			route:           fabricUnicastRoute{},
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.14.0/24"},
		}),

		Entry("EVPN type-5 numbered", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay {
				return underlayWithFamilies(infra.Default, "ipv4unicast", "evpn")
			},
			peerFamily:   ipfamily.IPv4,
			routeFamily:  ipfamily.IPv4,
			route:        fabricEVPNRoute{},
			learnedRoute: fabricRouteIdentity{prefix: "198.18.21.0/24"},
		}),
		Entry("EVPN type-5 listen range", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay {
				return underlayWithFamilies(infra.ListenRange, "ipv4unicast", "evpn")
			},
			peerFamily:   ipfamily.IPv4,
			routeFamily:  ipfamily.IPv4,
			route:        fabricEVPNRoute{},
			learnedRoute: fabricRouteIdentity{prefix: "198.18.21.0/24"},
		}),
	)
})

func checkFabricTransit(
	scenario fabricTransitScenario, routerExecutor, receiverExecutor executor.Executor, receiverNeighbor string,
) {
	GinkgoHelper()
	By("learning A's route on OpenPERouter")
	Eventually(func() (bool, error) {
		return scenario.route.checkFabricTransit(routerExecutor, scenario)
	}, 2*time.Minute, time.Second).Should(BeTrue())

	By("keeping A's route absent on B")
	Consistently(func() (bool, error) {
		neighbor, err := frr.NeighborInfo(receiverNeighbor, receiverExecutor)
		if err != nil {
			return false, err
		}
		if neighbor.BgpState != "Established" {
			return false, fmt.Errorf("receiver BGP session to %s is %s", receiverNeighbor, neighbor.BgpState)
		}
		return scenario.route.fabricRoutePresent(receiverExecutor, scenario)
	}, 5*time.Second, time.Second).Should(BeFalse())
}

func underlayWithFamilies(kind infra.UnderlayKind, familyTypes ...string) v1alpha1.Underlay {
	underlay := infra.Underlay(kind)
	for neighborIndex := range underlay.Spec.Neighbors {
		underlay.Spec.Neighbors[neighborIndex].AddressFamilies = make([]v1alpha1.NeighborAddressFamily, 0, len(familyTypes))
		for _, familyType := range familyTypes {
			underlay.Spec.Neighbors[neighborIndex].AddressFamilies = append(
				underlay.Spec.Neighbors[neighborIndex].AddressFamilies,
				v1alpha1.NeighborAddressFamily{Type: familyType},
			)
		}
	}
	return underlay
}

func unnumberedTwoLeafUnderlay(nodeName string) v1alpha1.Underlay {
	neighbor, err := infra.NeighborForFamily(nodeName, infra.KindLeaf2, ipfamily.Unnumbered)
	Expect(err).NotTo(HaveOccurred())
	underlay := infra.Underlay(infra.Unnumbered)
	underlay.Spec.Interfaces = append(underlay.Spec.Interfaces, v1alpha1.UnderlayInterface{
		Type:          "NetworkDevice",
		NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: neighbor.ID},
	})
	underlay.Spec.Neighbors = append(underlay.Spec.Neighbors, v1alpha1.Neighbor{
		ASN:                  new(int64(64513)),
		Interface:            new(neighbor.ID),
		ConnectTimeSeconds:   new(int64(5)),
		KeepaliveTimeSeconds: new(int64(3)),
		HoldTimeSeconds:      new(int64(9)),
	})
	return underlay
}

type fabricUnicastRoute struct{}

func (route fabricUnicastRoute) checkFabricTransit(
	exec executor.Executor, scenario fabricTransitScenario,
) (bool, error) {
	return route.fabricRoutePresent(exec, scenario)
}

func (route fabricUnicastRoute) configureIsolatedLeafkindPeers(
	scenario fabricTransitScenario, leaf1Neighbor, leaf2Neighbor infra.Neighbor,
) {
	GinkgoHelper()
	configurations := fabricLeafConfigurations(scenario, leaf1Neighbor, leaf2Neighbor,
		route.leafFamilies(scenario.routeFamily), "leafkind.split-horizon.tmpl")
	Expect(infra.LeafKind1Config.Configure(configurations[0])).To(Succeed())
	Expect(infra.LeafKind2Config.Configure(configurations[1])).To(Succeed())
}

func (fabricUnicastRoute) leafFamilies(family ipfamily.Family) []networklayerprotocol.NLP {
	return []networklayerprotocol.NLP{{AFI: networklayerprotocol.AFI(family), SAFI: networklayerprotocol.Unicast}}
}

func (fabricUnicastRoute) fabricRoutePresent(exec executor.Executor, scenario fabricTransitScenario) (bool, error) {
	routes, err := frr.BGPIPRoutesFor(exec, scenario.routeFamily)
	if err != nil {
		return false, err
	}
	_, present, err := selectedValidBestPaths(routes, scenario.learnedRoute.prefix)
	return present, err
}

type fabricEVPNRoute struct{}

func (fabricEVPNRoute) checkFabricTransit(exec executor.Executor, scenario fabricTransitScenario) (bool, error) {
	routes, err := frr.EVPNInfo(exec)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(evpnPathsForRoute(routes, scenario.learnedRoute), func(path frr.Path) bool {
		return path.Valid && path.Bestpath
	}), nil
}

func (route fabricEVPNRoute) configureIsolatedLeafkindPeers(
	scenario fabricTransitScenario, leaf1Neighbor, leaf2Neighbor infra.Neighbor,
) {
	GinkgoHelper()
	configurations := fabricLeafConfigurations(scenario, leaf1Neighbor, leaf2Neighbor,
		route.leafFamilies(scenario.routeFamily), "leafkind.split-horizon.evpn.tmpl")
	for index, leaf := range []infra.LeafKind{infra.LeafKind1Config, infra.LeafKind2Config} {
		// Match the VTEPs provisioned by the leafkind setup.sh in Containerlab.
		configurations[index].VTEPIP = fmt.Sprintf("192.0.2.%d", index+11)
		configurations[index].RouterID = configurations[index].VTEPIP
		Expect(leaf.Configure(configurations[index])).To(Succeed())
	}
}

func (fabricEVPNRoute) leafFamilies(ipfamily.Family) []networklayerprotocol.NLP {
	return []networklayerprotocol.NLP{{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN}}
}

func (fabricEVPNRoute) fabricRoutePresent(exec executor.Executor, scenario fabricTransitScenario) (bool, error) {
	routes, err := frr.EVPNInfo(exec)
	if err != nil {
		return false, err
	}
	return evpnContainsRoute(routes, scenario.learnedRoute), nil
}

type fabricVPNRoute struct{}

func (fabricVPNRoute) checkFabricTransit(exec executor.Executor, scenario fabricTransitScenario) (bool, error) {
	routes, err := frr.L3VPNInfo(exec, scenario.routeFamily)
	if err != nil {
		return false, err
	}
	prefixes := routes.Routes.RouteDistinguishers[frr.RouteDistinguisher(scenario.learnedRoute.rd)]
	for _, path := range prefixes[frr.BGPPrefix(scenario.learnedRoute.prefix)] {
		if path.Valid && path.Bestpath.Overall && slices.Contains(
			strings.Fields(path.ExtendedCommunity.String), "RT:"+scenario.learnedRoute.routeTarget) {
			return true, nil
		}
	}
	return false, nil
}

func (route fabricVPNRoute) configureIsolatedLeafkindPeers(
	scenario fabricTransitScenario, leaf1Neighbor, leaf2Neighbor infra.Neighbor,
) {
	GinkgoHelper()
	configurations := fabricLeafConfigurations(scenario, leaf1Neighbor, leaf2Neighbor,
		route.leafFamilies(scenario.routeFamily), "leafkind.split-horizon.srv6.tmpl")
	for index, leaf := range []infra.LeafKind{infra.LeafKind1Config, infra.LeafKind2Config} {
		configurations[index].RouterID = fmt.Sprintf("192.0.2.%d", index+1)
		configurations[index].SRv6 = &infra.SRv6Config{
			Prefix: fmt.Sprintf("fd00:0:%d::/48", index+11), EncapBehavior: infra.HEncapsRed,
		}
		Expect(leaf.Configure(configurations[index])).To(Succeed())
	}
}

func (fabricVPNRoute) leafFamilies(family ipfamily.Family) []networklayerprotocol.NLP {
	return []networklayerprotocol.NLP{{AFI: networklayerprotocol.AFI(family), SAFI: networklayerprotocol.Unicast}}
}

func (fabricVPNRoute) fabricRoutePresent(exec executor.Executor, scenario fabricTransitScenario) (bool, error) {
	routes, err := frr.L3VPNInfo(exec, scenario.routeFamily)
	if err != nil {
		return false, err
	}
	return frr.ContainsPrefixWithRouteTarget(routes, scenario.learnedRoute.prefix, scenario.learnedRoute.routeTarget), nil
}

func fabricLeafConfigurations(
	scenario fabricTransitScenario, leaf1Neighbor, leaf2Neighbor infra.Neighbor,
	families []networklayerprotocol.NLP, templateName string,
) [2]infra.LeafKindConfiguration {
	neighbors := []infra.Neighbor{leaf1Neighbor, leaf2Neighbor}
	var configurations [2]infra.LeafKindConfiguration
	for index, leaf := range []infra.LeafKind{infra.LeafKind1Config, infra.LeafKind2Config} {
		configurations[index] = infra.LeafKindConfiguration{
			PeerIPFamily: scenario.peerFamily, BGPAddressFamilies: families,
			TemplateName: templateName, ASN: leaf.ASN, Neighbors: []infra.Neighbor{neighbors[index]},
		}
	}
	configurations[0].Networks = map[string][]string{
		networklayerprotocol.SAFIAFIKey(
			networklayerprotocol.AFI(scenario.routeFamily), networklayerprotocol.Unicast): {scenario.learnedRoute.prefix},
	}
	return configurations
}

func selectedValidBestPaths(routes frr.IPInfo, prefix string) ([]frr.FRRRoute, bool, error) {
	paths, found := routes.Routes[prefix]
	if !found {
		return nil, false, nil
	}

	selected := make([]frr.FRRRoute, 0, len(paths))
	for _, path := range paths {
		if path.Valid && path.BestPath {
			selected = append(selected, path)
		}
	}
	if len(selected) == 0 {
		return nil, true, fmt.Errorf("no valid best path among %d paths", len(paths))
	}
	return selected, true, nil
}

func evpnContainsRoute(data frr.EVPNData, route fabricRouteIdentity) bool {
	return len(evpnPathsForRoute(data, route)) > 0
}

func evpnPathsForRoute(data frr.EVPNData, route fabricRouteIdentity) []frr.Path {
	var paths []frr.Path
	for _, entry := range data.Entries {
		if route.rd != "" && entry.RD != route.rd {
			continue
		}
		for _, prefix := range entry.Prefixes {
			for _, path := range prefix.Paths {
				if path.Removed {
					continue
				}
				if path.RouteType != 5 {
					continue
				}
				if path.EthTag != route.ethTag {
					continue
				}
				if fmt.Sprintf("%s/%d", path.IP, path.IPLen) == route.prefix {
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

func splitHorizonSRv6Underlay(_ string) v1alpha1.Underlay {
	underlay := underlayWithFamilies(infra.IPv6, "ipv4vpn", "ipv6vpn")
	srv6 := infra.Underlay(infra.SRv6)
	// Exchange locator reachability with both leaves over the test-facing links.
	underlay.Spec.ISIS = srv6.Spec.ISIS
	underlay.Spec.ISIS.Interfaces = nil
	for _, iface := range underlay.Spec.Interfaces {
		underlay.Spec.ISIS.Interfaces = append(underlay.Spec.ISIS.Interfaces, v1alpha1.ISISInterface{
			Name: iface.NetworkDevice.InterfaceName, IPFamily: new(v1alpha1.IPFamilyIPv6),
		})
	}
	underlay.Spec.SRV6 = srv6.Spec.SRV6
	// Keep EVPN initialized locally as the table switches between VPN and EVPN.
	// The test peers still activate only VPN address families.
	underlay.Spec.TunnelEndpoint = infra.Underlay(infra.EVPNandSRv6).Spec.TunnelEndpoint
	underlay.Spec.RouterIDCIDR = srv6.Spec.RouterIDCIDR
	return underlay
}

func fabricTransitNodes(cs clientset.Interface) ([]corev1.Node, corev1.Node) {
	GinkgoHelper()
	nodes, err := k8s.GetNodes(cs)
	Expect(err).NotTo(HaveOccurred())
	selectedNodes, err := k8s.NodesWithLabel(nodes, openperouter.InstrumentedNodesLabel)
	Expect(err).NotTo(HaveOccurred())
	Expect(selectedNodes).NotTo(BeEmpty(), "no nodes match instrumented node label selector %q",
		openperouter.InstrumentedNodesLabel)
	return nodes, selectedNodes[0]
}
