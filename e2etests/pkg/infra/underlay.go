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
				ASN:     new(int64(64512)),
				Address: new("192.168.11.2"),
			},
			{
				ASN:     new(int64(64513)),
				Address: new("192.168.12.2"),
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
				ASN:     new(int64(64512)),
				Address: new("2001:db8:11::2"),
			},
			{
				ASN:     new(int64(64513)),
				Address: new("2001:db8:12::2"),
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
				ASN:       new(int64(64512)),
				Interface: new("toleafkind1"),
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
				ASN:          new(int64(64520)),
				Address:      new("2001:db8:1234::1"),
				EBGPMultiHop: new(true),
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

var UnderlayEVPNandSRv6 = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:        64514,
		Interfaces: defaultInterfaces,
		Neighbors: []v1alpha1.Neighbor{
			// leafA - use automatically derived address families.
			{
				ASN:          new(int64(64520)),
				Address:      new("2001:db8:1234::1"),
				EBGPMultiHop: new(true),
			},
			// leafB - explicitly set address families.
			{
				ASN:          new(int64(64520)),
				Address:      new("2001:db8:1234::2"),
				EBGPMultiHop: new(true),
				AddressFamilies: []v1alpha1.NeighborAddressFamily{
					{Type: "ipv6unicast"},
					{Type: "ipv4vpn"},
					{Type: "ipv6vpn"},
				},
			},
			// leafkind
			{
				ASN:     new(int64(64512)),
				Address: new("192.168.11.2"),
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
