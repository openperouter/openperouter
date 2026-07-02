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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnderlaySpec defines the desired state of Underlay.
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

	// neighbors is the list of external BGP neighbors to peer with.
	// Note: MaxItems=128 is arbitrarily chosen to keep total CEL cost low
	// Note: kubeapilinter complained about 'the struct has no required fields', but CEL enforces either/or choices
	// for Address and Interface.
	// Multiple neighbors are supported for connecting to multiple TOR switches
	// or establishing redundant BGP sessions. Each neighbor address must be unique.
	// At least one neighbor is required.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=128
	// +required
	// +listType=atomic
	Neighbors []Neighbor `json:"neighbors,omitempty"` //nolint:kubeapilinter

	// interfaces is the list of interfaces the router uses for underlay
	// connectivity. Each entry is a discriminated union describing how the
	// interface is obtained. At least one interface is required.
	// +kubebuilder:validation:MinItems=1
	// +required
	// +listType=atomic
	Interfaces []UnderlayInterface `json:"interfaces,omitempty"`

	// tunnelEndpoint contains tunnel endpoint configuration for the underlay.
	// +optional
	TunnelEndpoint *TunnelEndpointConfig `json:"tunnelEndpoint,omitempty"`

	// gracefulRestart configures BGP Graceful Restart behaviour.
	// When set, FRR advertises GR capability and preserves forwarding
	// state across restarts so that peers keep stale routes active.
	// Omit to disable graceful restart.
	// +optional
	GracefulRestart *GracefulRestartConfig `json:"gracefulRestart,omitempty"`
}

// UnderlayInterfaceType selects how the router obtains an underlay link.
// It is the discriminator of the UnderlayInterface union and is designed to be
// extended with future modes.
// +kubebuilder:validation:Enum=NetworkDevice;CNI
type UnderlayInterfaceType string

const (
	// UnderlayInterfaceTypeNetworkDevice moves an existing host network device
	// into the router netns.
	UnderlayInterfaceTypeNetworkDevice UnderlayInterfaceType = "NetworkDevice"

	// UnderlayInterfaceTypeCNI invokes a CNI plugin to provision an interface
	// in the router netns.
	UnderlayInterfaceTypeCNI UnderlayInterfaceType = "CNI"
)

// UnderlayInterface defines how the router obtains a single underlay link.
// Exactly one of the sub-structs must match the type field.
// The union is designed to be extended with future modes
// for controller-provisioned interfaces.
//
// +union
// +kubebuilder:validation:XValidation:rule="has(self.networkDevice) == (self.type == 'NetworkDevice')",message="type/config mismatch: networkDevice must be set if and only if type is 'NetworkDevice'"
// +kubebuilder:validation:XValidation:rule="has(self.cniDevice) == (self.type == 'CNI')",message="type/config mismatch: cniDevice must be set if and only if type is 'CNI'"
type UnderlayInterface struct {
	// type selects how the router obtains this underlay link.
	// +required
	// +unionDiscriminator
	Type UnderlayInterfaceType `json:"type,omitempty"`

	// networkDevice moves an existing host network device into the router netns.
	// The device can be of any kind (physical NIC, bridge, macvlan, etc.).
	// Must be set when type is "NetworkDevice".
	// +optional
	NetworkDevice *NetworkDevice `json:"networkDevice,omitempty"`

	// cniDevice invokes a CNI plugin to provision an interface in the router
	// netns. IPAM is delegated to the CNI plugin. Must be set when type is
	// "CNI".
	// +optional
	CNIDevice *CNIDevice `json:"cniDevice,omitempty"`
}

// NetworkDevice moves an existing host network device into the router netns.
type NetworkDevice struct {
	// interfaceName is the name of the host network device to move into
	// the router netns.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +required
	InterfaceName string `json:"interfaceName,omitempty"`
}

// CNIConfigType selects the source of the CNI configuration.
// It is the discriminator of the CNIDevice union and is designed to be
// extended with future config sources (e.g. a NetworkAttachmentDefinition
// reference or a filesystem path).
// +kubebuilder:validation:Enum=RawConfig
type CNIConfigType string

const (
	// CNIConfigTypeRawConfig embeds the CNI config JSON directly in the spec.
	CNIConfigTypeRawConfig CNIConfigType = "RawConfig"
)

// CNIDevice invokes a CNI plugin to provision an interface in the router
// netns. The config source is a discriminated union — additional source
// variants can be added later if a concrete user need emerges.
//
// +union
// +kubebuilder:validation:XValidation:rule="has(self.rawConfig) == (self.type == 'RawConfig')",message="type/config mismatch: rawConfig must be set if and only if type is 'RawConfig'"
type CNIDevice struct {
	// type selects the source of the CNI configuration.
	// +required
	// +unionDiscriminator
	Type CNIConfigType `json:"type,omitempty"`

	// rawConfig embeds a CNI conf or conflist JSON blob directly in this
	// spec. Immutable once set: to change it, delete and recreate the
	// Underlay. Immutability is enforced by the validation webhook because
	// CEL transition rules cannot be evaluated inside atomic lists.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	RawConfig *apiextensionsv1.JSON `json:"rawConfig,omitempty"`

	// interfaceName is the name of the interface the CNI plugin creates
	// inside the router netns (passed as CNI_IFNAME). Defaults to "net1".
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +default="net1"
	// +optional
	InterfaceName *string `json:"interfaceName,omitempty"`

	// runtimeConfig is opaque JSON passed as capability arguments to the
	// CNI invocation. Only keys that the plugin declares in its
	// "capabilities" config block are forwarded; undeclared keys are
	// silently stripped. Well-known capabilities include ips, mac,
	// bandwidth, portMappings, ipRanges and deviceID.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	RuntimeConfig *apiextensionsv1.JSON `json:"runtimeConfig,omitempty"`
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
	// each node. A loopback interface will be created with IPs derived from
	// these CIDRs. At least one IPv4 or IPv6 CIDR is required. At most one of each family may be specified.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self.all(c, isCIDR(c))",message="all entries must be valid CIDRs"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 4).size() <= 1",message="at most one IPv4 CIDR is allowed"
	// +kubebuilder:validation:XValidation:rule="self.filter(c, isCIDR(c) && cidr(c).ip().family() == 6).size() <= 1",message="at most one IPv6 CIDR is allowed"
	// +listType=atomic
	// +required
	CIDRs []string `json:"cidrs,omitempty"`
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
