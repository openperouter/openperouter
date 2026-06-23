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
		ASN: 64514,
		Interfaces: []v1alpha1.UnderlayInterface{
			{
				Type:          "NetworkDevice",
				NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"},
			},
			{
				Type:          "NetworkDevice",
				NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch2"},
			},
		},
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
		ASN: 64514,
		Interfaces: []v1alpha1.UnderlayInterface{
			{
				Type:          "NetworkDevice",
				NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"},
			},
			{
				Type:          "NetworkDevice",
				NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch2"},
			},
		},
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
