// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
)

func TestBuildStatus(t *testing.T) {
	underlayErr := &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind: openpeerrors.KindUnderlay, Name: "my-underlay",
			Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: "bad ASN",
		},
	}

	partialErr := errors.Join(
		&openpeerrors.ResourceError{
			Obj: v1alpha1.FailedResource{
				Kind: openpeerrors.KindL3VNI, Name: "bad-vni",
				Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: "invalid vrf",
			},
		},
		&openpeerrors.ResourceError{
			Obj: v1alpha1.FailedResource{
				Kind: openpeerrors.KindL2VNI, Name: "orphan",
				Reason: v1alpha1.FailedResourceReasonDependencyFailed, Message: "no L3VNI",
			},
		},
	)

	tests := []struct {
		name                    string
		reconcileErr            error
		expectedReady           metav1.ConditionStatus
		expectedDegraded        metav1.ConditionStatus
		expectedReadyReason     string
		expectedReadyMessage    string
		expectedFailedResources []v1alpha1.FailedResource
	}{
		{
			name:                 "all resources valid",
			reconcileErr:         nil,
			expectedReady:        metav1.ConditionTrue,
			expectedDegraded:     metav1.ConditionFalse,
			expectedReadyReason:  v1alpha1.ConditionReasonConfigSuccessful,
			expectedReadyMessage: "All configuration applied successfully",
		},
		{
			name:                 "underlay failed",
			reconcileErr:         underlayErr,
			expectedReady:        metav1.ConditionFalse,
			expectedDegraded:     metav1.ConditionTrue,
			expectedReadyReason:  v1alpha1.ConditionReasonUnderlayFailed,
			expectedReadyMessage: "Underlay failed validation, existing FRR configuration left as-is",
			expectedFailedResources: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindUnderlay, Name: "my-underlay", Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: "bad ASN"},
			},
		},
		{
			name:                 "partial failure",
			reconcileErr:         partialErr,
			expectedReady:        metav1.ConditionFalse,
			expectedDegraded:     metav1.ConditionTrue,
			expectedReadyReason:  v1alpha1.ConditionReasonConfigFailed,
			expectedReadyMessage: "Some resources failed validation, see status.failedResources for details",
			expectedFailedResources: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VNI, Name: "bad-vni", Reason: v1alpha1.FailedResourceReasonValidationFailed, Message: "invalid vrf"},
				{Kind: openpeerrors.KindL2VNI, Name: "orphan", Reason: v1alpha1.FailedResourceReasonDependencyFailed, Message: "no L3VNI"},
			},
		},
		{
			name:                 "reconcile hard error",
			reconcileErr:         fmt.Errorf("failed to reload frr config: exit status 1"),
			expectedReady:        metav1.ConditionFalse,
			expectedDegraded:     metav1.ConditionTrue,
			expectedReadyReason:  v1alpha1.ConditionReasonConfigFailed,
			expectedReadyMessage: "failed to reload frr config: exit status 1",
		},
		{
			name:                 "internal error",
			reconcileErr:         fmt.Errorf("failed to get router pod instance: pod not found"),
			expectedReady:        metav1.ConditionFalse,
			expectedDegraded:     metav1.ConditionTrue,
			expectedReadyReason:  v1alpha1.ConditionReasonConfigFailed,
			expectedReadyMessage: "failed to get router pod instance: pod not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := buildStatus(tt.reconcileErr)

			ready := apimeta.FindStatusCondition(s.Conditions, v1alpha1.ConditionTypeReady)
			if ready == nil {
				t.Fatal("Ready condition not set")
			}
			if ready.Status != tt.expectedReady {
				t.Errorf("Ready status = %s, want %s", ready.Status, tt.expectedReady)
			}
			if ready.Reason != tt.expectedReadyReason {
				t.Errorf("Ready reason = %s, want %s", ready.Reason, tt.expectedReadyReason)
			}
			if ready.Message != tt.expectedReadyMessage {
				t.Errorf("Ready message = %q, want %q", ready.Message, tt.expectedReadyMessage)
			}

			degraded := apimeta.FindStatusCondition(s.Conditions, v1alpha1.ConditionTypeDegraded)
			if degraded == nil {
				t.Fatal("Degraded condition not set")
			}
			if degraded.Status != tt.expectedDegraded {
				t.Errorf("Degraded status = %s, want %s", degraded.Status, tt.expectedDegraded)
			}

			if !reflect.DeepEqual(s.FailedResources, tt.expectedFailedResources) {
				t.Errorf("FailedResources mismatch:\n  got:  %+v\n  want: %+v", s.FailedResources, tt.expectedFailedResources)
			}
		})
	}
}
