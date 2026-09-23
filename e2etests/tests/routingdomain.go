// SPDX-License-Identifier:Apache-2.0

package tests

import "github.com/openperouter/openperouter/api/v1alpha1"

func l3vniRoutingDomain(name string) *v1alpha1.RoutingDomain {
	return &v1alpha1.RoutingDomain{
		Type:  v1alpha1.RoutingDomainTypeL3VNI,
		L3VNI: &v1alpha1.L3VNIReference{Name: name},
	}
}

func l3vpnRoutingDomain(name string) *v1alpha1.RoutingDomain {
	return &v1alpha1.RoutingDomain{
		Type:  v1alpha1.RoutingDomainTypeL3VPN,
		L3VPN: &v1alpha1.L3VPNReference{Name: name},
	}
}
