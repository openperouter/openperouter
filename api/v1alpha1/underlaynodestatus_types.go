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

// InterfaceStatus represents the status of each network interface used for an
// Underlay.
type InterfaceStatus struct {
	// Name is the name of the network interface as specified in the Underlay's nics field.
	// +required
	Name string `json:"name"`

	// Status indicates the current status of the interface.
	// Valid values are:
	// - "Found": Interface exists and is available for use
	// - "NotFound": Interface does not exist on this node
	// - "Error": An error occurred while checking or configuring the interface
	// - "Moved": Interface has been successfully moved to the router namespace
	// - "InUse": Interface is already in use by another process
	// +required
	// +kubebuilder:validation:Enum=Found;NotFound;Error;Moved;InUse
	Status string `json:"status"`

	// Message provides additional details about the interface status.
	// This field may contain error messages, configuration details, or other
	// relevant information about the interface state.
	// +optional
	Message string `json:"message,omitempty"`
}

// UnderlayNodeStatusStatus defines the state of the Underlay configuration on
// the specific Node.
type UnderlayNodeStatusStatus struct {
	// InterfaceStatuses contains the status of network interfaces referenced in the Underlay spec.
	// Each interface specified in the Underlay's nics field will have a corresponding
	// status entry indicating whether it was found and successfully configured.
	// +optional
	InterfaceStatuses []InterfaceStatus `json:"interfaceStatuses,omitempty"`

	// LastReconciled is the timestamp of the last successful reconciliation of this UnderlayNodeStatus.
	// +optional
	LastReconciled *metav1.Time `json:"lastReconciled,omitempty"`
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
//	  interfaceStatuses:
//	  - name: "eth0"
//	    status: "Found"
//	    message: "Interface successfully moved to router namespace"
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

// Interface status constants
const (
	// InterfaceStatusFound indicates that the interface exists and is available for use.
	InterfaceStatusFound = "Found"

	// InterfaceStatusNotFound indicates that the interface does not exist on this node.
	InterfaceStatusNotFound = "NotFound"

	// InterfaceStatusError indicates that an error occurred while checking or configuring the interface.
	InterfaceStatusError = "Error"

	// InterfaceStatusMoved indicates that the interface has been successfully moved to the router namespace.
	InterfaceStatusMoved = "Moved"

	// InterfaceStatusInUse indicates that the interface is already in use by another process.
	InterfaceStatusInUse = "InUse"
)
