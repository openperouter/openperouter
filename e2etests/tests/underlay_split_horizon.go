// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

type fabricRouteKind string

const (
	fabricUnicastRoute fabricRouteKind = "unicast"
	fabricEVPNRoute    fabricRouteKind = "evpn"
	fabricVPNRoute     fabricRouteKind = "vpn"
)

type fabricRouteIdentity struct {
	prefix      string
	rd          string
	routeTarget string
	ethTag      int
	label       int
	esi         string
	routerMAC   string
}

type fabricTransitScenario struct {
	underlayFactory func(nodeName string) v1alpha1.Underlay
	peerFamily      ipfamily.Family
	routeFamily     ipfamily.Family
	routeKind       fabricRouteKind
	learnedRoute    fabricRouteIdentity
}

var _ = Describe("Underlay fabric transit protection", Ordered, func() {
	DescribeTableSubtree("address family and source identity", runFabricTransitScenario,
		Entry("IPv4 unicast numbered", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.Default, "ipv4unicast") },
			peerFamily:      ipfamily.IPv4,
			routeFamily:     ipfamily.IPv4,
			routeKind:       fabricUnicastRoute,
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.11.0/24"},
		}),
		Entry("IPv6 unicast numbered", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.IPv6, "ipv6unicast") },
			peerFamily:      ipfamily.IPv6,
			routeFamily:     ipfamily.IPv6,
			routeKind:       fabricUnicastRoute,
			learnedRoute:    fabricRouteIdentity{prefix: "2001:db8:fa11::/48"},
		}),
		Entry("IPv4 unicast unnumbered", fabricTransitScenario{
			underlayFactory: unnumberedTwoLeafUnderlay,
			peerFamily:      ipfamily.Unnumbered,
			routeFamily:     ipfamily.IPv4,
			routeKind:       fabricUnicastRoute,
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.12.0/24"},
		}),
		Entry("IPv4 unicast listen range", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.ListenRange, "ipv4unicast") },
			peerFamily:      ipfamily.IPv4,
			routeFamily:     ipfamily.IPv4,
			routeKind:       fabricUnicastRoute,
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.13.0/24"},
		}),
		Entry("IPv4-over-IPv6 listen range", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.ListenRangeIPv6, "ipv4unicast") },
			peerFamily:      ipfamily.IPv6,
			routeFamily:     ipfamily.IPv4,
			routeKind:       fabricUnicastRoute,
			learnedRoute:    fabricRouteIdentity{prefix: "198.18.14.0/24"},
		}),
		Entry("EVPN type-5 numbered", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.Default, "evpn") },
			peerFamily:      ipfamily.IPv4,
			routeFamily:     ipfamily.IPv4,
			routeKind:       fabricEVPNRoute,
			learnedRoute: fabricRouteIdentity{
				prefix: "198.18.21.0/24", rd: "64512:4021", ethTag: 4021, label: 4021,
				esi: "00:00:00:00:00:00:00:00:00:21", routerMAC: "00:00:00:00:00:21",
			},
		}),
		Entry("EVPN type-5 listen range", fabricTransitScenario{
			underlayFactory: func(_ string) v1alpha1.Underlay { return underlayWithFamilies(infra.ListenRange, "evpn") },
			peerFamily:      ipfamily.IPv4,
			routeFamily:     ipfamily.IPv4,
			routeKind:       fabricEVPNRoute,
			learnedRoute: fabricRouteIdentity{
				prefix: "198.18.22.0/24", rd: "64512:4022", ethTag: 4022, label: 4022,
				esi: "00:00:00:00:00:00:00:00:00:22", routerMAC: "00:00:00:00:00:22",
			},
		}),
	)
	Context("VPN address families", runFabricVPNScenario)
})

