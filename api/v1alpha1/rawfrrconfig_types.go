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

// RawFRRConfigSpec defines the desired state of RawFRRConfig.
type RawFRRConfigSpec struct {
	// NodeSelector specifies which nodes this RawFRRConfig applies to.
	// If empty or not specified, applies to all nodes.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// Priority controls the ordering of raw config snippets in the rendered FRR configuration.
	// Lower values are rendered first. Snippets with the same priority have undefined order.
	// +kubebuilder:default:=0
	// +kubebuilder:validation:Minimum=0
	// +optional
	Priority int `json:"priority,omitempty"`

	// RawConfig is the raw FRR configuration text to append to the rendered configuration.
	// WARNING: This feature is intended for advanced use cases. No validation of FRR syntax
	// is performed at admission time; invalid configuration will cause FRR reload failures.
	// +kubebuilder:validation:MinLength=1
	// +required
	RawConfig string `json:"rawConfig"`
}

// RawFRRConfigStatus defines the observed state of RawFRRConfig.
type RawFRRConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-rawfrrconfig,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=rawfrrconfigs,versions=v1alpha1,name=rawfrrconfigvalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// RawFRRConfig is the Schema for the rawfrrconfigs API.
type RawFRRConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RawFRRConfigSpec   `json:"spec,omitempty"`
	Status RawFRRConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RawFRRConfigList contains a list of RawFRRConfig.
type RawFRRConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RawFRRConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RawFRRConfig{}, &RawFRRConfigList{})
}
