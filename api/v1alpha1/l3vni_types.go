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

// L3VNISpec defines the desired state of VNI.
// +kubebuilder:validation:XValidation:rule="!has(self.hostsession) || !has(self.hostsession.hostasn) || self.hostsession.hostasn != self.hostsession.asn",message="hostASN must be different from asn"
type L3VNISpec struct {
	// nodeSelector specifies which nodes this L3VNI applies to.
	// If empty or not specified, applies to all nodes.
	// Multiple L3VNIs can match the same node.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// vrf is the name of the linux VRF to be used inside the PERouter namespace.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +required
	VRF *string `json:"vrf,omitempty"`

	// vni is the VXLan VNI to be used
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	VNI *int64 `json:"vni,omitempty"`

	// vxlanport is the port to be used for VXLan encapsulation.
	// +default=4789
	// +optional
	VXLanPort *int32 `json:"vxlanport,omitempty"`

	// hostsession is the configuration for the host session.
	// +optional
	HostSession *HostSession `json:"hostsession,omitempty"`

	// exportRTs are the Route Targets to be used for exporting routes.
	// RouteTarget defines a BGP Extended Community for route filtering.
	// +optional
	// +listType=atomic
	ExportRTs []string `json:"exportRTs,omitempty"`

	// importRTs are the Route Targets to be used for importing routes.
	// RouteTarget defines a BGP Extended Community for route filtering.
	// +optional
	// +listType=atomic
	ImportRTs []string `json:"importRTs,omitempty"`
}

// L3VNIStatus defines the observed state of L3VNI.
type L3VNIStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-l3vni,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=l3vnis,versions=v1alpha1,name=l3vnivalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// L3VNI represents a VXLan L3VNI to receive EVPN type 5 routes
// from.
type L3VNI struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of L3VNI.
	// +required
	Spec L3VNISpec `json:"spec,omitzero"`
	// status defines the observed state of L3VNI.
	// +optional
	Status *L3VNIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// L3VNIList contains a list of L3VNI.
type L3VNIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []L3VNI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&L3VNI{}, &L3VNIList{})
}
