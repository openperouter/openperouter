// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// We want to use the same Interfaces for all underlays wherever possible. The reason is that if the underlay is
// updated and the interfaces change, the controller will tear down dependent resources (VNIs, passthrough)
// and restore the old interfaces before setting up the new ones. While this is now graceful (no pod restart
// required), changing interfaces mid-test can still disrupt E2E tests that cache router state in BeforeAll().
//
// Interface names default to toswitch1/toswitch2; RegisterLinks may override them.

var defaultInterfaces []v1alpha1.UnderlayInterface

// DefaultInterfaces exposes the interfaces shared by all the underlay
// fixtures, so underlays defined outside this package can keep the
// interface parity described above.
var DefaultInterfaces []v1alpha1.UnderlayInterface

func buildDefaultInterfaces(cfg UnderlayIfacesConfig) []v1alpha1.UnderlayInterface {
	ifaces := []v1alpha1.UnderlayInterface{
		{
			Type:          "NetworkDevice",
			NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: cfg.Iface1},
		},
	}
	if cfg.DualLeaf {
		ifaces = append(ifaces, v1alpha1.UnderlayInterface{
			Type:          "NetworkDevice",
			NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: cfg.Iface2},
		})
	}
	return ifaces
}

// Full dual-leaf neighbor lists; trimmed by ConfigureUnderlayIfaces when needed.
var (
	underlayNeighborsFull           []v1alpha1.Neighbor
	underlayIPv6NeighborsFull       []v1alpha1.Neighbor
	underlayListenNeighborsFull     []v1alpha1.Neighbor
	underlayListenIPv6NeighborsFull []v1alpha1.Neighbor
)

// neighborsFor returns full when dualLeaf is set, otherwise only the first neighbor.
func neighborsFor(dualLeaf bool, full []v1alpha1.Neighbor) []v1alpha1.Neighbor {
	n := full
	if !dualLeaf && len(full) > 1 {
		n = full[:1]
	}
	out := make([]v1alpha1.Neighbor, len(n))
	copy(out, n)
	return out
}

// applyUnderlayInterfaces updates shared underlay fixtures from cfg.
func applyUnderlayInterfaces(cfg UnderlayIfacesConfig) {
	defaultInterfaces = buildDefaultInterfaces(cfg)
	DefaultInterfaces = defaultInterfaces
	Underlay.Spec.Interfaces = defaultInterfaces
	UnderlayIPv6.Spec.Interfaces = defaultInterfaces
	UnderlayListenRange.Spec.Interfaces = defaultInterfaces
	UnderlayListenRangeIPv6.Spec.Interfaces = defaultInterfaces
	UnderlaySRv6.Spec.Interfaces = defaultInterfaces
	UnderlaySRv6EncapRed.Spec.Interfaces = defaultInterfaces
	UnderlayEVPNandSRv6.Spec.Interfaces = defaultInterfaces

	Underlay.Spec.Neighbors = neighborsFor(cfg.DualLeaf, underlayNeighborsFull)
	UnderlayIPv6.Spec.Neighbors = neighborsFor(cfg.DualLeaf, underlayIPv6NeighborsFull)
	UnderlayListenRange.Spec.Neighbors = neighborsFor(cfg.DualLeaf, underlayListenNeighborsFull)
	UnderlayListenRangeIPv6.Spec.Neighbors = neighborsFor(cfg.DualLeaf, underlayListenIPv6NeighborsFull)

	if UnderlaySRv6.Spec.ISIS != nil && len(UnderlaySRv6.Spec.ISIS.Interfaces) > 0 {
		UnderlaySRv6.Spec.ISIS.Interfaces[0].Name = cfg.Iface1
	}
	if UnderlaySRv6EncapRed.Spec.ISIS != nil && len(UnderlaySRv6EncapRed.Spec.ISIS.Interfaces) > 0 {
		UnderlaySRv6EncapRed.Spec.ISIS.Interfaces[0].Name = cfg.Iface1
	}
	if UnderlayEVPNandSRv6.Spec.ISIS != nil && len(UnderlayEVPNandSRv6.Spec.ISIS.Interfaces) > 0 {
		UnderlayEVPNandSRv6.Spec.ISIS.Interfaces[0].Name = cfg.Iface1
	}
}

func init() {
	underlayNeighborsFull = append([]v1alpha1.Neighbor(nil), Underlay.Spec.Neighbors...)
	underlayIPv6NeighborsFull = append([]v1alpha1.Neighbor(nil), UnderlayIPv6.Spec.Neighbors...)
	underlayListenNeighborsFull = append([]v1alpha1.Neighbor(nil), UnderlayListenRange.Spec.Neighbors...)
	underlayListenIPv6NeighborsFull = append([]v1alpha1.Neighbor(nil), UnderlayListenRangeIPv6.Spec.Neighbors...)
	cfg, err := ResolveUnderlayHostIfaces(nil)
	if err != nil {
		panic(err)
	}
	applyUnderlayInterfaces(cfg)
}

// Underlay is the multi-session configuration with multiple interfaces and neighbors
var Underlay = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
}

var UnderlayIPv6 = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
}

var UnderlayUnnumbered = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
}

// UnderlayListenRange accepts dynamic eBGP sessions from the leaf switches
// via bgp listen range instead of configuring them as explicit neighbors.
// The leaves actively dial the nodes from addresses inside the ranges
// (192.168.11.2 and 192.168.12.2), so the sessions establish with the node
// side acting as the passive end. The address families are set explicitly:
// dynamic peers negotiate their multiprotocol capabilities when the session
// is accepted, so every family the tests rely on must be active on the
// peer-group before the leaves connect.
var UnderlayListenRange = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
}

// UnderlayListenRangeIPv6 is the IPv6 variant of UnderlayListenRange: the
// leaves dial the nodes over IPv6 from 2001:db8:11::2 and 2001:db8:12::2.
// The ipv4 unicast family carries the IPv4 VTEP reachability over the IPv6
// sessions.
var UnderlayListenRangeIPv6 = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
}

var UnderlaySRv6 = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
}

var UnderlaySRv6EncapRed = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
			EncapBehavior: new(v1alpha1.HEncapsRed),
			Locator: v1alpha1.SRV6Locator{
				BasePrefix: "fd00:0:32::/48",
				Format:     "usid-f3216",
			},
		},
	},
}

var UnderlayEVPNandSRv6 = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
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
}
