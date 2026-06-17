// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Underlay is the multi-session configuration with multiple interfaces and neighbors
var Underlay = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:  64514,
		Nics: []string{"toswitch1", "toswitch2"},
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
		ASN:  64514,
		Nics: []string{"toswitch1", "toswitch2"},
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
		ASN:  64514,
		Nics: []string{"toleafkind1"},
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

// controlPlaneSelector matches the kind control-plane node, which runs the
// route reflector.
var controlPlaneSelector = &metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{
		{Key: "node-role.kubernetes.io/control-plane", Operator: metav1.LabelSelectorOpExists},
	},
}

// workerSelector matches the kind worker nodes, which act as route reflector
// clients.
var workerSelector = &metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{
		{Key: "node-role.kubernetes.io/control-plane", Operator: metav1.LabelSelectorOpDoesNotExist},
	},
}

// UnderlayRR runs the control-plane router as a pure BGP route reflector: no
// tunnel endpoint, one listen-range dynamic neighbor that reflects both ipv4
// unicast (VTEP /32 reachability) and l2vpn evpn (type-2/3) to the clients.
var UnderlayRR = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rr",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:          64514,
		Nics:         []string{"toswitch1"},
		NodeSelector: controlPlaneSelector,
		RouteReflector: &v1alpha1.RouteReflectorConfig{
			ClusterID: new("192.0.2.1"),
		},
		Neighbors: []v1alpha1.Neighbor{
			{
				ListenRange: new("192.168.11.0/24"),
				Type:        new("internal"),
				AddressFamilies: []v1alpha1.NeighborAddressFamily{
					{
						Type: "ipv4unicast",
						Properties: []v1alpha1.NeighborAddressFamilyProperty{
							{Type: v1alpha1.NeighborAddressFamilyPropertyRouteReflectorClient},
						},
					},
					{
						Type: "evpn",
						Properties: []v1alpha1.NeighborAddressFamilyProperty{
							{Type: v1alpha1.NeighborAddressFamilyPropertyRouteReflectorClient},
						},
					},
				},
			},
		},
	},
}

// UnderlayRRClient runs on the worker nodes as iBGP clients of UnderlayRR.
var UnderlayRRClient = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "client",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:          64514,
		Nics:         []string{"toswitch1"},
		NodeSelector: workerSelector,
		TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
			CIDRs: []string{"100.65.0.0/24"},
		},
		Neighbors: []v1alpha1.Neighbor{
			{
				Address: new("192.168.11.3"),
				Type:    new("internal"),
			},
		},
	},
}
