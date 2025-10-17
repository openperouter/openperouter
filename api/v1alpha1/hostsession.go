// SPDX-License-Identifier:Apache-2.0

package v1alpha1

// Host Session represent the leg between the router and the host.
// A BGP session is established over this leg.
type HostSession struct {
	// ASN is the local AS number to use to establish a BGP session with
	// the default namespace.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN uint32 `json:"asn,omitempty"`

	// ASN is the expected AS number for a BGP speaking component running in
	// the default network namespace. If not set, the ASN field is going to be used.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	HostASN uint32 `json:"hostasn,omitempty"`

	// LocalCIDR is the CIDR configuration for the veth pair
	// to connect with the default namespace. The interface under
	// the PERouter side is going to use the first IP of the cidr on all the nodes.
	// At least one of IPv4 or IPv6 must be provided.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="LocalCIDR can't be changed"
	LocalCIDR LocalCIDRConfig `json:"localcidr"`
}

type LocalCIDRConfig struct {
	// IPv4 is the IPv4 CIDR to be used for the veth pair
	// to connect with the default namespace. The interface under
	// the PERouter side is going to use the first IP of the cidr on all the nodes.
	// +optional
	IPv4 string `json:"ipv4,omitempty"`

	// IPv6 is the IPv6 CIDR to be used for the veth pair
	// to connect with the default namespace. The interface under
	// the PERouter side is going to use the first IP of the cidr on all the nodes.
	// +optional
	IPv6 string `json:"ipv6,omitempty"`
}
