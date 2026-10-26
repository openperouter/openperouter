// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	Default              UnderlayKind = "default"
	IPv6                 UnderlayKind = "ipv6"
	Unnumbered           UnderlayKind = "unnumbered"
	ListenRange          UnderlayKind = "listen-range"
	ListenRangeIPv6      UnderlayKind = "listen-range-ipv6"
	SRv6                 UnderlayKind = "srv6"
	EVPNandSRv6          UnderlayKind = "evpn-and-srv6"
	ISISOnly             UnderlayKind = "isis-only"
	SimpleBFD            UnderlayKind = "simple-bfd"
	BFDWithParameters    UnderlayKind = "bfd-with-parameters"
	IBGP                 UnderlayKind = "ibgp"
	RouteReflector       UnderlayKind = "route-reflector"
	RouteReflectorClient UnderlayKind = "route-reflector-client"

	// RouteReflectorAddress is the control-plane router pod address on the
	// leafkind1 switch subnet, where the RouteReflector underlay accepts
	// dynamic neighbors.
	RouteReflectorAddress = "192.168.11.3"
)

// We want to use the same Interfaces for all underlays wherever possible. The reason is that if the underlay is
// updated and the interfaces change, the controller will tear down dependent resources (VNIs, passthrough)
// and restore the old interfaces before setting up the new ones. While this is now graceful (no pod restart
// required), changing interfaces mid-test can still disrupt E2E tests that cache router state in BeforeAll().

var defaultInterfaces = []v1alpha1.UnderlayInterface{
	{
		Type:          "NetworkDevice",
		NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"},
	},
	{
		Type:          "NetworkDevice",
		NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch2"},
	},
}

// UnderlayKind identifies the underlay fixtures returned by Underlay.
type UnderlayKind string

