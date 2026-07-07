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
// +kubebuilder:validation:XValidation:rule="!has(self.srv6) || has(self.isis)",message="SRv6 can only be configured if isis is set"
// +kubebuilder:validation:XValidation:rule="!has(self.srv6) || (has(self.tunnelEndpoint) && has(self.tunnelEndpoint.cidrs) && self.tunnelEndpoint.cidrs.exists(c, cidr(c).ip().family() == 6))",message="SRv6 requires at least one IPv6 CIDR in tunnelEndpoint.cidrs"
type UnderlaySpec struct {
	// nodeSelector specifies which nodes this Underlay applies to.
	// If empty or not specified, applies to all nodes (backward compatible).
	// Multiple Underlays with overlapping node selectors will be rejected.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// asn is the local AS number to use for the session with the TOR switch.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN int64 `json:"asn,omitempty"`

	// routeridcidr is the ipv4 cidr to be used to assign a different routerID on each node.
	// +default="10.0.0.0/24"
	// +optional
	RouterIDCIDR *string `json:"routeridcidr,omitempty"`

	// Note: MaxItems:=128 is arbitrarily chosen to keep total CEL cost low
	// Note: kubeapilinter complained about 'the struct has no required fields', but CEL enforces either/or choices
	// for Address and Interface.

	// neighbors is the list of external BGP neighbors to peer with.
	// Multiple neighbors are supported for connecting to multiple TOR switches
	// or establishing redundant BGP sessions. Each neighbor address must be unique.
	// At least one neighbor is required.
	// +kubebuilder:validation:MinItems:=1
	// +kubebuilder:validation:MaxItems:=128
	// +required
	// +listType=atomic
	Neighbors []Neighbor `json:"neighbors,omitempty"` //nolint:kubeapilinter

	// nics is the list of physical nics to move under the PERouter namespace to connect
	// to external routers. At least one NIC is required.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:items:MaxLength=15
	// +required
	// +listType=atomic
	Nics []string `json:"nics,omitempty"`

	// tunnelEndpoint contains tunnel endpoint configuration for the underlay.
	// +optional
	TunnelEndpoint *TunnelEndpointConfig `json:"tunnelEndpoint,omitempty"`

	// gracefulRestart configures BGP Graceful Restart behaviour.
	// When set, FRR advertises GR capability and preserves forwarding
	// state across restarts so that peers keep stale routes active.
	// Omit to disable graceful restart.
	// +optional
	GracefulRestart *GracefulRestartConfig `json:"gracefulRestart,omitempty"`

	// isis holds the ISIS configuration for the underlay.
	// +optional
	ISIS *ISISConfig `json:"isis,omitempty"`

	// srv6 holds the SRv6 configuration. Requires ISIS or Neighbors configuration.
	// +optional
	SRV6 *SRV6Config `json:"srv6,omitempty"`
}

// GracefulRestartConfig holds BGP Graceful Restart parameters.
// Its presence on the Underlay enables graceful restart.
type GracefulRestartConfig struct {
	// restartTimeSeconds is the time in seconds that the restarting router
	// requests its peers to preserve routes. Peers will wait this long
	// before removing stale routes.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4095
	// +default=120
	// +optional
	RestartTimeSeconds *int64 `json:"restartTimeSeconds,omitempty"`

	// stalePathTimeSeconds is the time in seconds that stale paths from a
	// restarting peer are retained locally.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4095
	// +default=360
	// +optional
	StalePathTimeSeconds *int64 `json:"stalePathTimeSeconds,omitempty"`
}

// TunnelEndpointConfig contains tunnel endpoint configuration for the underlay.
type TunnelEndpointConfig struct {
	// cidrs is a list of CIDRs to be used to assign IPs to the local tunnel endpoint on
	// each node. IPs derived from these CIDRs will be assigned to the local loopback.
	// At least one IPv4 or IPv6 CIDR is required. At most one of each family may be specified.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c))",message="all entries must be valid CIDRs"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 4).size() <= 1",message="at most one IPv4 CIDR is allowed"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 6).size() <= 1",message="at most one IPv6 CIDR is allowed"
	// +listType=atomic
	// +required
	CIDRs []string `json:"cidrs,omitempty"`
}

