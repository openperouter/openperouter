// SPDX-License-Identifier:Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// L2TrunkType selects the kind of port that carries the trunk's tagged traffic.
// +kubebuilder:validation:Enum=Host;SRIOV
type L2TrunkType string

const (
	// L2TrunkTypeHost carries the trunk through a host-side interface.
	L2TrunkTypeHost L2TrunkType = "Host"

	// L2TrunkTypeSRIOV carries the trunk through an SR-IOV VF bound to grout.
	L2TrunkTypeSRIOV L2TrunkType = "SRIOV"
)

// L2TrunkSpec defines a shared attachment for VLAN-tagged L2VNIs.
// +union
// +kubebuilder:validation:XValidation:rule="self.type == 'Host' || !has(self.hostMaster)",message="hostMaster can only be set when type is 'Host'"
// +kubebuilder:validation:XValidation:rule="has(self.sriov) == (self.type == 'SRIOV')",message="type/config mismatch: sriov must be set if and only if type is 'SRIOV'"
// +kubebuilder:validation:XValidation:rule="self.type == oldSelf.type",message="type is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.sriov) == has(oldSelf.sriov) && (!has(self.sriov) || self.sriov == oldSelf.sriov)",message="sriov is immutable"
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

	// sriov selects the SR-IOV VF used as the trunk port. The VF is bound
	// to grout and no host-side interface is created. Must be set when
	// type is "SRIOV". Only supported by the grout data path.
	// Immutable.
	// +optional
	SRIOV *SRIOVTrunkConfig `json:"sriov,omitempty"`
}

// SRIOVTrunkConfig identifies the SR-IOV VF bound to grout as the trunk port.
type SRIOVTrunkConfig struct {
	// pfName is the kernel network interface name of the Physical Function
	// that owns the VF (e.g. "enp1s0").
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9._-]*$`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +required
	PFName string `json:"pfName,omitempty"`

	// vfIndex is the index of the VF on the PF. The VF must have no VLAN
	// configured so it receives the tagged frames of every workload VF on
	// the same PF.
	// +kubebuilder:validation:Minimum=0
	// +required
	VFIndex *int32 `json:"vfIndex,omitempty"`

	// acceleratedConfig sets optional grout port parameters for the VF.
	// +optional
	AcceleratedConfig *AcceleratedConfig `json:"acceleratedConfig,omitempty"`
}

// L2TrunkStatus defines the observed state of L2Trunk.
type L2TrunkStatus struct{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-l2trunk,mutating=false,failurePolicy=fail,groups=network.openperouter.io,resources=l2trunks,versions=v1alpha1,name=l2trunkvalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1
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
