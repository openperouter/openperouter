// SPDX-License-Identifier:Apache-2.0

package v1alpha1

// Neighbor represents a BGP Neighbor we want FRR to connect to.
// +kubebuilder:validation:XValidation:rule="has(self.asn) || has(self.type)",message="either ASN or Type must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.asn) || !has(self.type)",message="ASN and Type cannot be set together"
// +kubebuilder:validation:XValidation:rule="has(self.holdTimeSeconds) == has(self.keepaliveTimeSeconds)",message="holdTimeSeconds and keepaliveTimeSeconds must be both set or both unset"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || self.holdTimeSeconds == 0 || self.holdTimeSeconds >= 3",message="holdTimeSeconds must be 0 or >=3"
// +kubebuilder:validation:XValidation:rule="!has(self.holdTimeSeconds) || !has(self.keepaliveTimeSeconds) || self.keepaliveTimeSeconds <= self.holdTimeSeconds",message="keepaliveTimeSeconds must be lower than or equal to holdTimeSeconds"
// +kubebuilder:validation:XValidation:rule="has(self.address) || has(self.interface) || has(self.listenRange)",message="Either a valid Address, Interface name or ListenRange must be provided for Neighbor"
// +kubebuilder:validation:XValidation:rule="!has(self.address) || !has(self.interface)",message="Address and Interface cannot be set together for Neighbor"
// +kubebuilder:validation:XValidation:rule="!has(self.listenRange) || !has(self.address)",message="listenRange and address are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!has(self.listenRange) || !has(self.interface)",message="listenRange and interface are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.addressFamilies) && self.addressFamilies.exists(af, has(af.properties) && af.properties.exists(o, o.type == 'routeReflectorClient'))) || (has(self.type) && self.type == 'internal')",message="routeReflectorClient requires type internal"
type Neighbor struct {
	// asn is the AS number of the neighbor. Either ASN or Type must be set.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	ASN *int64 `json:"asn,omitempty"`

	// type is the AS type of the neighbor. Either ASN or Type must be set.
	// +kubebuilder:validation:Enum=external;internal
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

	// listenRange is a CIDR range for dynamic neighbor acceptance via FRR
	// bgp listen range. When set, the hostcontroller generates a
	// "bgp listen range <listenRange> peer-group <name>" stanza instead of
	// an explicit neighbor statement. Mutually exclusive with address and
	// interface.
	// +kubebuilder:validation:XValidation:rule="isCIDR(self)",message="listenRange must be a valid CIDR"
	// +kubebuilder:validation:XValidation:rule="!isCIDR(self) || cidr(self).ip().family() != 6 || !cidr(self).ip().isLinkLocalUnicast()",message="listenRange must not be an IPv6 link-local range"
	// +kubebuilder:validation:MaxLength:=43
	// +kubebuilder:validation:MinLength:=1
	// +optional
	ListenRange *string `json:"listenRange,omitempty"`

	// port is the port to dial when establishing the session.
	// Defaults to 179.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=16384
	Port *int32 `json:"port,omitempty"`

	// password to be used for establishing the BGP session.
	// Password and PasswordSecret are mutually exclusive.
	// +optional
	Password *string `json:"password,omitempty"`

	// passwordSecret is name of the authentication secret for the neighbor.
	// the secret must be of type "kubernetes.io/basic-auth", and created in the
	// same namespace as the perouter daemon. The password is stored in the
	// secret as the key "password".
	// Password and PasswordSecret are mutually exclusive.
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

	// ebgpMultiHop indicates if the BGPPeer is multi-hops away.
	// +optional
	EBGPMultiHop *bool `json:"ebgpMultiHop,omitempty"`

	// bfd defines the BFD configuration for the BGP session.
	// +optional
	BFD *BFDSettings `json:"bfd,omitempty"`

	// addressFamilies specifies the BGP address families that shall be enabled
	// for this BGP neighbor.
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
	// +kubebuilder:validation:MaxItems:=3
	// +optional
	// +listType=map
	// +listMapKey=type
	AddressFamilies []NeighborAddressFamily `json:"addressFamilies,omitempty"`
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

	// echoInterval configures the minimal echo receive transmission
	// interval that this system is capable of handling in milliseconds.
	// Defaults to 50ms
	// +kubebuilder:validation:Maximum:=60000
	// +kubebuilder:validation:Minimum:=10
	// +optional
	EchoInterval *int32 `json:"echoInterval,omitempty"`

	// echoMode enables or disables the echo transmission mode.
	// This mode is disabled by default, and not supported on multi
	// hops setups.
	// +optional
	EchoMode *bool `json:"echoMode,omitempty"`

	// passiveMode marks session as passive: a passive session will not
	// attempt to start the connection and will wait for control packets
	// from peer before it begins replying.
	// +optional
	PassiveMode *bool `json:"passiveMode,omitempty"`

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
	// +kubebuilder:validation:Enum:=ipv4unicast;ipv6unicast;evpn
	// +required
	Type string `json:"type,omitempty"`

	// properties is the set of optional per-address-family features for this
	// neighbor (for example, marking the neighbor as a route reflector client
	// in this address family).
	// Note: MaxItems=8 is arbitrarily chosen to keep total CEL cost low.
	// +optional
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=type
	Properties []NeighborAddressFamilyProperty `json:"properties,omitempty"`
}

// NeighborAddressFamilyPropertyType defines an optional feature on a neighbor
// address family.
// +kubebuilder:validation:Enum=routeReflectorClient
type NeighborAddressFamilyPropertyType string

const (
	// NeighborAddressFamilyPropertyRouteReflectorClient marks the neighbor as a
	// route reflector client of the local router in this address family (RFC 4456).
	NeighborAddressFamilyPropertyRouteReflectorClient NeighborAddressFamilyPropertyType = "routeReflectorClient"
)

// NeighborAddressFamilyProperty is an optional feature applied to a neighbor
// address family. The type field selects the property; typed sub-fields hold
// parameters for properties that require them.
type NeighborAddressFamilyProperty struct {
	// type selects the property.
	// +required
	Type NeighborAddressFamilyPropertyType `json:"type,omitempty"`
}
