// SPDX-License-Identifier:Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// L2TrunkType selects the kind of port that carries the trunk's tagged traffic.
// +kubebuilder:validation:Enum=Host
type L2TrunkType string

const (
	// L2TrunkTypeHost carries the trunk through a host-side interface.
	L2TrunkTypeHost L2TrunkType = "Host"
)

// L2TrunkSpec defines a shared attachment for VLAN-tagged L2VNIs.
// +union
// +kubebuilder:validation:XValidation:rule="self.type == oldSelf.type",message="type is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.hostMaster) == has(oldSelf.hostMaster) && (!has(self.hostMaster) || self.hostMaster == oldSelf.hostMaster)",message="hostMaster is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.hostMaster) || !has(self.hostMaster.linuxBridge) || self.hostMaster.linuxBridge.lifecycle == 'External'",message="L2Trunk does not support managed Linux bridges"
// +kubebuilder:validation:XValidation:rule="!has(self.hostMaster) || !has(self.hostMaster.ovsBridge) || self.hostMaster.ovsBridge.lifecycle == 'External'",message="L2Trunk does not support managed OVS bridges"
type L2TrunkSpec struct {
	// nodeSelector specifies which nodes this L2Trunk applies to.
	// If empty or not specified, applies to all nodes.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// type selects the kind of port that carries the trunk's tagged traffic.
	// +required
	// +unionDiscriminator
	Type L2TrunkType `json:"type,omitempty"`

	// hostMaster is the host bridge the trunk's host-side interface attaches to.
	// The bridge must have lifecycle External.
	// When omitted, the host-side interface is left for external attachment.
	// Immutable.
	// +optional
	HostMaster *HostMaster `json:"hostMaster,omitempty"`
}

// L2TrunkStatus defines the observed state of L2Trunk.
type L2TrunkStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// L2Trunk owns a shared attachment for VLAN-tagged L2VNIs.
type L2Trunk struct {
	metav1.TypeMeta `json:",inline"`
	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// spec defines the desired state of L2Trunk.
	// +required
	Spec L2TrunkSpec `json:"spec,omitzero"`
	// status defines the observed state of L2Trunk.
	// +optional
	Status *L2TrunkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// L2TrunkList contains a list of L2Trunk resources.
type L2TrunkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []L2Trunk `json:"items"`
}
