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
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

const (
	ConditionTypeReady    = "Ready"
	ConditionTypeDegraded = "Degraded"

	ReasonConfigurationSuccessful = "ConfigurationSuccessful"
	ReasonConfigurationFailed     = "ConfigurationFailed"
	ReasonUnderlayFailed          = "UnderlayFailed"
)

func ensureStatusCR(ctx context.Context, cl client.Client, nodeName, namespace string) error {
	status := &v1alpha1.RouterNodeConfigurationStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeName,
			Namespace: namespace,
		},
	}
	err := cl.Create(ctx, status)
	if err == nil || apierrors.IsAlreadyExists(err) {
		return nil
	}
	return fmt.Errorf("failed to create RouterNodeConfigurationStatus %s: %w", nodeName, err)
}

func updateStatus(ctx context.Context, cl client.Client, nodeName, namespace string, result ReconcileResult) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		status := &v1alpha1.RouterNodeConfigurationStatus{}
		if err := cl.Get(ctx, client.ObjectKey{Name: nodeName, Namespace: namespace}, status); err != nil {
			return fmt.Errorf("failed to get RouterNodeConfigurationStatus %s: %w", nodeName, err)
		}

		oldStatus := status.Status.DeepCopy()

		newStatus := buildStatus(result)
		status.Status = &newStatus

		if equality.Semantic.DeepEqual(oldStatus, status.Status) {
			return nil
		}

		if err := cl.Status().Update(ctx, status); err != nil {
			return fmt.Errorf("failed to update RouterNodeConfigurationStatus %s: %w", nodeName, err)
		}

		return nil
	})
}

func buildStatus(result ReconcileResult) v1alpha1.RouterNodeConfigurationStatusStatus {
	var s v1alpha1.RouterNodeConfigurationStatusStatus
	s.FailedResources = result.FailedResources

	if result.ReconcileError != nil {
		setDegraded(&s, ReasonConfigurationFailed, result.ReconcileError.Error())
		return s
	}

	if !result.HasFailures() {
		setReady(&s, ReasonConfigurationSuccessful, "All configuration applied successfully")
		return s
	}

	if result.HasFailure(KindUnderlay) {
		setDegraded(&s, ReasonUnderlayFailed, "Underlay failed validation, existing FRR configuration left as-is")
		return s
	}

	setDegraded(&s, ReasonConfigurationFailed, failedResourcesSummary(result.FailedResources))
	return s
}

func setDegraded(s *v1alpha1.RouterNodeConfigurationStatusStatus, reason, message string) {
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    ConditionTypeDegraded,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

func setReady(s *v1alpha1.RouterNodeConfigurationStatusStatus, reason, message string) {
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:    ConditionTypeDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

func failedResourcesSummary(resources []v1alpha1.FailedResource) string {
	names := make([]string, len(resources))
	for i, f := range resources {
		names[i] = f.Kind + "/" + f.Name
	}
	slices.Sort(names)
	return fmt.Sprintf("%d resources failed (%s)", len(resources), strings.Join(names, ", "))
}
