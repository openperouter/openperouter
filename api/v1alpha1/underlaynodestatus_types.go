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

// InterfaceStatusType represents the status of a network interface configuration.
// +kubebuilder:validation:Enum=SuccessfullyConfigured;NotFound;InUse;Error
type InterfaceStatusType string

const (
	// InterfaceStatusSuccessfullyConfigured indicates the interface was successfully configured.
	InterfaceStatusSuccessfullyConfigured InterfaceStatusType = "SuccessfullyConfigured"
	// InterfaceStatusNotFound indicates the interface was not found on the node.
	InterfaceStatusNotFound InterfaceStatusType = "NotFound"
	// InterfaceStatusInUse indicates the interface is currently in use and cannot be configured.
	InterfaceStatusInUse InterfaceStatusType = "InUse"
	// InterfaceStatusError indicates an error occurred while configuring the interface.
	InterfaceStatusError InterfaceStatusType = "Error"
)

// InterfaceStatus represents the status of a network interface configuration.
type InterfaceStatus struct {
	// Name is the name of the network interface.
	// +required
	Name string `json:"name"`

	// Status indicates the current status of the interface configuration.
	// +required
	Status InterfaceStatusType `json:"status"`

	// Message provides additional information about the interface status.
	// +optional
	Message string `json:"message,omitempty"`
}

// UnderlayNodeStatusSpec defines which Underlay and Node this Status
// represents.
type UnderlayNodeStatusSpec struct {
	// NodeName is the name of the node this status applies to.
	// +required
	NodeName string `json:"nodeName"`

	// UnderlayName is the name of the Underlay resource this status is for.
	// +required
	UnderlayName string `json:"underlayName"`
}

// UnderlayNodeStatusStatus defines the state of the Underlay configuration on
// the specific Node.
type UnderlayNodeStatusStatus struct {
	// LastReconciled is the timestamp of the last successful reconciliation of this UnderlayNodeStatus.
	// +optional
	LastReconciled *metav1.Time `json:"lastReconciled,omitempty"`

	// InterfaceStatuses contains the status of network interface configurations.
	// +optional
	InterfaceStatuses []InterfaceStatus `json:"interfaceStatuses,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=uns
// +kubebuilder:printcolumn:name="Node",type="string",JSONPath=".spec.nodeName"
// +kubebuilder:printcolumn:name="Underlay",type="string",JSONPath=".spec.underlayName"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// UnderlayNodeStatus represents the node-specific status of an Underlay configuration.
//
// UnderlayNodeStatus is automatically created and managed by the Underlay controller
// for each node in the cluster when an Underlay resource is created. It provides
// detailed information about the status of the Underlay configuration on individual nodes,
// including interface discovery and configuration application status.
//
// This resource is read-only for users and should not be manually created or modified.
//
// Example:
//
//	apiVersion: openpe.openperouter.github.io/v1alpha1
//	kind: UnderlayNodeStatus
//	metadata:
//	  name: my-underlay.worker-1
//	  namespace: openperouter-system
//	spec:
//	  nodeName: worker-1
//	  underlayName: my-underlay
//	status:
//	  lastReconciled: "2024-01-01T00:00:00Z"
type UnderlayNodeStatus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   UnderlayNodeStatusSpec   `json:"spec"`
	Status UnderlayNodeStatusStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UnderlayNodeStatusList contains a list of UnderlayNodeStatus.
type UnderlayNodeStatusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []UnderlayNodeStatus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnderlayNodeStatus{}, &UnderlayNodeStatusList{})
}
