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

// L2VNISpec defines the desired state of VNI.
type L2VNISpec struct {

	// VRF is the name of the linux VRF to be used inside the PERouter namespace.
	// The field is optional, if not set it the name of the VNI instance will be used.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +optional
	VRF *string `json:"vrf,omitempty"`

	// VNI is the VXLan VNI to be used
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	VNI uint32 `json:"vni,omitempty"`

	// VXLanPort is the port to be used for VXLan encapsulation.
	// +kubebuilder:default:=4789
	VXLanPort uint32 `json:"vxlanport,omitempty"`

	// HostMaster is the interface on the host the veth should be enslaved to.
	// If not set, the host veth will not be enslaved to any interface and it must be
	// enslaved manually (or by some other means). This is useful if another controller
	// is leveraging the host interface for the VNI.
	// +optional
	HostMaster *HostMaster `json:"hostmaster"`

	// L2GatewayIP is the IP address to be used for the L2 gateway. When this is set, the
	// bridge the veths are enslaved to will be configured with this IP address, effectively
	// acting as a distributed gateway for the VNI.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="L2GatewayIP can't be changed"
	L2GatewayIP string `json:"l2gatewayip,omitempty"`
}

// +kubebuilder:validation:Required
// +kubebuilder:validation:xvalidation:rule="(self.name != '' && self.autocreate == false) || (self.name == '' && self.autocreate == true)",message="either name must be set or autocreate must be true, but not both."

type HostMaster struct {
	// Name of the host interface. Must match VRF name validation if set.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MaxLength=15
	// +optional
	Name string `json:"name,omitempty"`

	// Type of the host interface. Currently only "bridge" is supported.
	// +kubebuilder:validation:Enum=bridge
	Type string `json:"type,omitempty"`

	// If true, the interface will be created automatically if not present.
	// The name of the bridge is of the form br-hs-<VNI>.
	// +kubebuilder:default:=false
	AutoCreate bool `json:"autocreate,omitempty"`
}

// VNIStatus defines the observed state of VNI.
type L2VNIStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-l2vni,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=l2vnis,versions=v1alpha1,name=l2vnivalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// L2VNI represents a VXLan VNI to receive EVPN type 2 routes
// from.
type L2VNI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   L2VNISpec   `json:"spec,omitempty"`
	Status L2VNIStatus `json:"status,omitempty"`
}

// VRFName returns the name to be used for the
// vrf corresponding to the object.
func (v L2VNI) VRFName() string {
	if v.Spec.VRF != nil && *v.Spec.VRF != "" {
		return *v.Spec.VRF
	}
	return v.Name
}

// +kubebuilder:object:root=true

// VNIList contains a list of VNI.
type L2VNIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []L2VNI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&L2VNI{}, &L2VNIList{})
}
