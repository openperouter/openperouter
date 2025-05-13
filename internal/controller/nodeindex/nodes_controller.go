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

package nodeindex

import (
	"context"
	"log/slog"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	v1 "k8s.io/api/core/v1"
)

type requestKey string

type NodesReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	LogLevel string
	Logger   *slog.Logger
}

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update

func (r *NodesReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("controller", "NodeIndex", "request", req.String())
	logger.Info("start reconcile")
	defer logger.Info("end reconcile")

	ctx = context.WithValue(ctx, requestKey("request"), req.String())

	var nodes v1.NodeList
	if err := r.List(ctx, &nodes); err != nil {
		slog.Error("failed to list nodes", "error", err)
		return ctrl.Result{}, err
	}

	nodesToAnnotate := nodesToAnnotate(nodes.Items)
	for _, n := range nodesToAnnotate {
		if err := r.Update(ctx, &n); err != nil {
			slog.Error("failed to update node", "node", n.Name, "error", err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *NodesReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Node{}).
		WithEventFilter(predicate.AnnotationChangedPredicate{}).
		Named("nodecontroller").
		Complete(r)
}
