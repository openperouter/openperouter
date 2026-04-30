// SPDX-License-Identifier:Apache-2.0

package v1alpha1

// Neighbor represents a BGP Neighbor we want FRR to connect to.
// +kubebuilder:validation:XValidation:rule="has(self.asn) || has(self.type)",message="either ASN or Type must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.asn) || !has(self.type)",message="ASN and Type cannot be set together"
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

	// address is the IP address to establish the session with.
	// +kubebuilder:validation:MinLength=1
	// +required
	Address string `json:"address,omitempty"`

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

	// minimumTtl configures, for multi hop sessions only, the minimum
	// expected TTL for an incoming BFD control packet.
	// +kubebuilder:validation:Maximum:=254
	// +kubebuilder:validation:Minimum:=1
	// +optional
	MinimumTTL *int32 `json:"minimumTtl,omitempty"`
}