// underlays holds the underlay fixtures. The values are shared
// package-level state: Underlay only hands out deep copies of them, so
// callers can modify the returned underlays freely, including in-place
// interface customization (e.g. the acceleratedConfig port parameters
// in the hw-emulation suite).
var underlays = map[UnderlayKind]v1alpha1.Underlay{
	Default: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: defaultInterfaces,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:                  new(int64(64512)),
					Address:              new("192.168.11.2"),
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
				{
					ASN:                  new(int64(64513)),
					Address:              new("192.168.12.2"),
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	},
	IPv6: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: defaultInterfaces,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:                  new(int64(64512)),
					Address:              new("2001:db8:11::2"),
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
				{
					ASN:                  new(int64(64513)),
					Address:              new("2001:db8:12::2"),
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	},
	Unnumbered: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN: 64514,
			Interfaces: []v1alpha1.UnderlayInterface{
				{
					Type:          "NetworkDevice",
					NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toleafkind1"},
				},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:                  new(int64(64512)),
					Interface:            new("toleafkind1"),
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	},
	// ListenRange accepts dynamic eBGP sessions from the leaf switches
	// via bgp listen range instead of configuring them as explicit neighbors.
	// The leaves actively dial the nodes from addresses inside the ranges
	// (192.168.11.2 and 192.168.12.2), so the sessions establish with the node
	// side acting as the passive end. The address families are set explicitly:
	// dynamic peers negotiate their multiprotocol capabilities when the session
	// is accepted, so every family the tests rely on must be active on the
	// peer-group before the leaves connect.
	ListenRange: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: defaultInterfaces,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:         new(int64(64512)),
					ListenRange: new("192.168.11.0/24"),
					AddressFamilies: []v1alpha1.NeighborAddressFamily{
						{Type: "ipv4unicast"},
						{Type: "evpn"},
					},
				},
				{
					ASN:         new(int64(64513)),
					ListenRange: new("192.168.12.0/24"),
					AddressFamilies: []v1alpha1.NeighborAddressFamily{
						{Type: "ipv4unicast"},
						{Type: "evpn"},
					},
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	},
	// ListenRangeIPv6 is the IPv6 variant of ListenRange: the
	// leaves dial the nodes over IPv6 from 2001:db8:11::2 and 2001:db8:12::2.
	// The ipv4 unicast family carries the IPv4 VTEP reachability over the IPv6
	// sessions.
	ListenRangeIPv6: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: defaultInterfaces,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:         new(int64(64512)),
					ListenRange: new("2001:db8:11::/64"),
					AddressFamilies: []v1alpha1.NeighborAddressFamily{
						{Type: "ipv6unicast"},
						{Type: "ipv4unicast"},
						{Type: "evpn"},
					},
				},
				{
					ASN:         new(int64(64513)),
					ListenRange: new("2001:db8:12::/64"),
					AddressFamilies: []v1alpha1.NeighborAddressFamily{
						{Type: "ipv6unicast"},
						{Type: "ipv4unicast"},
						{Type: "evpn"},
					},
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	},
	SRv6: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: defaultInterfaces,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:                  new(int64(64520)),
					Address:              new("2001:db8:1234::1"),
					Properties:           []v1alpha1.NeighborProperty{{Type: v1alpha1.NeighborPropertyEBGPMultiHop}},
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
			},
			RouterIDCIDR: new("10.0.0.0/24"),
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{
					"2001:db8:1234:5678::/64",
				},
			},
			ISIS: &v1alpha1.ISISConfig{
				BaseNet: "49.0001.0002.0003.0004.00",
				Level:   new(int32(1)),
				Interfaces: []v1alpha1.ISISInterface{
					{
						Name:     "toswitch1",
						IPFamily: new(v1alpha1.IPFamilyIPv6),
					},
				},
			},
			SRV6: &v1alpha1.SRV6Config{
				Locator: v1alpha1.SRV6Locator{
					BasePrefix: "fd00:0:32::/48",
					Format:     "usid-f3216",
				},
			},
		},
	},
	EVPNandSRv6: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: defaultInterfaces,
			Neighbors: []v1alpha1.Neighbor{
				// leafSRV6 - use automatically derived address families.
				{
					ASN:                  new(int64(64520)),
					Address:              new("2001:db8:1234::1"),
					Properties:           []v1alpha1.NeighborProperty{{Type: v1alpha1.NeighborPropertyEBGPMultiHop}},
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
				// leafkind
				{
					ASN:                  new(int64(64512)),
					Address:              new("192.168.11.2"),
					ConnectTimeSeconds:   new(int64(5)),
					KeepaliveTimeSeconds: new(int64(3)),
					HoldTimeSeconds:      new(int64(9)),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{
					"2001:db8:1234:5678::/64",
					"100.65.0.0/24",
				},
			},
			RouterIDCIDR: new("10.0.0.0/24"),
			ISIS: &v1alpha1.ISISConfig{
				BaseNet: "49.0001.0002.0003.0004.00",
				Level:   new(int32(1)),
				Interfaces: []v1alpha1.ISISInterface{
					{
						Name:     "toswitch1",
						IPFamily: new(v1alpha1.IPFamilyIPv6),
					},
				},
			},
			SRV6: &v1alpha1.SRV6Config{
				Locator: v1alpha1.SRV6Locator{
					BasePrefix: "fd00:0:32::/48",
					Format:     "usid-f3216",
				},
			},
		},
	},
	ISISOnly: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: defaultInterfaces,
			// Although neighbors are not needed for IS-IS only tests, we need a dummy neighbor to make validation happy.
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64512)),
					Address: new("192.0.2.1"),
				},
			},
			ISIS: &v1alpha1.ISISConfig{
				BaseNet: "49.0001.0002.0003.0004.00",
				Level:   new(int32(1)),
				Interfaces: []v1alpha1.ISISInterface{
					{
						Name:     "toswitch1",
						IPFamily: new(v1alpha1.IPFamilyDualStack),
					},
					{
						Name:     "lo",
						IPFamily: new(v1alpha1.IPFamilyDualStack),
					},
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{
					"100.65.0.0/24",
					"2001:db8:1234:5678::/64",
				},
			},
		},
	},
	SimpleBFD: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay-simple-bfd"},
		Spec: v1alpha1.UnderlaySpec{
			ASN: 64514,
			Interfaces: []v1alpha1.UnderlayInterface{
				{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"}},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64512)),
					Address: new("192.168.11.2"),
					BFD:     &v1alpha1.BFDSettings{},
				},
			},
		},
	},
	BFDWithParameters: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay-bfd-params"},
		Spec: v1alpha1.UnderlaySpec{
			ASN: 64514,
			Interfaces: []v1alpha1.UnderlayInterface{
				{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"}},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64512)),
					Address: new("192.168.11.2"),
					BFD: &v1alpha1.BFDSettings{
						TransmitInterval: new(int32(90)),
						ReceiveInterval:  new(int32(80)),
						DetectMultiplier: new(int32(5)),
					},
				},
			},
		},
	},
	// IBGP peers with both leaf switches as Internal neighbors, with the
	// nodes and the leaves sharing ASN 64512.
	IBGP: {
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64512,
			Interfaces: defaultInterfaces,
			Neighbors: []v1alpha1.Neighbor{
				{
					Type:    new("Internal"),
					Address: new("192.168.11.2"),
				},
				{
					Type:    new("Internal"),
					Address: new("192.168.12.2"),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	},
	// RouteReflector runs the control-plane router as a pure BGP route
	// reflector: no tunnel endpoint, one listen-range dynamic neighbor
	// that reflects both ipv4 unicast (VTEP /32 reachability) and l2vpn
	// evpn (type-2/3) to the clients.
	RouteReflector: {
		ObjectMeta: metav1.ObjectMeta{Name: "rr"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:          64514,
			Interfaces:   defaultInterfaces,
			NodeSelector: &k8s.ControlPlaneNodesLabelSelector,
			RouteReflector: &v1alpha1.RouteReflectorConfig{
				ClusterID: new("192.0.2.1"),
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ListenRange: new("192.168.11.0/24"),
					Type:        new("Internal"),
					AddressFamilies: []v1alpha1.NeighborAddressFamily{
						{
							Type: "ipv4unicast",
							Properties: []v1alpha1.AddressFamilyProperty{
								{Type: v1alpha1.AddressFamilyPropertyRouteReflectorClient},
							},
						},
						{
							Type: "evpn",
							Properties: []v1alpha1.AddressFamilyProperty{
								{Type: v1alpha1.AddressFamilyPropertyRouteReflectorClient},
							},
						},
					},
				},
			},
		},
	},
	// RouteReflectorClient runs on the worker nodes as iBGP client of RouteReflector.
	RouteReflectorClient: {
		ObjectMeta: metav1.ObjectMeta{Name: "client"},
		Spec: v1alpha1.UnderlaySpec{
			ASN:          64514,
			Interfaces:   defaultInterfaces,
			NodeSelector: &k8s.NonControlPlaneNodesLabelSelector,
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					Address: new(RouteReflectorAddress),
					Type:    new("Internal"),
				},
			},
		},
	},
}

// Underlay returns a deep copy of the underlay fixture for the given kind,
// with the namespace set to the active openperouter namespace.
func Underlay(kind UnderlayKind) v1alpha1.Underlay {
	u, ok := underlays[kind]
	if !ok {
		panic(fmt.Sprintf("unknown underlay kind %q", kind))
	}
	underlay := *u.DeepCopy()
	underlay.Namespace = openperouter.Namespace
	return underlay
}
