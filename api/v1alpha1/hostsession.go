// SPDX-License-Identifier:Apache-2.0

package v1alpha1

// Host Session represents the leg between the router and the host.
// A BGP session is established over this leg.
// +kubebuilder:validation:XValidation:rule="has(self.hostASN) || has(self.hostType)",message="either HostASN or HostType must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.hostASN) || !has(self.hostType)",message="HostASN and HostType cannot be set together"
type HostSession struct {
	// asn is the local AS number to use to establish a BGP session with
	// the default namespace.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN int64 `json:"asn,omitempty"`

	// hostASN is the expected AS number for a BGP speaking component running in
	// the default network namespace. Either HostASN or HostType must be set.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	HostASN *int64 `json:"hostASN,omitempty"`

	// hostType is the AS type of the BGP speaking component running in the
	// default network namespace. Either HostASN or HostType must be set.
	// +kubebuilder:validation:Enum=External;Internal
	// +optional
	HostType *string `json:"hostType,omitempty"`

	// localCIDRs is the list of CIDRs for the veth pair connecting to the
	// default namespace. The router side uses the first usable IP of each CIDR.
	// At most one IPv4 and one IPv6 CIDR may be set; list order is not significant.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c))",message="all entries must be valid CIDRs"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 4).size() <= 1",message="at most one IPv4 CIDR is allowed"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 6).size() <= 1",message="at most one IPv6 CIDR is allowed"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="localCIDRs can't be changed"
	// +listType=atomic
	// +required
	LocalCIDRs []string `json:"localCIDRs,omitempty"`
}
