/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnderlaySpec defines the desired state of Underlay.
// +kubebuilder:validation:XValidation:rule="!has(self.evpn) || !has(self.srv6)",message="cannot have both EVPN and SRV6 VPN at the same time"
type UnderlaySpec struct {
	// NodeSelector specifies which nodes this Underlay applies to.
	// If empty or not specified, applies to all nodes (backward compatible).
	// Multiple Underlays with overlapping node selectors will be rejected.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// ASN is the local AS number to use for the session with the TOR switch.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN uint32 `json:"asn,omitempty"`

	// RouterIDCIDR is the ipv4 cidr to be used to assign a different routerID on each node.
	// +kubebuilder:default="10.0.0.0/24"
	// +optional
	RouterIDCIDR string `json:"routeridcidr,omitempty"`

	// Neighbors is the list of external neighbors to peer with.
	// +kubebuilder:validation:MinItems=1
	Neighbors []Neighbor `json:"neighbors,omitempty"`

	// Nics is the list of physical nics to move under the PERouter namespace to connect
	// to external routers. This field is optional when using Multus networks for TOR connectivity.
	// +kubebuilder:validation:items:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:items:MaxLength=15
	Nics []string `json:"nics,omitempty"`

	// +optional
	EVPN *EVPNConfig `json:"evpn,omitempty"`

	// ISIS holds the ISIS configurations for the underlay, one per ISIS process.
	// +kubebuilder:validation:MaxItems=10
	ISIS []ISISConfig `json:"isis,omitempty"`

	// SRV6 holds the SRV6 configuration.
	// +optional
	SRV6 *SRV6Config `json:"srv6,omitempty"`
}

// EVPNConfig contains EVPN-VXLAN configuration for the underlay.
// +kubebuilder:validation:XValidation:rule="(self.?vtepcidr.orValue(\"\") != \"\") != (self.?vtepInterface.orValue(\"\") != \"\")",message="exactly one of vtepcidr or vtepInterface must be specified"
type EVPNConfig struct {
	// VTEPCIDR is CIDR to be used to assign IPs to the local VTEP on each node.
	// A loopback interface will be created with an IP derived from this CIDR.
	// Mutually exclusive with vtepInterface.
	// +optional
	VTEPCIDR string `json:"vtepcidr,omitempty"`

	// VTEPInterface is the name of an existing interface to use as the VTEP source.
	// The interface must already have an IP address configured that will be used
	// as the VTEP IP. Mutually exclusive with vtepcidr.
	// The ToR must advertise the interface IP into the fabric underlay
	// (e.g. via redistribute connected) so that the VTEP address is reachable
	// from other leaves.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +optional
	VTEPInterface string `json:"vtepInterface,omitempty"`
}

// ISISConfig contains ISIS configuration for the underlay.
type ISISConfig struct {
	// Name holds the name of the ISIS process.
	// +required
	Name string `json:"name"`
	// Net holds the ISIS net address.
	// TODO: right now, the last part of the system ID is offset by routerID .. however, is there a more elegant solution??
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:MaxItems=3
	// +required
	Net []ISISNet `json:"net"`
	// Type configures the ISIS type, system wide. It defaults to level-1-2 unless specified otherwise.
	// +kubebuilder:validation:Enum=1;2
	// +optional
	Type uint32 `json:"type,omitempty"`
	// Interfaces holds ISIS interface level configuration.
	// +kubebuilder:validation:MaxItems=1000
	// +optional
	Interfaces []ISISInterface `json:"interfaces,omitempty"`
}

// ISISNet represents a single ISIS net address.
// +kubebuilder:validation:MinLength=25
// +kubebuilder:validation:MaxLength=25
// +kubebuilder:validation:XValidation:rule=`self.matches('^[0-9a-f]{2}\\.([0-9a-f]{4}\\.){4}[0-9a-f]{2}$')`,message="Provided net address must match canonical format"
type ISISNet string

// ISISInterface holds ISIS interface level configuration.
type ISISInterface struct {
	// Name of the interface that these settings shall apply to.
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[^\\/:\\s]+$')`,message="Interface must not contain /, :, or whitespace"
	// +kubebuilder:validation:XValidation:rule=`self != '.' && self != '..'`,message="Interface cannot be . or .."
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength:=1
	// +required
	Name string `json:"name"`
	// +optional
	// ipv4 isis <name> enabled
	IPv4 bool `json:"ipv4,omitempty"`
	// ipv6 isis <name> enabled
	// +optional
	IPv6 bool `json:"ipv6,omitempty"`
}

// SRV6Config contains SRV6 configuration for the underlay.
type SRV6Config struct {
	// Source specifies the source for the SRV6 VPN.
	// +required
	Source SRV6Source `json:"source"`

	// Locator defines the locator for this SRV6 VPN.
	// +required
	Locator SRV6Locator `json:"locator"`
}

// +kubebuilder:validation:XValidation:rule="has(self.cidr) || has(self.interface)",message="exactly one of cidr or interface must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.cidr) || !has(self.interface)",message="exactly one of cidr or interface must be specified"
type SRV6Source struct {
	// CIDR to assign IPs to the local VTEP on each node from.
	// The IPv6 address will be assigned to the loopback interface.
	// Mutually exclusive with interface.
	// +kubebuilder:validation:XValidation:rule="isCIDR(self) && cidr(self).ip().family() == 6",message="cidr must be an IPv6 CIDR"
	// +kubebuilder:validation:MaxLength:=43
	// +kubebuilder:validation:MinLength:=1
	// +optional
	CIDR string `json:"cidr,omitempty"`

	// Interface is the name of an existing interface to use as the VTEP source.
	// The interface must already have an IP address configured that will be used
	// as the VTEP IP. Mutually exclusive with cidr.
	// The ToR must advertise the interface IP into the fabric underlay
	// (e.g. via redistribute connected) so that the VTEP address is reachable
	// from other leaves.
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[^\\/:\\s]+$')`,message="interface must not contain /, :, or whitespace"
	// +kubebuilder:validation:XValidation:rule=`self != '.' && self != '..'`,message="interface cannot be . or .."
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength:=1
	// +optional
	Interface string `json:"interface,omitempty"` // https://regex101.com/r/RlniVP/2 see kernel bool dev_valid_name(...)
}

type SRV6Locator struct {
	// Name of the Locator.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +kubebuilder:validation:MinLength:=1
	// +required
	Name string `json:"name"`

	// Prefix is CIDR to be used for the locator.
	// +kubebuilder:validation:XValidation:rule="isCIDR(self) && cidr(self).ip().family() == 6",message="prefix must be an IPv6 CIDR"
	// +kubebuilder:validation:MaxLength:=43
	// +kubebuilder:validation:MinLength:=1
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// Format specifies the format of the locator. Defaults to usid-f3216
	// +kubebuilder:validation:Enum=usid-f3216
	// +optional
	Format string `json:"format,omitempty"`
}

// UnderlayStatus defines the observed state of Underlay.
type UnderlayStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-underlay,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=underlays,versions=v1alpha1,name=underlayvalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// Underlay is the Schema for the underlays API.
type Underlay struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UnderlaySpec   `json:"spec,omitempty"`
	Status UnderlayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UnderlayList contains a list of Underlay.
type UnderlayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Underlay `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Underlay{}, &UnderlayList{})
}
