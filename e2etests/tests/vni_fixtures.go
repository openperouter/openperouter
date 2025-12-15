// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// NewL3VNIRed creates a standard "red" L3VNI with host session configuration.
// This VNI uses:
// - VRF: red
// - VNI: 100
// - ASN: 64514
// - Host ASN: 64515
// - IPv4 CIDR: 192.169.10.0/24
// - IPv6 CIDR: 2001:db8:1::/64
func NewL3VNIRed() v1alpha1.L3VNI {
	return v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64515,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.10.0/24",
					IPv6: "2001:db8:1::/64",
				},
			},
			VNI: 100,
		},
	}
}

// NewL3VNIRedSimple creates a simple "red" L3VNI without host session.
// This version is used for L2 overlay tests that don't need BGP sessions.
// - VRF: red
// - VNI: 100
func NewL3VNIRedSimple() v1alpha1.L3VNI {
	return v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: 100,
		},
	}
}

// NewL3VNIBlue creates a standard "blue" L3VNI with host session configuration.
// This VNI uses:
// - VRF: blue
// - VNI: 200
// - ASN: 64514
// - Host ASN: 64515
// - IPv4 CIDR: 192.169.11.0/24
// - IPv6 CIDR: 2001:db8:2::/64
func NewL3VNIBlue() v1alpha1.L3VNI {
	return v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blue",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "blue",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64515,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.11.0/24",
					IPv6: "2001:db8:2::/64",
				},
			},
			VNI: 200,
		},
	}
}

// NewL3VNIWithHostSession creates a custom L3VNI with the specified parameters.
// This function allows creating VNIs with custom names, VRFs, VNI IDs, and host sessions.
//
// Example:
//
//	hostSession := &v1alpha1.HostSession{
//	    ASN:     64514,
//	    HostASN: 64515,
//	    LocalCIDR: v1alpha1.LocalCIDRConfig{
//	        IPv4: "192.169.12.0/24",
//	    },
//	}
//	vni := NewL3VNIWithHostSession("custom", "custom-vrf", 300, hostSession)
func NewL3VNIWithHostSession(name, vrf string, vni uint32, hostSession *v1alpha1.HostSession) v1alpha1.L3VNI {
	return v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF:         vrf,
			HostSession: hostSession,
			VNI:         vni,
		},
	}
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
