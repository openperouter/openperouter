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

const (
	LinuxBridge = "LinuxBridge"
	OVSBridge   = "OVSBridge"

	// RoutingDomainTypeL3VNI selects an L3VNI as the routing domain provider.
	RoutingDomainTypeL3VNI = "L3VNI"

	// RoutingDomainTypeL3VPN selects an L3VPN as the routing domain provider.
	RoutingDomainTypeL3VPN = "L3VPN"
)

// L2VNISpec defines the desired state of VNI.
// +kubebuilder:validation:XValidation:rule="!has(self.gatewayIPs) || size(self.gatewayIPs) == 0 || has(self.routingDomain)",message="gatewayIPs cannot be set without routingDomain"
type L2VNISpec struct {
	// nodeSelector specifies which nodes this L2VNI applies to.
	// If empty or not specified, applies to all nodes.
	// Multiple L2VNIs can match the same node.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// routingDomain optionally attaches this L2VNI to a routing domain
	// provided by a backing resource (L3VNI or L3VPN). When omitted, the
	// L2VNI is a disconnected overlay (east-west L2 only, no VRF, no
	// gateway).
	// +optional
	RoutingDomain *RoutingDomain `json:"routingDomain,omitempty"`

	// vni is the VXLan VNI to be used
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=16777215
	// +required
	VNI int32 `json:"vni,omitempty"`

	// vxlanPort is the port to be used for VXLan encapsulation.
	// +default=4789
	// +optional
	VXLanPort *int32 `json:"vxlanPort,omitempty"`

	// underlayAddressFamily selects which VTEP address family to use for this VNI's
	// VXLAN interface. When omitted, defaults to the available family in the underlay
	// (IPv4 preferred in dual-stack).
	// +kubebuilder:validation:Enum=IPv4;IPv6
	// +optional
	UnderlayAddressFamily *string `json:"underlayAddressFamily,omitempty"`

	// hostMaster is the interface on the host the veth should be attached to.
	// If not set, the host veth will not be attached to any interface and it must be
	// attached manually (or by some other means). This is useful if another controller
	// is leveraging the host interface for the VNI.
	// +optional
	HostMaster *HostMaster `json:"hostMaster,omitempty"`

	// gatewayIPs is a list of IP addresses in CIDR notation for the
	// distributed anycast gateway on this L2 segment's bridge
	// (Integrated Routing and Bridging interface). It is a property of
	// the L2 segment itself, so it lives on the L2VNI rather than
	// inside the routing-domain reference.
	// Maximum of 2 addresses are allowed. If 2 addresses are provided, one must be IPv4 and one must be IPv6.
	// +optional
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="GatewayIPs cannot be changed"
	// +listType=atomic
	GatewayIPs []string `json:"gatewayIPs,omitempty"`
}

// RoutingDomain is a discriminated union over the resource kinds that can
// provide a routing domain. Exactly one sub-struct must match the type
// discriminator.
// +union
// +kubebuilder:validation:XValidation:rule="self.type != 'L3VNI' || (has(self.l3vni) && !has(self.l3vpn))",message="type L3VNI requires l3vni to be set and l3vpn to be unset"
// +kubebuilder:validation:XValidation:rule="self.type != 'L3VPN' || (has(self.l3vpn) && !has(self.l3vni))",message="type L3VPN requires l3vpn to be set and l3vni to be unset"
type RoutingDomain struct {
	// type selects the kind of resource that provides this routing domain.
	// +kubebuilder:validation:Enum=L3VNI;L3VPN
	// +required
	// +unionDiscriminator
	Type string `json:"type,omitempty"`

	// l3vni references the L3VNI (metadata.name) in the same namespace that
	// provides the routing domain for this L2VNI.
	// +optional
	L3VNI *L3VNIReference `json:"l3vni,omitempty"`

	// l3vpn references the L3VPN (metadata.name) in the same namespace that
	// provides the routing domain for this L2VNI.
	// +optional
	L3VPN *L3VPNReference `json:"l3vpn,omitempty"`
}

// L3VNIReference references an L3VNI by name.
type L3VNIReference struct {
	// name is the metadata.name of the L3VNI in the same namespace.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name,omitempty"`
}

