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

package routerconfiguration

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func TestSetConditions(t *testing.T) {
	underlayResult := ReconcileResult{}
	underlayResult.AddFailure("Underlay", "my-underlay", v1alpha1.ValidationFailed, "bad ASN")

	partialResult := ReconcileResult{}
	partialResult.AddFailure("L3VNI", "bad-vni", v1alpha1.ValidationFailed, "invalid vrf")
	partialResult.AddFailure("L2VNI", "orphan", v1alpha1.DependencyFailed, "no L3VNI")

	tests := []struct {
		name                 string
		result               ReconcileResult
		reconcileErr         error
		expectedReady        metav1.ConditionStatus
		expectedDegraded     metav1.ConditionStatus
		expectedReadyReason  string
		expectedReadyMessage string
	}{
		{
			name:                 "all resources valid",
			result:               ReconcileResult{},
			expectedReady:        metav1.ConditionTrue,
			expectedDegraded:     metav1.ConditionFalse,
			expectedReadyReason:  ReasonConfigurationSuccessful,
			expectedReadyMessage: "All configuration applied successfully",
		},
		{
			name:                 "underlay failed",
			result:               underlayResult,
			expectedReady:        metav1.ConditionFalse,
			expectedDegraded:     metav1.ConditionTrue,
			expectedReadyReason:  ReasonUnderlayFailed,
			expectedReadyMessage: "Underlay failed validation, existing FRR configuration left as-is",
		},
		{
			name:                 "partial failure",
			result:               partialResult,
			expectedReady:        metav1.ConditionFalse,
			expectedDegraded:     metav1.ConditionTrue,
			expectedReadyReason:  ReasonConfigurationFailed,
			expectedReadyMessage: "2 resources failed (L2VNI/orphan, L3VNI/bad-vni)",
		},
		{
			name:                 "reconcile hard error",
			result:               ReconcileResult{},
			reconcileErr:         fmt.Errorf("failed to reload frr config: exit status 1"),
			expectedReady:        metav1.ConditionFalse,
			expectedDegraded:     metav1.ConditionTrue,
			expectedReadyReason:  ReasonConfigurationFailed,
			expectedReadyMessage: "failed to reload frr config: exit status 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &v1alpha1.RouterNodeConfigurationStatus{}
			setConditions(status, tt.result, tt.reconcileErr)

			ready := findCondition(status.Status.Conditions, ConditionTypeReady)
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

			degraded := findCondition(status.Status.Conditions, ConditionTypeDegraded)
			if degraded == nil {
				t.Fatal("Degraded condition not set")
			}
			if degraded.Status != tt.expectedDegraded {
				t.Errorf("Degraded status = %s, want %s", degraded.Status, tt.expectedDegraded)
			}
		})
	}
}
