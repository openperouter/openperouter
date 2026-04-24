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

type LogLevel string

// These are valid logging level for OpenPERouter components.
const (
	LogLevelAll   LogLevel = "all"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelNone  LogLevel = "none"
)

// OpenPERouterSpec defines the desired state of OpenPERouter
type OpenPERouterSpec struct {
	// logLevel defines the verbosity of the controller and the router logging.
	// Allowed values are: all, debug, info, warn, error, none. (default: info)
	// +optional
	// +kubebuilder:validation:Enum=all;debug;info;warn;error;none
	LogLevel *LogLevel `json:"logLevel,omitempty"`
	// multusNetworkAnnotation specifies the Multus network annotation to be added to the router pod.
	// +optional
	MultusNetworkAnnotation *string `json:"multusNetworkAnnotation,omitempty"`
	// runOnMaster determines if all pods (router, controller, and nodemarker) will run on master/control-plane nodes. (default: true)
	// +optional
	// +default=true
	RunOnMaster *bool `json:"runOnMaster,omitempty"`
	// ovsSocketPath specifies the OVS database socket path. Defaults to standard OVS location if not specified.
	// +optional
	OVSSocketPath *string `json:"ovsSocketPath,omitempty"`
	// ovsRunDir specifies the OVS run directory to mount. This is the directory containing the OVS socket. (default: /var/run/openvswitch)
	// +optional
	OVSRunDir *string `json:"ovsRunDir,omitempty"`
	// healthProbePort specifies the port for the controller's health and readiness probes. (default: 9081)
	// +optional
	// +default=9081
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	HealthProbePort *int32 `json:"healthProbePort,omitempty"`
}

// OpenPERouterStatus defines the observed state of OpenPERouter
type OpenPERouterStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// OpenPERouter is the Schema for the openperouters API
type OpenPERouter struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of OpenPERouter.
	// +optional
	Spec *OpenPERouterSpec `json:"spec,omitempty"`
	// status defines the observed state of OpenPERouter.
	// +optional
	Status *OpenPERouterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OpenPERouterList contains a list of OpenPERouter
type OpenPERouterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OpenPERouter `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OpenPERouter{}, &OpenPERouterList{})
}
