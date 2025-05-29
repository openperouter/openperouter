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

package operator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	openpev1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/envconfig"
	pkgerrors "github.com/pkg/errors"
)

const (
	defaultResourceName = "openperouter"
	chartPath           = "./bindata/deployment/openperouter"
)

var openperouterChartPath = chartPath // is mocked in tests

// OpenPERouterReconciler reconciles a OpenPERouter object
type OpenPERouterReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Logger    *slog.Logger
	Namespace string
	EnvConfig envconfig.EnvConfig
	chart     *helmChart
}

// Namespace Scoped
// +kubebuilder:rbac:groups=apps,namespace=openperouter-system,resources=deployments;daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",namespace=openperouter-system,resources=services;configmaps,verbs=create;delete;get;update;patch
// +kubebuilder:rbac:groups="",namespace=openperouter-system,resources=events,verbs=create;patch

// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=openperouters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=openperouters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=openperouters/finalizers,verbs=update

func (r *OpenPERouterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("controller", "OpenPERouter", "request", req.String())
	logger.Info("start reconcile")
	defer logger.Info("end reconcile")

	instance := &openpev1alpha1.OpenPERouter{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if req.Name != defaultResourceName {
		logger.Error("invalidResourceName", "name is:", req.Name, "must be:", defaultResourceName)
		if err := UpdateStatus(ctx, r.Client, instance, ConditionDegraded, "IncorrectResourceName", fmt.Sprintf("Resource name must be %s", defaultResourceName)); err != nil {
			slog.Error("statusUpdateFailed", "with err: ", err, "Desired status", ConditionDegraded)
			return ctrl.Result{}, nil
		}
		logger.Info("updated status successfully", "condition", ConditionDegraded, "resource name", req.Name)
		return ctrl.Result{}, nil
	}

	err = r.syncK8SResources(ctx, instance)
	if err != nil {
		slog.Error("syncResourcesFailed", "with err:", err)
		updateErr := UpdateStatus(ctx, r.Client, instance, ConditionDegraded, "FailedToSyncResources", err.Error())
		if updateErr != nil {
			err = fmt.Errorf("failed to sync k8s resources: %w, failed to update status: %w", err, updateErr)
		}
		return ctrl.Result{}, err
	}

	err = IsOpenPERouterAvailable(ctx, r.Client, r.Namespace)
	if errors.As(err, &ResourcesUnexpectedError{}) {
		err = UpdateStatus(ctx, r.Client, instance, ConditionDegraded, "FailedToCheckAvailability", err.Error())
		return ctrl.Result{Requeue: true}, err
	}
	if errors.As(err, &ResourcesNotReadyError{}) {
		err = UpdateStatus(ctx, r.Client, instance, ConditionProgressing, ConditionProgressing, "")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	err = UpdateStatus(ctx, r.Client, instance, ConditionAvailable, ConditionAvailable, "")
	return ctrl.Result{}, err
}

func (r *OpenPERouterReconciler) syncK8SResources(ctx context.Context, config *openpev1alpha1.OpenPERouter) error {
	objs, err := r.chart.Objects(r.EnvConfig, config)
	if err != nil {
		return err
	}

	for _, obj := range objs {
		objKind := obj.GetKind()
		// Skip applying role and role binding object, because with the operator these are being set outside,
		// either in manifests or via the csv.
		if objKind == "Role" || objKind == "RoleBinding" {
			continue
		}
		objNS := obj.GetNamespace()
		if objNS != "" { // Avoid setting reference on a cluster-scoped resource.
			if err := controllerutil.SetControllerReference(config, obj, r.Scheme); err != nil {
				return pkgerrors.Wrapf(err, "Failed to set controller reference to %s %s", objNS, obj.GetName())
			}
		}
		if err := ApplyObject(ctx, r.Client, obj); err != nil {
			return pkgerrors.Wrapf(err, "could not apply (%s) %s/%s", obj.GroupVersionKind(), objNS, obj.GetName())
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OpenPERouterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.chart, err = newChart(openperouterChartPath, defaultResourceName, r.Namespace)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&openpev1alpha1.OpenPERouter{}).
		Complete(r)
}
