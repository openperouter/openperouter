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

// UnderlaySpec defines the desired state of Underlay.
type UnderlaySpec struct {
	// nodeSelector specifies which nodes this Underlay applies to.
	// If empty or not specified, applies to all nodes (backward compatible).
	// Multiple Underlays with overlapping node selectors will be rejected.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// asn is the local AS number to use for the session with the TOR switch.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	// +required
	ASN int64 `json:"asn,omitempty"`

	// routeridcidr is the ipv4 cidr to be used to assign a different routerID on each node.
	// +default="10.0.0.0/24"
	// +optional
	RouterIDCIDR *string `json:"routeridcidr,omitempty"`

	// neighbors is the list of external BGP neighbors to peer with.
	// Note: MaxItems=128 is arbitrarily chosen to keep total CEL cost low
	// Note: kubeapilinter complained about 'the struct has no required fields', but CEL enforces either/or choices
	// for Address and Interface.
	// Multiple neighbors are supported for connecting to multiple TOR switches
	// or establishing redundant BGP sessions. Each neighbor address must be unique.
	// At least one neighbor is required.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=128
	// +required
	// +listType=atomic
	Neighbors []Neighbor `json:"neighbors,omitempty"` //nolint:kubeapilinter

	// nics is the list of physical nics to move under the PERouter namespace to connect
	// to external routers. At least one NIC is required.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:items:MaxLength=15
	// +required
	// +listType=atomic
	Nics []string `json:"nics,omitempty"`

	// evpn contains EVPN-VXLAN configuration for the underlay.
	// +optional
	EVPN *EVPNConfig `json:"evpn,omitempty"`

	// gracefulRestart configures BGP Graceful Restart behaviour.
	// When set, FRR advertises GR capability and preserves forwarding
	// state across restarts so that peers keep stale routes active.
	// Omit to disable graceful restart.
	// +optional
	GracefulRestart *GracefulRestartConfig `json:"gracefulRestart,omitempty"`
}

// GracefulRestartConfig holds BGP Graceful Restart parameters.
// Its presence on the Underlay enables graceful restart.
type GracefulRestartConfig struct {
	// restartTimeSeconds is the time in seconds that the restarting router
	// requests its peers to preserve routes. Peers will wait this long
	// before removing stale routes.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4095
	// +default=120
	// +optional
	RestartTimeSeconds *int64 `json:"restartTimeSeconds,omitempty"`

	// stalePathTimeSeconds is the time in seconds that stale paths from a
	// restarting peer are retained locally.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4095
	// +default=360
	// +optional
	StalePathTimeSeconds *int64 `json:"stalePathTimeSeconds,omitempty"`
}

// EVPNConfig contains EVPN-VXLAN configuration for the underlay.
type EVPNConfig struct {
	// vtepCIDR is the CIDR to be used to assign IPs to the local VTEP on each node.
	// A loopback interface will be created with an IP derived from this CIDR.
	// +optional
	VTEPCIDR *string `json:"vtepCIDR,omitempty"`
}

// UnderlayStatus defines the observed state of Underlay.
type UnderlayStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-underlay,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=underlays,versions=v1alpha1,name=underlayvalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// Underlay is the Schema for the underlays API.
type Underlay struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Underlay.
	// +required
	Spec UnderlaySpec `json:"spec,omitzero,omitempty"`
	// status defines the observed state of Underlay.
	// +optional
	Status *UnderlayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UnderlayList contains a list of Underlay.
type UnderlayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Underlay `json:"items"`
}
