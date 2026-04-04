// SPDX-License-Identifier:Apache-2.0

package v1alpha1

// Host Session represents the leg between the router and the host.
// A BGP session is established over this leg.
// +kubebuilder:validation:XValidation:rule="has(self.hostasn) || has(self.hosttype)",message="either HostASN or HostType must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.hostasn) || !has(self.hosttype)",message="HostASN and HostType cannot be set together"
type HostSession struct {
	// ASN is the local AS number to use to establish a BGP session with
	// the default namespace.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN uint32 `json:"asn,omitempty"`

	// HostASN is the expected AS number for a BGP speaking component running in
	// the default network namespace. Either HostASN or HostType must be set.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	HostASN uint32 `json:"hostasn,omitempty"`

	// HostType is the AS type of the BGP speaking component running in the
	// default network namespace. Either HostASN or HostType must be set.
	// +kubebuilder:validation:Enum=external;internal
	// +optional
	HostType string `json:"hosttype,omitempty"`

	// NextHopSelf forces the next-hop to be set to this router, for eBGP and iBGP alike.
	// +optional
	NextHopSelf bool `json:"nexthopself,omitempty"`

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
