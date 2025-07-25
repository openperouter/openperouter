// SPDX-License-Identifier:Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Neighbor represents a BGP Neighbor we want FRR to connect to.
// +kubebuilder:validation:XValidation:rule="!has(self.hostasn) || self.hostasn != self.asn",message="hostASN must be different from asn for eBGP"
type Neighbor struct {
	// ASN is the AS number to use for the local end of the session.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	ASN uint32 `json:"asn,omitempty"`

	// ASN is the expected AS number for a BGP speaking component running in
	// the default network namespace. If not set, the ASN field is going to be used.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	HostASN *uint32 `json:"hostasn,omitempty"`

	// Address is the IP address to establish the session with.
	Address string `json:"address"`

	// Port is the port to dial when establishing the session.
	// Defaults to 179.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=16384
	Port *uint16 `json:"port,omitempty"`

	// Password to be used for establishing the BGP session.
	// Password and PasswordSecret are mutually exclusive.
	// +optional
	Password string `json:"password,omitempty"`

	// PasswordSecret is name of the authentication secret for the neighbor.
	// the secret must be of type "kubernetes.io/basic-auth", and created in the
	// same namespace as the perouter daemon. The password is stored in the
	// secret as the key "password".
	// Password and PasswordSecret are mutually exclusive.
	// +optional
	PasswordSecret string `json:"passwordSecret,omitempty"`

	// HoldTime is the requested BGP hold time, per RFC4271.
	// Defaults to 180s.
	// +optional
	HoldTime *metav1.Duration `json:"holdTime,omitempty"`

	// KeepaliveTime is the requested BGP keepalive time, per RFC4271.
	// Defaults to 60s.
	// +optional
	KeepaliveTime *metav1.Duration `json:"keepaliveTime,omitempty"`

	// Requested BGP connect time, controls how long BGP waits between connection attempts to a neighbor.
	// +kubebuilder:validation:XValidation:message="connect time should be between 1 seconds to 65535",rule="duration(self).getSeconds() >= 1 && duration(self).getSeconds() <= 65535"
	// +kubebuilder:validation:XValidation:message="connect time should contain a whole number of seconds",rule="duration(self).getMilliseconds() % 1000 == 0"
	// +optional
	ConnectTime *metav1.Duration `json:"connectTime,omitempty"`

	// EBGPMultiHop indicates if the BGPPeer is multi-hops away.
	// +optional
	EBGPMultiHop bool `json:"ebgpMultiHop,omitempty"`
}
