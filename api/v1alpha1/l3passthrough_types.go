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

type L3PassthroughSpec struct {
	// HostSession is the configuration for the host session.
	HostSession HostSession `json:"hostsession,omitempty"`
}

// L3PassthroughStatus defines the observed state of L3Passthrough.
type L3PassthroughStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:webhook:verbs=create;update,path=/validate-openperouter-io-v1alpha1-l3passthrough,mutating=false,failurePolicy=fail,groups=openpe.openperouter.github.io,resources=l3passthroughs,versions=v1alpha1,name=l3passthroughvalidationwebhook.openperouter.io,sideEffects=None,admissionReviewVersions=v1

// L3Passthrough represents a session with the host which is not encapsulated and
// takes part to the bgp fabric.
type L3Passthrough struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   L3PassthroughSpec   `json:"spec,omitempty"`
	Status L3PassthroughStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// L3PassthroughList contains a list of L3Passthrough.
type L3PassthroughList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []L3Passthrough `json:"items"`
}

func init() {
	SchemeBuilder.Register(&L3Passthrough{}, &L3PassthroughList{})
}
