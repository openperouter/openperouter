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
type L3VNISpec struct {
	// VRF is the name of the linux VRF to be used inside the PERouter namespace.
	// The field is optional, if not set it the name of the VNI instance will be used.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +optional
	VRF *string `json:"vrf,omitempty"`

	// VNI is the VXLan VNI to be used
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	// +optional
	VNI uint32 `json:"vni,omitempty"`

	// VXLanPort is the port to be used for VXLan encapsulation.
	// +kubebuilder:default:=4789
	VXLanPort uint32 `json:"vxlanport,omitempty"`

	// HostSession is the configuration for the host session.
	// +optional
	HostSession *HostSession `json:"hostsession,omitempty"`
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
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   L3VNISpec   `json:"spec,omitempty"`
	Status L3VNIStatus `json:"status,omitempty"`
}

// VRFName returns the name to be used for the
// vrf corresponding to the object.
func (v L3VNI) VRFName() string {
	if v.Spec.VRF != nil && *v.Spec.VRF != "" {
		return *v.Spec.VRF
	}
	return v.Name
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
