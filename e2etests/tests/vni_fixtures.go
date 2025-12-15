// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// L3VNIBuilder provides a fluent API for constructing L3VNI objects.
type L3VNIBuilder struct {
	name        string
	vrf         string
	vni         uint32
	hostSession *v1alpha1.HostSession
}

// NewL3VNI creates a new L3VNIBuilder with the given name and VNI.
// The name is used for both the L3VNI name and the VRF name.
func NewL3VNI(name string, vni uint32) *L3VNIBuilder {
	return &L3VNIBuilder{
		name: name,
		vrf:  name,
		vni:  vni,
	}
}

// WithHostSession configures a host session with the specified parameters.
// This enables BGP sessions between the VNI and host namespace.
func (b *L3VNIBuilder) WithHostSession(asn, hostASN uint32, localCIDR v1alpha1.LocalCIDRConfig) *L3VNIBuilder {
	b.hostSession = &v1alpha1.HostSession{
		ASN:       asn,
		HostASN:   hostASN,
		LocalCIDR: localCIDR,
	}
	return b
}

// Build constructs the final L3VNI object.
func (b *L3VNIBuilder) Build() v1alpha1.L3VNI {
	return v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.name,
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF:         b.vrf,
			VNI:         b.vni,
			HostSession: b.hostSession,
		},
	}
}

// IPv4LocalCIDR creates a LocalCIDRConfig for single-stack IPv4.
func IPv4LocalCIDR(ipv4 string) v1alpha1.LocalCIDRConfig {
	return v1alpha1.LocalCIDRConfig{IPv4: ipv4}
}

// IPv6LocalCIDR creates a LocalCIDRConfig for single-stack IPv6.
func IPv6LocalCIDR(ipv6 string) v1alpha1.LocalCIDRConfig {
	return v1alpha1.LocalCIDRConfig{IPv6: ipv6}
}

// DualStackLocalCIDR creates a LocalCIDRConfig for dual-stack (IPv4 and IPv6).
func DualStackLocalCIDR(ipv4, ipv6 string) v1alpha1.LocalCIDRConfig {
	return v1alpha1.LocalCIDRConfig{IPv4: ipv4, IPv6: ipv6}
}

// NewL2VNIRed creates a standard "red110" L2VNI associated with the red VRF.
// This L2VNI uses:
// - VRF: red (pointer)
// - VNI: 110
func NewL2VNIRed() v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: ptr.To("red"),
			VNI: 110,
		},
	}
}

// NewL3PassthroughDefault creates a standard L3Passthrough with default configuration.
// This passthrough uses:
// - ASN: 64514
// - Host ASN: 64515
// - IPv4 CIDR: 192.169.10.0/24
// - IPv6 CIDR: 2001:db8:1::/64
func NewL3PassthroughDefault() v1alpha1.L3Passthrough {
	return v1alpha1.L3Passthrough{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "passthrough",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3PassthroughSpec{
			HostSession: v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64515,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.10.0/24",
					IPv6: "2001:db8:1::/64",
				},
			},
		},
	}
}
