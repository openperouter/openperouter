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

// FailureReason describes why a resource failed during reconciliation.
type FailureReason string

const (
	ValidationFailed        FailureReason = "ValidationFailed"
	DependencyFailed        FailureReason = "DependencyFailed"
	OverlayAttachmentFailed FailureReason = "OverlayAttachmentFailed"
	FrrConfigurationFailed  FailureReason = "FrrConfigurationFailed"
)

// FailedResource describes a single resource that failed during reconciliation.
type FailedResource struct {
	// kind is the type of OpenPERouter resource that failed (e.g. "Underlay", "L2VNI", "L3VNI", "FrrConfiguration").
	// +required
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind,omitempty"`
	// name is the name of the specific resource instance.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
	// reason is why the resource failed.
	// +required
	// +kubebuilder:validation:Enum=ValidationFailed;DependencyFailed;OverlayAttachmentFailed;FrrConfigurationFailed
	Reason FailureReason `json:"reason,omitempty"`
	// message is a detailed error description.
	// +optional
	Message *string `json:"message,omitempty"`
}

// RouterNodeConfigurationStatusStatus defines the observed state of RouterNodeConfigurationStatus.
type RouterNodeConfigurationStatusStatus struct {
	// conditions represent the latest available observations of the configuration state.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
	// failedResources lists resources that failed during reconciliation.
	// +optional
	// +listType=atomic
	FailedResources []FailedResource `json:"failedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Degraded",type=string,JSONPath=`.status.conditions[?(@.type=="Degraded")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// RouterNodeConfigurationStatus reports the configuration result for a single node.
type RouterNodeConfigurationStatus struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// status is the observed state of the node configuration.
	// +optional
	Status *RouterNodeConfigurationStatusStatus `json:"status,omitempty"`
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
