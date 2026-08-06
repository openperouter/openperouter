// SPDX-License-Identifier:Apache-2.0

package v1alpha1

// Neighbor represents a BGP Neighbor we want FRR to connect to.
// +kubebuilder:validation:XValidation:rule="has(self.asn) || has(self.type)",message="either ASN or Type must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.asn) || !has(self.type)",message="ASN and Type cannot be set together"
// +kubebuilder:validation:XValidation:rule="has(self.holdTimeSeconds) == has(self.keepaliveTimeSeconds)",message="holdTimeSeconds and keepaliveTimeSeconds must be both set or both unset"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || self.holdTimeSeconds == 0 || self.holdTimeSeconds >= 3",message="holdTimeSeconds must be 0 or >=3"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || !has(self.keepaliveTimeSeconds) || self.keepaliveTimeSeconds <= self.holdTimeSeconds",message="keepaliveTimeSeconds must be lower than or equal to holdTimeSeconds"
// +kubebuilder:validation:XValidation:rule="has(self.address) || has(self.interface)",message="Either a valid Address or Interface name must be provided for Neighbor"
// +kubebuilder:validation:XValidation:rule="!has(self.address) || !has(self.interface)",message="Address and Interface cannot be set together for Neighbor"
// +kubebuilder:validation:XValidation:rule="!has(self.interface) || !has(self.addressFamilies) || !self.addressFamilies.exists(f, f.type == 'ipv4vpn' || f.type == 'ipv6vpn')",message="ipv4vpn and ipv6vpn address families are not supported for unnumbered (interface) neighbors"
// +kubebuilder:validation:XValidation:rule="!has(self.address) || !has(self.addressFamilies) || ip(self.address).family() != 4 || !self.addressFamilies.exists(f, f.type == 'ipv4vpn' || f.type == 'ipv6vpn')",message="ipv4vpn and ipv6vpn address families require an IPv6 neighbor address"
type Neighbor struct {
	// asn is the AS number of the neighbor. Either ASN or Type must be set.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	ASN *int64 `json:"asn,omitempty"`

	// type is the AS type of the neighbor. Either ASN or Type must be set.
	// +kubebuilder:validation:Enum=External;Internal
	// +optional
	Type *string `json:"type,omitempty"`

	// address is the IP address to establish the session with. The IP address
	// can be either IPv4 or IPv6.
	// +kubebuilder:validation:XValidation:rule="isIP(self)",message="Address must be a valid IPv4 or IPv6 address"
	// +kubebuilder:validation:MaxLength:=39
	// +kubebuilder:validation:MinLength:=1
	// +optional
	Address *string `json:"address,omitempty"`

	// interface is the interface name for BGP unnumbered sessions. The session will be established via IPv6 link locals.
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[^\\/:\\s]+$')`,message="Interface must not contain /, :, or whitespace"
	// +kubebuilder:validation:XValidation:rule=`self != '.' && self != '..'`,message="Interface cannot be . or .."
	// +kubebuilder:validation:MaxLength:=15
	// +kubebuilder:validation:MinLength:=1
	// +optional
	Interface *string `json:"interface,omitempty"` // https://regex101.com/r/RlniVP/2 see kernel bool dev_valid_name(...)

	// port is the port to dial when establishing the session.
	// Defaults to 179.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=16384
	Port *int32 `json:"port,omitempty"`

	// Password is the resolved BGP session password, populated internally.
	// Not part of the CRD API — Kubernetes users must use PasswordSecret.
	// In systemd mode, set via the static config types.
	Password *string `json:"-"`

	// passwordSecret is name of the authentication secret for the neighbor.
	// The secret must be of type "kubernetes.io/basic-auth", and created in the
	// same namespace as the perouter daemon. The password is stored in the
	// secret as the key "password".
	// +optional
	PasswordSecret *string `json:"passwordSecret,omitempty"`

	// holdTimeSeconds is the requested BGP hold time in seconds, per RFC4271.
	// Defaults to 180.
	// +optional
	HoldTimeSeconds *int64 `json:"holdTimeSeconds,omitempty"`

	// keepaliveTimeSeconds is the requested BGP keepalive time in seconds, per RFC4271.
	// Defaults to 60.
	// +optional
	KeepaliveTimeSeconds *int64 `json:"keepaliveTimeSeconds,omitempty"`

	// connectTimeSeconds controls how long BGP waits between connection attempts to a neighbor, in seconds.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	ConnectTimeSeconds *int64 `json:"connectTimeSeconds,omitempty"`

	// properties is the set of optional session-level features for this
	// neighbor (e.g. ebgpMultiHop).
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems:=1
	Properties []NeighborProperty `json:"properties,omitempty"`

	// bfd defines the BFD configuration for the BGP session.
	// +optional
	BFD *BFDSettings `json:"bfd,omitempty"`

	// addressFamilies specifies the BGP address families that shall be enabled
	// for this BGP neighbor. evpn and ipv4vpn/ipv6vpn are mutually exclusive.
	// If ipv4vpn or ipv6vpn are set, the update source of this neighbor will
	// be set to the loopback's IPv6 address.
	// If addressFamilies is not provided or empty, the following defaults are
	// chosen:
	// For unnumbered neighbors:
	// - ipv4unicast
	// - ipv6unicast if passthrough is configured with IPv6 local CIDR
	// - evpn if L2VNIs or L3VNIs are present.
	// For IPv4 neighbors:
	// - ipv4unicast
	// - ipv6unicast if passthrough is configured with IPv6 local CIDR
	// - evpn if L2VNIs or L3VNIs are present.
	// For IPv6 neighbors:
	// - ipv4unicast if L2VNIs or L3VNIs are present, or if passthrough is configured with IPv4 local CIDR
	// - ipv6unicast
	// - evpn if L2VNIs or L3VNIs are present
	// - ipv4vpn if L3VPNs and SRv6 configuration are present.
	// - ipv6vpn if L3VPNs and SRv6 configuration are present.
	// +kubebuilder:validation:XValidation:rule="!(self.exists(f, f.type == 'evpn') && self.exists(f, f.type == 'ipv4vpn' || f.type == 'ipv6vpn'))",message="EVPN and IPv4VPN/IPv6VPN address families are mutually exclusive"
	// +kubebuilder:validation:MaxItems:=4
	// +optional
	// +listType=map
	// +listMapKey=type
	AddressFamilies []NeighborAddressFamily `json:"addressFamilies,omitempty"`
}

