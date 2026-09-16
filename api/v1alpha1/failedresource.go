// SPDX-License-Identifier:Apache-2.0

package v1alpha1

// +kubebuilder:validation:Enum=Underlay;L2VNI;L3VNI;L3VPN;FrrConfiguration;L3Passthrough
type FailedResourceKind string

// FailedResourceReason machine-readable reason for a failure.
// +kubebuilder:validation:Enum=ValidationFailed;DependencyFailed;OverlayAttachmentFailed;FrrConfigurationFailed
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=100
type FailedResourceReason string

const (
	// FailedResourceReasonValidationFailed indicates failed pre-emptive semantic validation
	// (e.g., interface not found, VNI conflict).
	FailedResourceReasonValidationFailed FailedResourceReason = "ValidationFailed"

	// FailedResourceReasonDependencyFailed dependent-on resource is not ready
	// (e.g., L2VNI specify an interface managed by failing Underlay resource).
	FailedResourceReasonDependencyFailed FailedResourceReason = "DependencyFailed"

	// FailedResourceReasonOverlayAttachmentFailed provisioning failure at the logical network layer of the router
	// (e.g.: failed to create VRF, move interface to router namespace).
	FailedResourceReasonOverlayAttachmentFailed FailedResourceReason = "OverlayAttachmentFailed"

	// FailedResourceReasonFrrConfigurationFailed applying FRR configuration failed.
	FailedResourceReasonFrrConfigurationFailed FailedResourceReason = "FrrConfigurationFailed"
)

// FailedResource describe failing router API resource
type FailedResource struct {
	// kind resource type name (e.g.: L3VNI, L2VNI).
	// +required
	Kind FailedResourceKind `json:"kind"` // nolint:kubeapilinter // required filed should not set omitempty

	// name failed API resource metadata.name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"` // nolint:kubeapilinter // required filed should not set omitempty

	// reason failure reason.
	// +required
	Reason FailedResourceReason `json:"reason"` // nolint:kubeapilinter // required filed should not set omitempty

	// message human-readable failure description.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=500
	Message string `json:"message"` // nolint:kubeapilinter // required filed should not set omitempty
}
