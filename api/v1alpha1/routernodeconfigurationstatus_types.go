/*
Copyright 2025.

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

// FailedResource represents a resource that failed configuration
type FailedResource struct {
	// Kind is the type of OpenPERouter resource that failed (Underlay, L2VNI, L3VNI, or L3Passthrough)
	Kind string `json:"kind"`

	// Name is the name of the specific resource instance
	Name string `json:"name"`

	// Message explains the failure reason
	Message string `json:"message,omitempty"`
}

// RouterNodeConfigurationStatusStatus defines the observed state of RouterNodeConfigurationStatus.
type RouterNodeConfigurationStatusStatus struct {
	// LastUpdateTime indicates when the configuration status was last updated
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// FailedResources contains information about resources that failed configuration
	FailedResources []FailedResource `json:"failedResources,omitempty"`

	// Conditions represent the latest available observations of the RouterNodeConfigurationStatus state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced

// RouterNodeConfigurationStatus is the Schema for the routernodeconfigurationstatuses API.
type RouterNodeConfigurationStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status RouterNodeConfigurationStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RouterNodeConfigurationStatusList contains a list of RouterNodeConfigurationStatus.
type RouterNodeConfigurationStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RouterNodeConfigurationStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RouterNodeConfigurationStatus{}, &RouterNodeConfigurationStatusList{})
}
