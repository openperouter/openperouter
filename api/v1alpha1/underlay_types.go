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

	EVPN *EVPNConfig `json:"evpn,omitempty"`
}

// EVPNConfig contains EVPN-VXLAN configuration for the underlay.
// +kubebuilder:validation:XValidation:rule="(self.?vtepcidr.orValue(\"\") != \"\") != (self.?vtepInterface.orValue(\"\") != \"\")",message="exactly one of vtepcidr or vtepInterface must be specified"
type EVPNConfig struct {
	// VTEPCIDR is CIDR to be used to assign IPs to the local VTEP on each node.
	// A loopback interface will be created with an IP derived from this CIDR.
	// Mutually exclusive with VTEPInterface.
	// +optional
	VTEPCIDR string `json:"vtepcidr,omitempty"`

	// VTEPInterface is the name of an existing interface to use as the VTEP source.
	// The interface must already have an IP address configured that will be used
	// as the VTEP IP. Mutually exclusive with VTEPCIDR.
	// The ToR must advertise the interface IP into the fabric underlay
	// (e.g. via redistribute connected) so that the VTEP address is reachable
	// from other leaves.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +optional
	VTEPInterface string `json:"vtepInterface,omitempty"`
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
