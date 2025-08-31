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

// L3SRV6Spec defines the desired state of VNI.
type L3SRV6Spec struct {
	// VRF is the name of the linux VRF to be used inside the PERouter namespace.
	// The field is optional, if not set it the name of the VNI instance will be used.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	VRF string `json:"vrf,omitempty"`

	RouteTargetAssignedNumber uint32 `json:"rtassignednumber,omitempty"`

	// HostSession is the configuration for the host session.
	HostSession HostSession `json:"hostsession"`
}

// L3SRV6Status defines the observed state of L3VNI.
type L3SRV6Status struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// L3SRV6 represents a VXLan L3VNI to receive EVPN type 5 routes
// from.
type L3SRV6 struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   L3SRV6Spec   `json:"spec"`
	Status L3SRV6Status `json:"status"`
}

// +kubebuilder:object:root=true

// L3SRV6List contains a list of L3VNI.
type L3SRV6List struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []L3SRV6 `json:"items"`
}

func init() {
	SchemeBuilder.Register(&L3SRV6{}, &L3VNIList{})
}
