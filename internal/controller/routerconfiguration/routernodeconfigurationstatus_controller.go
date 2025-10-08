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
	"log/slog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

// RouterNodeConfigurationStatusReconciler reconciles a RouterNodeConfigurationStatus object
type RouterNodeConfigurationStatusReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	MyNode      string
	MyNamespace string
	Logger      *slog.Logger
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

	// This controller only creates resources during daemon set startup
	// It does not perform reconciliation loops

	// Check if RouterNodeConfigurationStatus already for this node
	var routerNodeConfigurationStatus v1alpha1.RouterNodeConfigurationStatus
	err := r.Get(ctx, types.NamespacedName{
		Name:      r.MyNode,
		Namespace: r.MyNamespace,
	}, &routerNodeConfigurationStatus)

	if err != nil && !errors.IsNotFound(err) {
		logger.Error("failed to get RouterNodeConfigurationStatus", "error", err)
		return ctrl.Result{}, err
	}

	// If resource already exists, no need to create it again
	if err == nil {
		logger.Info("RouterNodeConfigurationStatus already exists", "name", routerNodeConfigurationStatus.Name)
		return ctrl.Result{}, nil
	}

	// Get the Node resource to set up owner reference
	var node corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Name: r.MyNode}, &node); err != nil {
		logger.Error("failed to get node", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}

	// Create RouterNodeConfigurationStatus resource
	routerNodeStatus := &v1alpha1.RouterNodeConfigurationStatus{
		ObjectMeta: metav1.ObjectMeta{
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
		},
	}

	if err := r.Create(ctx, routerNodeStatus); err != nil {
		logger.Error("failed to create RouterNodeConfigurationStatus", "error", err)
		return ctrl.Result{}, err
	}

	// Initialize status fields with a separate status update
	now := metav1.Now()
	routerNodeStatus.Status = v1alpha1.RouterNodeConfigurationStatusStatus{
		LastUpdateTime:  &now,
		FailedResources: []v1alpha1.FailedResource{},
		Conditions:      []metav1.Condition{},
	}

	if err := r.Status().Update(ctx, routerNodeStatus); err != nil {
		logger.Error("failed to update RouterNodeConfigurationStatus status", "error", err)
		return ctrl.Result{}, err
	}

	logger.Info("successfully created RouterNodeConfigurationStatus", "name", routerNodeStatus.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RouterNodeConfigurationStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RouterNodeConfigurationStatus{}).
		Named("routernodeconfigurationstatus").
		Complete(r)
}
