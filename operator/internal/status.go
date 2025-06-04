// SPDX-License-Identifier:Apache-2.0

package operator

import (
	"context"
	"time"

	openpev1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourcesNotReadyError struct {
	Message string
}

func (e ResourcesNotReadyError) Error() string { return e.Message }

func (e ResourcesNotReadyError) Is(target error) bool {
	_, ok := target.(*ResourcesNotReadyError)
	return ok
}

type ResourcesUnexpectedError struct {
	Message string
}

func (e ResourcesUnexpectedError) Error() string { return e.Message }

func (e ResourcesUnexpectedError) Is(target error) bool {
	_, ok := target.(*ResourcesUnexpectedError)
	return ok
}

type DeploymentNotReadyError struct{}

const (
	ConditionAvailable   = "Available"
	ConditionProgressing = "Progressing"
	ConditionDegraded    = "Degraded"
	ConditionUpgradeable = "Upgradeable"
)

func UpdateStatus(ctx context.Context, client k8sclient.Client, openperouter *openpev1alpha1.OpenPERouter, condition string, reason string, message string) error {
	conditions := getConditions(condition, reason, message)
	if equality.Semantic.DeepEqual(conditions, openperouter.Status.Conditions) {
		return nil
	}
	openperouter.Status.Conditions = getConditions(condition, reason, message)

	if err := client.Status().Update(ctx, openperouter); err != nil {
		return errors.Wrapf(err, "could not update status for object %+v", openperouter)
	}
	return nil
}

func getConditions(condition string, reason string, message string) []metav1.Condition {
	conditions := getBaseConditions()
	switch condition {
	case ConditionAvailable:
		conditions[0].Status = metav1.ConditionTrue
		conditions[1].Status = metav1.ConditionTrue
	case ConditionProgressing:
		conditions[2].Status = metav1.ConditionTrue
		conditions[2].Reason = reason
		conditions[2].Message = message
	case ConditionDegraded:
		conditions[3].Status = metav1.ConditionTrue
		conditions[3].Reason = reason
		conditions[3].Message = message
	}
	return conditions
}

func getBaseConditions() []metav1.Condition {
	now := time.Now()
	return []metav1.Condition{
		{
			Type:               ConditionAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionAvailable,
		},
		{
			Type:               ConditionUpgradeable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionUpgradeable,
		},
		{
			Type:               ConditionProgressing,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionProgressing,
		},
		{
			Type:               ConditionDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionDegraded,
		},
	}
}

func IsOpenPERouterAvailable(ctx context.Context, client k8sclient.Client, namespace string) error {
	controller := &appsv1.DaemonSet{}
	err := client.Get(ctx, types.NamespacedName{Name: "controller", Namespace: namespace}, controller)
	if err != nil {
		return ResourcesUnexpectedError{Message: err.Error()}
	}
	if controller.Status.DesiredNumberScheduled != controller.Status.CurrentNumberScheduled || controller.Status.DesiredNumberScheduled != controller.Status.NumberReady {
		return ResourcesNotReadyError{Message: "controller daemonset not ready"}
	}

	router := &appsv1.DaemonSet{}
	err = client.Get(ctx, types.NamespacedName{Name: "router", Namespace: namespace}, router)
	if err != nil {
		return ResourcesUnexpectedError{Message: err.Error()}
	}
	if router.Status.DesiredNumberScheduled != router.Status.CurrentNumberScheduled || router.Status.DesiredNumberScheduled != router.Status.NumberReady {
		return ResourcesNotReadyError{Message: "router daemonset not ready"}
	}

	nodemarker := &appsv1.Deployment{}
	err = client.Get(ctx, types.NamespacedName{Name: "nodemarker", Namespace: namespace}, nodemarker)
	if err != nil {
		return ResourcesUnexpectedError{Message: err.Error()}
	}
	if nodemarker.Status.ReadyReplicas != *nodemarker.Spec.Replicas {
		return ResourcesNotReadyError{Message: "nodemarker deployment not ready"}
	}

	return nil
}
