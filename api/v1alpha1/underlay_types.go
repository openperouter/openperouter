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
	// ASN is the local AS number to use for the session with the TOR switch.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	ASN uint32 `json:"asn,omitempty"`

	// VTEPCIDR is CIDR to be used to assign IPs to the local VTEP on each node.
	VTEPCIDR string `json:"vtepcidr,omitempty"`

	// Neighbors is the list of external neighbors to peer with.
	Neighbors []Neighbor `json:"neighbors,omitempty"`

	// Nics is the list of physical nics to move under the PERouter namespace to connect
	// to external routers.
	Nics []string `json:"nic,omitempty"`
}

// UnderlayStatus defines the observed state of Underlay.
type UnderlayStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
