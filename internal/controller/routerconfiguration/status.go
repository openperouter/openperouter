// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func buildStatus(err error) v1alpha1.RouterNodeConfigurationStatusStatus {
	var s v1alpha1.RouterNodeConfigurationStatusStatus

	if err == nil {
		setReady(&s, v1alpha1.ConditionReasonConfigSuccessful, "All configuration applied successfully")
		return s
	}

	s.FailedResources = openpeerrors.CollectFailures(err)
	reason, msg := degradedReason(err, s.FailedResources)
	setDegraded(&s, reason, msg)
	return s
}

func degradedReason(err error, failures []v1alpha1.FailedResource) (string, string) {
	if openpeerrors.HasUnderlayFailure(err) {
		return v1alpha1.ConditionReasonUnderlayFailed, "Underlay failed validation, existing FRR configuration left as-is"
	}
	if len(failures) > 0 {
		return v1alpha1.ConditionReasonConfigFailed, "Some resources failed validation, see status.failedResources for details"
	}
	return v1alpha1.ConditionReasonConfigFailed, err.Error()
}

func setDegraded(s *v1alpha1.RouterNodeConfigurationStatusStatus, reason, message string) {
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    v1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    v1alpha1.ConditionTypeDegraded,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

func setReady(s *v1alpha1.RouterNodeConfigurationStatusStatus, reason, message string) {
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    v1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    v1alpha1.ConditionTypeDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}
