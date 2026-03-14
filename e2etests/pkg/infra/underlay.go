// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var Underlay v1alpha1.Underlay

func init() {
	topo := Topology()

	leafkindIP, err := topo.GetBroadcastMemberIP("leafkind-switch", "leafkind", IPv4)
	if err != nil {
		panic(fmt.Sprintf("topology: %v", err))
	}

	leafkind := topo.Nodes["leafkind"]

	Underlay = v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "underlay",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN:  64514,
			Nics: []string{"toswitch"},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     leafkind.BGP.ASN,
					Address: stripCIDR(leafkindIP),
				},
			},
			EVPN: &v1alpha1.EVPNConfig{
				VTEPCIDR: "100.65.0.0/24",
			},
		},
	}
}
