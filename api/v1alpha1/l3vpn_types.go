/*
Copyright 2026.

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

// L3VPNSpec defines the desired state of L3VPN.
// +kubebuilder:validation:XValidation:rule="!has(self.hostsession) || self.hostsession.hostasn != self.hostsession.asn",message="hostASN must be different from asn"
type L3VPNSpec struct {
	// nodeSelector specifies which nodes this L3VPN applies to.
	// If empty or not specified, applies to all nodes.
	// Multiple L3VPNs can match the same node.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// vrf is the name of the linux VRF to be used inside the PERouter namespace.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]*$`
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=15
	// +required
	VRF string `json:"vrf,omitempty"`

	// exportRTs are the Route Targets to be used for exporting routes.
	// If no exportRTs are provided, defaults to single export Route Target
	// <asn>:<rdAssignedNumber>.
	// +kubebuilder:validation:MaxItems:=100
	// +listType=atomic
	// +optional
	ExportRTs []RouteTarget `json:"exportRTs,omitempty"`

	// importRTs are the Route Targets to be used for importing routes.
	// importRTs must always be provided explicitly.
	// +kubebuilder:validation:MaxItems:=100
	// +listType=atomic
	// +required
	ImportRTs []RouteTarget `json:"importRTs,omitempty"`

	// rdAssignedNumber sets the Route Distinguisher's Assigned Number subfield.
	// The Administrator subfield is automatically set to the value of the router
	// ID. OpenPERouter uses Type 1 Route Distinguishers as defined in RFC4364,
	// meaning <Administrator subfield>:<Assigned Number subfield>.
	//
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=65535
	// +required
	RDAssignedNumber int32 `json:"rdAssignedNumber,omitempty"`

	// hostsession is the configuration for the host session.
	// +optional
	HostSession *HostSession `json:"hostsession,omitempty"`
}

// L3VPNStatus defines the observed state of L3VPN.
type L3VPNStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-l3vpn,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=l3vpns,versions=v1alpha1,name=l3vpnsvalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// L3VPN represents an SRv6 IP VPN.
type L3VPN struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of L3VPN.
	// +required
	Spec L3VPNSpec `json:"spec,omitzero"`
	// status defines the observed state of L3VPN.
	// +optional
	Status *L3VPNStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// L3VPNList contains a list of L3VPN.
type L3VPNList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []L3VPN `json:"items"`
}