// BFDSessionMode selects whether the local system initiates the BFD session.
// +kubebuilder:validation:Enum=Active;Passive
type BFDSessionMode string

// NeighborPropertyType defines an optional feature on a Neighbor.
// The values are protocol / FRR configuration tokens and are kept verbatim so
// they map directly to the rendered stanzas.
// +kubebuilder:validation:Enum=ebgpMultiHop
type NeighborPropertyType string

const (
	// BFDSessionModeActive initiates the BFD session. This is the default
	// when sessionMode is omitted.
	BFDSessionModeActive BFDSessionMode = "Active"

	// BFDSessionModePassive waits for the peer to initiate the BFD session
	// before replying.
	BFDSessionModePassive BFDSessionMode = "Passive"

	// NeighborPropertyEBGPMultiHop enables eBGP multihop on the neighbor
	// session, rendered as "neighbor X ebgp-multihop [ttl]".
	NeighborPropertyEBGPMultiHop NeighborPropertyType = "ebgpMultiHop"
)

// EBGPMultiHopProperties holds parameters for the ebgpMultiHop property.
type EBGPMultiHopProperties struct {
	// ttl is the maximum number of hops for the eBGP multihop session.
	// When omitted, FRR defaults to 255.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	// +optional
	TTL *int32 `json:"ttl,omitempty"`
}

// NeighborProperty is an optional feature applied to a neighbor session. The
// type field selects the property; typed sub-fields hold parameters for
// properties that require them.
// +kubebuilder:validation:XValidation:rule="!has(self.ebgpMultiHop) || self.type == 'ebgpMultiHop'",message="ebgpMultiHop parameters can only be set when type is ebgpMultiHop"
type NeighborProperty struct {
	// type selects the property.
	// +required
	Type NeighborPropertyType `json:"type,omitempty"`

	// ebgpMultiHop holds parameters for the ebgpMultiHop property.
	// May only be set when type is ebgpMultiHop.
	// +optional
	EBGPMultiHop *EBGPMultiHopProperties `json:"ebgpMultiHop,omitempty"`
}

// BFDSettings defines the BFD configuration for a BGP session.
type BFDSettings struct {
	// receiveInterval is the minimum interval that this system is capable of
	// receiving control packets in milliseconds.
	// Defaults to 300ms.
	// +kubebuilder:validation:Maximum:=60000
	// +kubebuilder:validation:Minimum:=10
	// +optional
	ReceiveInterval *int32 `json:"receiveInterval,omitempty"`

	// transmitInterval is the minimum transmission interval (less jitter)
	// that this system wants to use to send BFD control packets in
	// milliseconds. Defaults to 300ms
	// +kubebuilder:validation:Maximum:=60000
	// +kubebuilder:validation:Minimum:=10
	// +optional
	TransmitInterval *int32 `json:"transmitInterval,omitempty"`

	// detectMultiplier configures the detection multiplier to determine
	// packet loss. The remote transmission interval will be multiplied
	// by this value to determine the connection loss detection timer.
	// +kubebuilder:validation:Maximum:=255
	// +kubebuilder:validation:Minimum:=2
	// +optional
	DetectMultiplier *int32 `json:"detectMultiplier,omitempty"`

	// sessionMode marks the session active or passive. Active (the default
	// when omitted) initiates the session. Passive waits for the peer to
	// initiate before replying (RFC 5880 Section 6.1).
	// +optional
	SessionMode *BFDSessionMode `json:"sessionMode,omitempty"`

	// minimumTTL configures, for multi hop sessions only, the minimum
	// expected TTL for an incoming BFD control packet.
	// +kubebuilder:validation:Maximum:=254
	// +kubebuilder:validation:Minimum:=1
	// +optional
	MinimumTTL *int32 `json:"minimumTTL,omitempty"`
}

// NeighborAddressFamily represents a single BGP address family configuration
// for a neighbor.
type NeighborAddressFamily struct {
	// type is the address family type.
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=11
	// +kubebuilder:validation:Enum:=ipv4unicast;ipv6unicast;evpn;ipv4vpn;ipv6vpn
	// +required
	Type string `json:"type,omitempty"`
}
