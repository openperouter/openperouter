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

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/status"
)

// RouterNodeConfigurationStatusReconciler reconciles a RouterNodeConfigurationStatus object
type RouterNodeConfigurationStatusReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	MyNode       string
	MyNamespace  string
	Logger       *slog.Logger
	StatusReader status.StatusReader
}

// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=routernodeconfigurationstatuses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=routernodeconfigurationstatuses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=routernodeconfigurationstatuses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *RouterNodeConfigurationStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("controller", "RouterNodeConfigurationStatus", "request", req.String())
	logger.Info("start reconcile")
	defer logger.Info("end reconcile")

	// Get or create RouterNodeConfigurationStatus for this node
	var routerNodeConfigurationStatus v1alpha1.RouterNodeConfigurationStatus
	err := r.Get(ctx, types.NamespacedName{
		Name:      r.MyNode,
		Namespace: r.MyNamespace,
	}, &routerNodeConfigurationStatus)

	if err != nil && !errors.IsNotFound(err) {
		logger.Error("failed to get RouterNodeConfigurationStatus", "error", err)
		return ctrl.Result{}, err
	}

	// Create resource if it doesn't exist
	if errors.IsNotFound(err) {
		if err := r.createRouterNodeStatus(ctx, &routerNodeConfigurationStatus); err != nil {
			return ctrl.Result{}, err
		}
		// Re-fetch the created resource to get the updated object with proper metadata
		if err := r.Get(ctx, types.NamespacedName{
			Name:      r.MyNode,
			Namespace: r.MyNamespace,
		}, &routerNodeConfigurationStatus); err != nil {
			logger.Error("failed to re-fetch created RouterNodeConfigurationStatus", "error", err)
			return ctrl.Result{}, err
		}
	}

	// Build status from shared state
	newStatus := r.buildStatus()

	// Only patch if status has changed
	if !r.statusEqual(routerNodeConfigurationStatus.Status, newStatus) {
		patch := client.MergeFrom(routerNodeConfigurationStatus.DeepCopy())
		routerNodeConfigurationStatus.Status = newStatus
		if err := r.Status().Patch(ctx, &routerNodeConfigurationStatus, patch); err != nil {
			logger.Error("failed to patch RouterNodeConfigurationStatus status", "error", err)
			return ctrl.Result{}, err
		}
		logger.Info("patched RouterNodeConfigurationStatus", "name", routerNodeConfigurationStatus.Name)
	}

	return ctrl.Result{}, nil
}

// createRouterNodeStatus creates a new RouterNodeConfigurationStatus resource
func (r *RouterNodeConfigurationStatusReconciler) createRouterNodeStatus(ctx context.Context, routerNodeStatus *v1alpha1.RouterNodeConfigurationStatus) error {
	// Get the Node resource to set up owner reference
	var node corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Name: r.MyNode}, &node); err != nil {
		r.Logger.Error("failed to get node", "node", r.MyNode, "error", err)
		return err
	}

	// Set up the RouterNodeConfigurationStatus resource
	routerNodeStatus.ObjectMeta = metav1.ObjectMeta{
		Name:      r.MyNode,
		Namespace: r.MyNamespace,
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: "v1",
				Kind:       "Node",
				Name:       node.Name,
				UID:        node.UID,
			},
		},
	}

	if err := r.Create(ctx, routerNodeStatus); err != nil {
		r.Logger.Error("failed to create RouterNodeConfigurationStatus", "error", err)
		return err
	}

	r.Logger.Info("successfully created RouterNodeConfigurationStatus", "name", routerNodeStatus.Name)
	return nil
}

// buildConditions creates Ready and Degraded conditions based on failure status
func (r *RouterNodeConfigurationStatusReconciler) buildConditions(failedCount int) []metav1.Condition {
	now := metav1.Now()

	readyCondition := metav1.Condition{
		Type:               "Ready",
		LastTransitionTime: now,
	}

	degradedCondition := metav1.Condition{
		Type:               "Degraded",
		LastTransitionTime: now,
	}

	if failedCount > 0 {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "ConfigurationFailed"
		readyCondition.Message = "Some OpenPERouter configurations failed"

		degradedCondition.Status = metav1.ConditionTrue
		degradedCondition.Reason = "ConfigurationFailed"
		degradedCondition.Message = r.buildFailureMessageFromCount(failedCount)
	} else {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "ConfigurationSuccessful"
		readyCondition.Message = "All OpenPERouter configurations are successful"

		degradedCondition.Status = metav1.ConditionFalse
		degradedCondition.Reason = "ConfigurationSuccessful"
		degradedCondition.Message = "All configurations are healthy"
	}

	return []metav1.Condition{readyCondition, degradedCondition}
}

// buildStatus creates the status from StatusReader's shared state
func (r *RouterNodeConfigurationStatusReconciler) buildStatus() v1alpha1.RouterNodeConfigurationStatusStatus {
	// Get aggregated status summary from StatusReader
	statusSummary := r.StatusReader.GetStatusSummary()

	// Convert to v1alpha1 FailedResource format
	failedResources := make([]v1alpha1.FailedResource, len(statusSummary.FailedResources))
	for i, failed := range statusSummary.FailedResources {
		failedResources[i] = v1alpha1.FailedResource{
			Kind:    string(failed.Kind),
			Name:    failed.Name,
			Message: failed.ErrorMessage,
		}
	}

	// Always set LastUpdateTime to now since we're updating the status
	lastUpdate := &metav1.Time{Time: time.Now()}

	// Build conditions
	conditions := r.buildConditions(len(failedResources))

	return v1alpha1.RouterNodeConfigurationStatusStatus{
		LastUpdateTime:  lastUpdate,
		FailedResources: failedResources,
		Conditions:      conditions,
	}
}

// buildFailureMessageFromCount creates a descriptive failure message
func (r *RouterNodeConfigurationStatusReconciler) buildFailureMessageFromCount(failedCount int) string {
	if failedCount > 0 {
		return fmt.Sprintf("%d resource(s) failed", failedCount)
	}
	return "Configuration failed"
}

// statusEqual compares two status objects for deep equality, ignoring timestamp differences
func (r *RouterNodeConfigurationStatusReconciler) statusEqual(a, b v1alpha1.RouterNodeConfigurationStatusStatus) bool {
	// Create copies to normalize timestamps
	aCopy := a.DeepCopy()
	bCopy := b.DeepCopy()

	// Normalize timestamps to ignore time differences
	aCopy.LastUpdateTime = nil
	bCopy.LastUpdateTime = nil

	// Normalize condition LastTransitionTime
	for i := range aCopy.Conditions {
		aCopy.Conditions[i].LastTransitionTime = metav1.Time{}
	}
	for i := range bCopy.Conditions {
		bCopy.Conditions[i].LastTransitionTime = metav1.Time{}
	}

	return reflect.DeepEqual(aCopy, bCopy)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RouterNodeConfigurationStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.StatusReader == nil {
		return fmt.Errorf("StatusReader is required but not set")
	}

	// Create predicate to only watch our node's RouterNodeConfigurationStatus
	nodeFilter := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetName() == r.MyNode && object.GetNamespace() == r.MyNamespace
	})

	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RouterNodeConfigurationStatus{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		WithEventFilter(nodeFilter).
		Named("routernodeconfigurationstatus")

	ctrlBuilder = ctrlBuilder.WatchesRawSource(
		source.Channel(r.StatusReader.GetChannel(), &handler.EnqueueRequestForObject{}),
	)

	return ctrlBuilder.Complete(r)
}