// ISISConfig contains ISIS configuration for the underlay.
type ISISConfig struct {
	// baseNet holds the ISIS NET address.
	// The configured Net address is a base address which is offset by the node index of each node.
	// Only accepts the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance
	// with the U.S. GOSIP version 2.0 for a total of 10 bytes.
	// +required
	BaseNet ISISNet `json:"baseNet,omitempty"`
	// features enables ISIS boolean features.
	// Supported features are:
	// advertisePassiveOnly: configures ISIS to advertise only prefixes that belong to passive interfaces.
	// +kubebuilder:validation:MaxItems:=32
	// +listType=atomic
	// +optional
	Features []ISISFeature `json:"features,omitempty"`
	// interfaces holds additional ISIS interface level configuration and / or per
	// interface overrides. By default, OpenPERouter enables IPv6 on all required
	// interfaces with default settings.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems:=128
	// +optional
	Interfaces []ISISInterface `json:"interfaces,omitempty"`
	// level configures the ISIS type, system wide. It defaults to level-1-2 unless specified otherwise.
	// +kubebuilder:validation:Enum:=1;2
	// +optional
	Level *int32 `json:"level,omitempty"`
}

// ISISNet represents a single ISIS NET address.
// Only accepts the simplified NSAP format with a fixed AreaID length of 3 bytes and a 6 byte SystemID in compliance
// with the U.S. GOSIP version 2.0 for a total of 10 bytes.
// +kubebuilder:validation:MinLength:=25
// +kubebuilder:validation:MaxLength:=25
// +kubebuilder:validation:XValidation:rule=`self.matches('^[0-9a-f]{2}\\.([0-9a-f]{4}\\.){4}[0-9a-f]{2}$')`,message="Provided net address must match canonical format"
type ISISNet string

// ISISFeature represents a single ISIS feature.
// +kubebuilder:validation:MinLength:=1
// +kubebuilder:validation:MaxLength:=128
// +kubebuilder:validation:Enum:=advertisePassiveOnly
type ISISFeature string

// ISISInterface holds ISIS interface level configuration.
type ISISInterface struct {
	// name of the interface that these settings shall apply to.
	// +kubebuilder:validation:XValidation:rule=`self.matches('^[^\\/:\\s]+$')`,message="Interface must not contain /, :, or whitespace"
	// +kubebuilder:validation:XValidation:rule=`self != '.' && self != '..'`,message="Interface cannot be . or .."
	// +kubebuilder:validation:MaxLength:=15
	// +kubebuilder:validation:MinLength:=1
	// +required
	Name string `json:"name,omitempty"`
	// ipFamily configures which address families ISIS is enabled for on this interface.
	// +optional
	IPFamily *IPFamily `json:"ipFamily,omitempty"`
	// features enables ISIS interface boolean features.
	// Supported features are:
	// passive: configures ISIS passive mode on this interface.
	// +kubebuilder:validation:MaxItems:=32
	// +listType=atomic
	// +optional
	Features []ISISInterfaceFeature `json:"features,omitempty"`
}

// ISISInterfaceFeature represents a single ISIS feature of an ISIS interface.
// +kubebuilder:validation:MinLength:=1
// +kubebuilder:validation:MaxLength:=128
// +kubebuilder:validation:Enum:=passive
type ISISInterfaceFeature string

// IPFamily specifies which address families are enabled.
// +kubebuilder:validation:Enum:=ipv4;ipv6;dualstack
type IPFamily string

const (
	IPFamilyIPv4      IPFamily = "ipv4"
	IPFamilyIPv6      IPFamily = "ipv6"
	IPFamilyDualStack IPFamily = "dualstack"
)

// SRV6Config contains SRV6 configuration for the underlay.
type SRV6Config struct {
	// locator defines the locator for this SRv6 VPN.
	// +required
	Locator SRV6Locator `json:"locator,omitzero"`
}

// SRV6Locator holds the configuration of a locator for SRv6.
type SRV6Locator struct {
	// basePrefix is the CIDR to be used for the locator, offset by the router index.
	// +kubebuilder:validation:XValidation:rule="isCIDR(self) && cidr(self).ip().family() == 6",message="prefix must be an IPv6 CIDR"
	// +kubebuilder:validation:MaxLength:=43
	// +kubebuilder:validation:MinLength:=1
	// +required
	BasePrefix string `json:"basePrefix,omitempty"`

	// format specifies the format of the locator. Defaults to usid-f3216
	// +kubebuilder:validation:MaxLength:=40
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:Enum:=usid-f3216
	// +required
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
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Underlay.
	// +required
	Spec UnderlaySpec `json:"spec,omitzero,omitempty"`
	// status defines the observed state of Underlay.
	// +optional
	Status *UnderlayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UnderlayList contains a list of Underlay.
type UnderlayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Underlay `json:"items"`
}
