// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var Underlay = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN:  64514,
		Nics: []string{"toswitch"},
		Neighbors: []v1alpha1.Neighbor{
			{
				ASN:     64512,
				Address: "192.168.11.2",
			},
		},
		EVPN: &v1alpha1.EVPNConfig{
			VTEPCIDR: "100.65.0.0/24",
		},
	},
}
