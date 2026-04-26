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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	status := &v1alpha1.RouterNodeConfigurationStatus{}
	err := cl.Get(ctx, client.ObjectKey{Name: nodeName, Namespace: namespace}, status)
	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to get RouterNodeConfigurationStatus %s: %w", nodeName, err)
	}
	if err == nil {
		return nil
	}

	status = &v1alpha1.RouterNodeConfigurationStatus{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeName,
			Namespace: namespace,
		},
	}
	if err := cl.Create(ctx, status); err != nil {
		return fmt.Errorf("failed to create RouterNodeConfigurationStatus %s: %w", nodeName, err)
	}
	return nil
}

func updateStatus(ctx context.Context, cl client.Client, nodeName, namespace string, result ReconcileResult) error {
	status := &v1alpha1.RouterNodeConfigurationStatus{}
	if err := cl.Get(ctx, client.ObjectKey{Name: nodeName, Namespace: namespace}, status); err != nil {
		return fmt.Errorf("failed to get RouterNodeConfigurationStatus %s: %w", nodeName, err)
	}

	oldStatus := status.Status.DeepCopy()

	status.Status.FailedResources = result.FailedResources
	setConditions(status, result, nil)

	if equality.Semantic.DeepEqual(*oldStatus, status.Status) {
		return nil
	}

	if err := cl.Status().Update(ctx, status); err != nil {
		return fmt.Errorf("failed to update RouterNodeConfigurationStatus %s: %w", nodeName, err)
	}

	return nil
}

func setConditions(status *v1alpha1.RouterNodeConfigurationStatus, result ReconcileResult, reconcileErr error) {
	now := metav1.Now()

	if reconcileErr != nil {
		apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonConfigurationFailed,
			Message:            reconcileErr.Error(),
			LastTransitionTime: now,
		})
		apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonConfigurationFailed,
			Message:            reconcileErr.Error(),
			LastTransitionTime: now,
		})
		return
	}

	if !result.HasFailures() && !result.HasFailure("Underlay") {
		apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonConfigurationSuccessful,
			Message:            "All configuration applied successfully",
			LastTransitionTime: now,
		})
		apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeDegraded,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonConfigurationSuccessful,
			Message:            "All configuration applied successfully",
			LastTransitionTime: now,
		})
		return
	}

	if result.HasFailure("Underlay") {
		apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             ReasonUnderlayFailed,
			Message:            "Underlay failed validation, existing FRR configuration left as-is",
			LastTransitionTime: now,
		})
		apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             ReasonUnderlayFailed,
			Message:            "Underlay failed validation, VNI processing skipped",
			LastTransitionTime: now,
		})
		return
	}

	names := make([]string, len(result.FailedResources))
	for i, f := range result.FailedResources {
		names[i] = f.Kind + "/" + f.Name
	}
	slices.Sort(names)
	msg := fmt.Sprintf("%d resources failed (%s)", len(result.FailedResources), strings.Join(names, ", "))
	apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             ReasonConfigurationFailed,
		Message:            msg,
		LastTransitionTime: now,
	})
	apimeta.SetStatusCondition(&status.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             ReasonConfigurationFailed,
		Message:            "Some resources failed to configure",
		LastTransitionTime: now,
	})
}