// L3VPNReference references an L3VPN by name.
type L3VPNReference struct {
	// name is the metadata.name of the L3VPN in the same namespace.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name,omitempty"`
}

// BridgeLifecycle determines how the bridge is provisioned.
// +kubebuilder:validation:Enum=Managed;External
type BridgeLifecycle string

const (
	// BridgeLifecycleManaged means the controller creates and owns the
	// bridge, named br-hs-<VNI>, and deletes it when the L2VNI is removed.
	BridgeLifecycleManaged BridgeLifecycle = "Managed"

	// BridgeLifecycleExternal means the user provides a pre-existing bridge
	// via the Name field. The controller does not create or delete it; only
	// veth ports are attached/detached.
	BridgeLifecycleExternal BridgeLifecycle = "External"
)

// LinuxBridgeConfig contains configuration for Linux bridge type.
// +kubebuilder:validation:XValidation:rule="(self.?name.orValue(\"\") != \"\") != (self.?lifecycle.orValue(\"\") == 'Managed')",message="name must be set when lifecycle is External, and must not be set when it is Managed."
// +kubebuilder:validation:XValidation:rule="!has(oldSelf) || self.lifecycle == oldSelf.lifecycle",message="lifecycle is immutable once set"
type LinuxBridgeConfig struct {
	// lifecycle determines if the bridge is managed by the controller or
	// provided by the user.
	// +required
	Lifecycle BridgeLifecycle `json:"lifecycle,omitempty"`

	// name of the Linux bridge interface. Required when lifecycle is
	// External, and must be omitted when it is Managed, in which case the
	// bridge is named br-hs-<VNI>.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +optional
	Name *string `json:"name,omitempty"`
}

// OVSBridgeConfig contains configuration for OVS bridge type.
// +kubebuilder:validation:XValidation:rule="(self.?name.orValue(\"\") != \"\") != (self.?lifecycle.orValue(\"\") == 'Managed')",message="name must be set when lifecycle is External, and must not be set when it is Managed."
// +kubebuilder:validation:XValidation:rule="!has(oldSelf) || self.lifecycle == oldSelf.lifecycle",message="lifecycle is immutable once set"
type OVSBridgeConfig struct {
	// lifecycle determines if the OVS bridge is managed by the controller or
	// provided by the user.
	// +required
	Lifecycle BridgeLifecycle `json:"lifecycle,omitempty"`

	// name of the OVS bridge interface. Required when lifecycle is
	// External, and must be omitted when it is Managed, in which case the
	// bridge is named br-hs-<VNI>.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +optional
	Name *string `json:"name,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="(self.type == 'LinuxBridge' && has(self.linuxBridge) && !has(self.ovsBridge)) || (self.type == 'OVSBridge' && has(self.ovsBridge) && !has(self.linuxBridge))",message="type/config mismatch: 'LinuxBridge' requires linuxBridge field, 'OVSBridge' requires ovsBridge field"
type HostMaster struct {
	// type of the host interface. Supported values: "LinuxBridge", "OVSBridge".
	// +kubebuilder:validation:Enum=LinuxBridge;OVSBridge
	// +required
	Type string `json:"type,omitempty"`

	// linuxBridge configuration. Must be set when Type is "LinuxBridge".
	// +optional
	LinuxBridge *LinuxBridgeConfig `json:"linuxBridge,omitempty"`

	// ovsBridge configuration. Must be set when Type is "OVSBridge".
	// +optional
	OVSBridge *OVSBridgeConfig `json:"ovsBridge,omitempty"`
}

// VNIStatus defines the observed state of VNI.
type L2VNIStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-l2vni,mutating=false,failurePolicy=fail,groups=network.openperouter.io,resources=l2vnis,versions=v1alpha1,name=l2vnivalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// L2VNI represents a VXLan VNI to receive EVPN type 2 routes
// from.
type L2VNI struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// spec defines the desired state of L2VNI.
	// +required
	Spec L2VNISpec `json:"spec,omitzero"`
	// status defines the observed state of L2VNI.
	// +optional
	Status *L2VNIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VNIList contains a list of VNI.
type L2VNIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []L2VNI `json:"items"`
}