func runFabricTransitScenario(scenario fabricTransitScenario) {
	var (
		cs                    clientset.Interface
		nodes                 []corev1.Node
		selectedNode          corev1.Node
		routerExecutor        executor.Executor
		leaf2Executor         executor.Executor
		leaf1Neighbor         infra.Neighbor
		leaf2Neighbor         infra.Neighbor
		routerToLeaf1Neighbor infra.Neighbor
		routerToLeaf2Neighbor infra.Neighbor
		underlay              v1alpha1.Underlay
	)

	AfterEach(func() {
		if cs != nil {
			dumpIfFails(cs)
		}
	})

	BeforeAll(func() {
		cs = k8sclient.New()
		nodeList, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodeList.Items
		selectedNodes, err := k8s.NodesWithLabel(nodes, openperouter.InstrumentedNodesLabel)
		Expect(err).NotTo(HaveOccurred())
		Expect(selectedNodes).NotTo(BeEmpty(), "no nodes match instrumented node label selector %q",
			openperouter.InstrumentedNodesLabel)
		selectedNode = selectedNodes[0]

		underlay = scenario.underlayFactory(selectedNode.Name)
		underlay.Spec.NodeSelector = k8s.NodeSelectorForNode(selectedNode)

		Expect(Updater.CleanAll()).To(Succeed())
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

		configureIsolatedLeafkindPeers(
			scenario, leaf1Neighbor, leaf2Neighbor,
			infra.LeafKind1Config.ASN, infra.LeafKind2Config.ASN)
		for _, neighbor := range []string{routerToLeaf1Neighbor.ID, routerToLeaf2Neighbor.ID} {
			validate.SessionWithNeighbor(routerExecutor, validate.SessionParameters{
				FromName: selectedNode.Name, ToName: "fabric peer", NeighborIP: neighbor, Established: true,
			})
		}
	})

	AfterAll(func() {
		if scenario.routeKind == fabricEVPNRoute {
			// FRR reload cannot negate the full static EVPN network command.
			leaf1Executor := executor.ForContainer(infra.KindLeaf1Container.Name)
			output, err := leaf1Executor.Exec("vtysh", "-c", "configure terminal",
				"-c", fmt.Sprintf("no router bgp %d", infra.LeafKind1Config.ASN))
			Expect(err).NotTo(HaveOccurred(), "failed to remove isolated BGP instance: %s", output)
		}
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(Updater.CleanAll()).To(Succeed())
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("does not propagate routes from external peer A to external peer B", func() {
		checkFabricTransit(scenario, routerExecutor, leaf2Executor)
	})
}

// leafSRV6 peers directly with OpenPE over multihop eBGP; the fabric provides
// IP reachability without an intermediate BGP speaker relaying the VPN routes.
func runFabricVPNScenario() {
	var (
		cs               clientset.Interface
		routerExecutor   executor.Executor
		receiverExecutor executor.Executor
	)

	BeforeAll(func() {
		cs = k8sclient.New()
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		selectedNodes, err := k8s.NodesWithLabel(nodes, openperouter.InstrumentedNodesLabel)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(selectedNodes)).To(BeNumerically(">=", 2),
			"VPN split horizon needs two routers matching instrumented node label selector %q",
			openperouter.InstrumentedNodesLabel)

		Expect(Updater.CleanAll()).To(Succeed())
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafSRV6Config.Reset()).To(Succeed())

		underlay := infra.Underlay(infra.SRv6)
		underlay.Spec.NodeSelector = k8s.NodeSelectorForNode(selectedNodes[0])
		routerIP, err := openperouter.GetVtepIPForNode(underlay.Spec.TunnelEndpoint, &selectedNodes[0], ipfamily.IPv6)
		Expect(err).NotTo(HaveOccurred())
		receiverIP, err := openperouter.GetVtepIPForNode(underlay.Spec.TunnelEndpoint, &selectedNodes[1], ipfamily.IPv6)
		Expect(err).NotTo(HaveOccurred())
		routerIP = ipfamily.StripCIDRMask(routerIP)
		receiverIP = ipfamily.StripCIDRMask(receiverIP)

		underlay.Spec.Neighbors[0].AddressFamilies = []v1alpha1.NeighborAddressFamily{
			{Type: "ipv4vpn"}, {Type: "ipv6vpn"},
		}
		receiverNeighbor := underlay.Spec.Neighbors[0]
		receiverNeighbor.Address = new(receiverIP)
		receiverNeighbor.ASN = new(int64(64515))
		underlay.Spec.Neighbors = append(underlay.Spec.Neighbors, receiverNeighbor)

		receiver := infra.Underlay(infra.SRv6)
		receiver.Name = "fabric-vpn-receiver"
		receiver.Spec.NodeSelector = k8s.NodeSelectorForNode(selectedNodes[1])
		// A distinct ASN prevents AS-loop rejection from masking a missing split-horizon filter.
		receiver.Spec.ASN = 64515
		routerNeighbor := receiverNeighbor
		routerNeighbor.Address = new(routerIP)
		routerNeighbor.ASN = new(underlay.Spec.ASN)
		receiver.Spec.Neighbors = []v1alpha1.Neighbor{routerNeighbor}

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{underlay, receiver},
		})).To(Succeed())
		routers, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routerExecutor, err = routers.ExecutorForNode(selectedNodes[0].Name)
		Expect(err).NotTo(HaveOccurred())
		receiverExecutor, err = routers.ExecutorForNode(selectedNodes[1].Name)
		Expect(err).NotTo(HaveOccurred())
		for _, neighbor := range []string{*underlay.Spec.Neighbors[0].Address, receiverIP} {
			validate.SessionWithNeighbor(routerExecutor, validate.SessionParameters{
				FromName: selectedNodes[0].Name, ToName: "fabric peer", NeighborIP: neighbor, Established: true,
			})
		}
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	AfterAll(func() {
		Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
		Expect(Updater.CleanAll()).To(Succeed())
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	DescribeTable("does not propagate VPN routes from external peer A to external peer B",
		func(scenario fabricTransitScenario) {
			Expect(infra.LeafSRV6Config.ChangePrefixes(nil, []string{scenario.learnedRoute.prefix}, nil)).To(Succeed())

			checkFabricTransit(scenario, routerExecutor, receiverExecutor)
		},
		Entry("IPv4 VPN over IPv6", fabricTransitScenario{
			peerFamily:   ipfamily.IPv6,
			routeFamily:  ipfamily.IPv4,
			routeKind:    fabricVPNRoute,
			learnedRoute: fabricRouteIdentity{prefix: "192.170.20.0/24", rd: "100.65.0.1:100", routeTarget: "64520:100"},
		}),
		Entry("IPv6 VPN over IPv6", fabricTransitScenario{
			peerFamily:   ipfamily.IPv6,
			routeFamily:  ipfamily.IPv6,
			routeKind:    fabricVPNRoute,
			learnedRoute: fabricRouteIdentity{prefix: "2001:db8:170:20::/64", rd: "100.65.0.1:100", routeTarget: "64520:100"},
		}),
	)
}

func checkFabricTransit(scenario fabricTransitScenario, routerExecutor, receiverExecutor executor.Executor) {
	GinkgoHelper()
	By("learning A's route on OpenPERouter")
	Eventually(func() (bool, error) {
		return fabricRoutePresent(routerExecutor, scenario)
	}, 2*time.Minute, time.Second).Should(BeTrue())

	By("keeping A's route absent on B")
	Consistently(func() (bool, error) {
		return fabricRoutePresent(receiverExecutor, scenario)
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

func configureIsolatedLeafkindPeers(
	scenario fabricTransitScenario,
	leaf1Neighbor, leaf2Neighbor infra.Neighbor,
	leaf1ASN, leaf2ASN int,
) {
	leaf1Configuration := isolatedLeafKindConfiguration(scenario, leaf1Neighbor, leaf1ASN)
	switch scenario.routeKind {
	case fabricEVPNRoute:
		if scenario.learnedRoute.prefix != "" {
			route := scenario.learnedRoute
			leaf1Configuration.EVPNNetworks = []infra.EVPNNetwork{{
				Prefix: route.prefix, RD: route.rd, EthernetTag: route.ethTag,
				Label: route.label, ESI: route.esi, RouterMAC: route.routerMAC,
			}}
		}
	default:
		leaf1Configuration.Networks = map[string][]string{
			fmt.Sprintf("%s unicast", scenario.routeFamily): {scenario.learnedRoute.prefix},
		}
	}
	Expect(infra.LeafKind1Config.Configure(leaf1Configuration)).To(Succeed())
	Expect(infra.LeafKind2Config.Configure(
		isolatedLeafKindConfiguration(scenario, leaf2Neighbor, leaf2ASN))).To(Succeed())
}

func isolatedLeafKindConfiguration(
	scenario fabricTransitScenario, neighbor infra.Neighbor, asn int,
) infra.LeafKindConfiguration {
	return infra.LeafKindConfiguration{
		PeerIPFamily:       scenario.peerFamily,
		BGPAddressFamilies: scenario.leafFamilies(),
		DisableSpinePeer:   true,
		DisableISIS:        true,
		ASN:                asn,
		Neighbors:          []infra.Neighbor{neighbor},
	}
}

func (scenario fabricTransitScenario) leafFamilies() []networklayerprotocol.NLP {
	switch scenario.routeKind {
	case fabricEVPNRoute:
		return []networklayerprotocol.NLP{{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN}}
	default:
		if scenario.routeFamily == ipfamily.IPv6 {
			return []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast}}
		}
		return []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}}
	}
}

func fabricRoutePresent(exec executor.Executor, scenario fabricTransitScenario) (bool, error) {
	switch scenario.routeKind {
	case fabricEVPNRoute:
		routes, err := frr.EVPNInfo(exec)
		if err != nil {
			return false, err
		}
		return evpnContainsRoute(routes, scenario.learnedRoute), nil
	case fabricVPNRoute:
		routes, err := frr.L3VPNInfo(exec, scenario.routeFamily)
		if err != nil {
			return false, err
		}
		return frr.ContainsPrefixWithRouteTarget(routes, scenario.learnedRoute.prefix, scenario.learnedRoute.routeTarget), nil
	default: // IPv4 and IPv6 unicast routes use the IP routing table.
		routes, err := frr.BGPIPRoutesFor(exec, scenario.routeFamily)
		if err != nil {
			return false, err
		}
		_, present, err := selectedValidBestPaths(routes, scenario.learnedRoute.prefix)
		return present, err
	}
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
	for _, entry := range data.Entries {
		if entry.RD != route.rd {
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
					return true
				}
			}
		}
	}
	return false
}
