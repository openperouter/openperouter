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
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/frrconfig"
	"github.com/openperouter/openperouter/internal/k8s"
	"github.com/openperouter/openperouter/internal/status"
	v1 "k8s.io/api/core/v1"
)

type PERouterReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	MyNode             string
	MyNamespace        string
	LogLevel           string
	Logger             *slog.Logger
	UnderlayFromMultus bool
	FRRConfigPath      string
	FRRReloadSocket    string
	RouterProvider     RouterProvider
	StatusReporter     status.StatusReporter
}

type requestKey string

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l3vnis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l3vnis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l3vnis/finalizers,verbs=update
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l2vnis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l2vnis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l2vnis/finalizers,verbs=update
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=underlays,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=underlays/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=underlays/finalizers,verbs=update
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l3passthroughs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l3passthroughs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=l3passthroughs/finalizers,verbs=update

func (r *PERouterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("controller", "RouterConfiguration", "request", req.String())
	logger.Info("start reconcile")
	defer logger.Info("end reconcile")

	ctx = context.WithValue(ctx, requestKey("request"), req.String())

	var underlays v1alpha1.UnderlayList
	if err := r.List(ctx, &underlays); err != nil {
		slog.Error("failed to list underlays", "error", err)
		return ctrl.Result{}, err
	}

	var l3vnis v1alpha1.L3VNIList
	if err := r.List(ctx, &l3vnis); err != nil {
		slog.Error("failed to list l3vnis", "error", err)
		return ctrl.Result{}, err
	}

	var l2vnis v1alpha1.L2VNIList
	if err := r.List(ctx, &l2vnis); err != nil {
		slog.Error("failed to list l2vnis", "error", err)
		return ctrl.Result{}, err
	}

	var l3passthrough v1alpha1.L3PassthroughList
	if err := r.List(ctx, &l3passthrough); err != nil {
		slog.Error("failed to list l3passthrough", "error", err)
		return ctrl.Result{}, err
	}

	node := &v1.Node{}
	if err := r.Get(ctx, client.ObjectKey{Name: r.MyNode}, node); err != nil {
		slog.Error("failed to get node", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}

	// Filter resources by node selector
	filteredUnderlays, err := filter.UnderlaysForNode(node, underlays.Items)
	if err != nil {
		slog.Error("failed to filter underlays for node", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}

	filteredL3VNIs, err := filter.L3VNIsForNode(node, l3vnis.Items)
	if err != nil {
		slog.Error("failed to filter l3vnis for node", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}

	filteredL2VNIs, err := filter.L2VNIsForNode(node, l2vnis.Items)
	if err != nil {
		slog.Error("failed to filter l2vnis for node", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}

	filteredL3Passthrough, err := filter.L3PassthroughsForNode(node, l3passthrough.Items)
	if err != nil {
		slog.Error("failed to filter l3passthrough for node", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}
	nodeIndex, err := r.RouterProvider.NodeIndex(ctx)
	if err != nil {
		slog.Error("failed to get node index", "error", err)
		return ctrl.Result{}, err
	}

	logger.Debug("using config", "l3vnis", l3vnis.Items, "l2vnis", l2vnis.Items, "underlays", underlays.Items, "l3passthrough", l3passthrough.Items)
	apiConfig := conversion.ApiConfigData{
		NodeIndex:          nodeIndex,
		UnderlayFromMultus: r.UnderlayFromMultus,
		Underlays:          filteredUnderlays,
		LogLevel:           r.LogLevel,
		L3VNIs:             filteredL3VNIs,
		L2VNIs:             filteredL2VNIs,
		L3Passthrough:      filteredL3Passthrough,
	}

	router, err := r.RouterProvider.New(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get router pod instance: %w", err)
	}

	targetNS, err := router.TargetNS(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to retrieve target namespace: %w", err)
	}
	canReconcile, err := router.CanReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to check if router can be reconciled: %w", err)
	}
	if !canReconcile {
		logger.Info("router is not ready for reconciliation, requeueing")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	updater := frrconfig.UpdaterForSocket(r.FRRReloadSocket, r.FRRConfigPath)

	r.cleanupRemovedFailedResources(underlays.Items, l3vnis.Items, l2vnis.Items, l3passthrough.Items)

	err = Reconcile(ctx, apiConfig, r.FRRConfigPath, targetNS, updater, r.StatusReporter)
	if nonRecoverableHostError(err) {
		if err := router.HandleNonRecoverableError(ctx); err != nil {
			slog.Error("failed to handle non recoverable error", "error", err)
			return ctrl.Result{}, err
		}
	}
	if err != nil {
		slog.Error("failed to configure the host", "error", err)
		return ctrl.Result{}, err
	}

	// NOTE: All resources should be already set as success by now. But just for a good measure, if we got this far, everything should be confirmed as well.
	r.reportUnderlayConfigurationSuccess(underlays.Items)
	r.reportL2VNIConfigurationSuccess(l2vnis.Items)
	r.reportL3VNIConfigurationSuccess(l3vnis.Items)
	r.reportL3PassthroughConfigurationSuccess(l3passthrough.Items)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PERouterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.StatusReporter == nil {
		return fmt.Errorf("StatusReporter is required but not set")
	}

	filterNonRouterPods := predicate.NewPredicateFuncs(func(object client.Object) bool {
		switch o := object.(type) {
		case *v1.Pod:
			if o.Spec.NodeName != r.MyNode {
				return false
			}
			if o.Namespace != r.MyNamespace {
				return false
			}

			if o.Labels != nil && o.Labels["app"] == "router" { // interested only in the router pod
				return true
			}
			return false
		default:
			return true
		}
	})

	filterUpdates := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch o := e.ObjectNew.(type) {
			case *v1.Node:
				// Only reconcile if this is our node and labels changed
				if o.Name != r.MyNode {
					return false
				}
				old := e.ObjectOld.(*v1.Node)
				oldLabels := labels.Set(old.Labels)
				newLabels := labels.Set(o.Labels)
				return !labels.Equals(oldLabels, newLabels)
			case *v1.Pod: // handle only status updates
				old := e.ObjectOld.(*v1.Pod)
				if k8s.PodIsReady(old) != k8s.PodIsReady(o) {
					return true
				}
				return false
			}
			return true
		},
	}

	if err := setPodNodeNameIndex(mgr); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Underlay{}).
		Watches(&v1.Node{}, &handler.EnqueueRequestForObject{}).
		Watches(&v1.Pod{}, &handler.EnqueueRequestForObject{}).
		Watches(&v1alpha1.L3VNI{}, &handler.EnqueueRequestForObject{}).
		Watches(&v1alpha1.L2VNI{}, &handler.EnqueueRequestForObject{}).
		Watches(&v1alpha1.L3Passthrough{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(filterNonRouterPods).
		WithEventFilter(filterUpdates).
		Named("routercontroller").
		Complete(r)
}

func setPodNodeNameIndex(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.Pod{}, nodeNameIndex, func(rawObj client.Object) []string {
		pod, ok := rawObj.(*v1.Pod)
		if pod == nil {
			slog.Error("podindexer", "error", "received nil pod")
			return nil
		}
		if !ok {
			slog.Error("podindexer", "error", "received object that is not pod", "object", rawObj.GetObjectKind().GroupVersionKind().Kind)
			return nil
		}
		if pod.Spec.NodeName != "" {
			return []string{pod.Spec.NodeName}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to set node indexer %w", err)
	}
	return nil
}

func (r *PERouterReconciler) reportUnderlayConfigurationSuccess(underlays []v1alpha1.Underlay) {
	for _, underlay := range underlays {
		r.StatusReporter.ReportResourceSuccess(status.UnderlayKind, underlay.Name)
	}
}

func (r *PERouterReconciler) reportL2VNIConfigurationSuccess(l2vnis []v1alpha1.L2VNI) {
	for _, l2vni := range l2vnis {
		r.StatusReporter.ReportResourceSuccess(status.L2VNIKind, l2vni.Name)
	}
}

func (r *PERouterReconciler) reportL3VNIConfigurationSuccess(l3vnis []v1alpha1.L3VNI) {
	for _, l3vni := range l3vnis {
		r.StatusReporter.ReportResourceSuccess(status.L3VNIKind, l3vni.Name)
	}
}

func (r *PERouterReconciler) reportL3PassthroughConfigurationSuccess(l3passthroughs []v1alpha1.L3Passthrough) {
	for _, l3passthrough := range l3passthroughs {
		r.StatusReporter.ReportResourceSuccess(status.L3PassthroughKind, l3passthrough.Name)
	}
}

func (r *PERouterReconciler) cleanupRemovedFailedResources(
	underlays []v1alpha1.Underlay,
	l3vnis []v1alpha1.L3VNI,
	l2vnis []v1alpha1.L2VNI,
	l3passthrough []v1alpha1.L3Passthrough,
) {
	// Build sets of current resources for fast lookup
	currentUnderlays := make(map[string]bool)
	for _, underlay := range underlays {
		currentUnderlays[underlay.Name] = true
	}

	currentL3VNIs := make(map[string]bool)
	for _, l3vni := range l3vnis {
		currentL3VNIs[l3vni.Name] = true
	}

	currentL2VNIs := make(map[string]bool)
	for _, l2vni := range l2vnis {
		currentL2VNIs[l2vni.Name] = true
	}

	currentL3Passthroughs := make(map[string]bool)
	for _, passthrough := range l3passthrough {
		currentL3Passthroughs[passthrough.Name] = true
	}

	// Get current failed resources and check if they still exist
	if statusReader, ok := r.StatusReporter.(status.StatusReader); ok {
		statusSummary := statusReader.GetStatusSummary()
		for _, failedResource := range statusSummary.FailedResources {
			var exists bool
			switch failedResource.Kind {
			case status.UnderlayKind:
				exists = currentUnderlays[failedResource.Name]
			case status.L3VNIKind:
				exists = currentL3VNIs[failedResource.Name]
			case status.L2VNIKind:
				exists = currentL2VNIs[failedResource.Name]
			case status.L3PassthroughKind:
				exists = currentL3Passthroughs[failedResource.Name]
			}

			// If the failed resource no longer exists, report it as removed
			if !exists {
				r.StatusReporter.ReportResourceRemoved(failedResource.Kind, failedResource.Name)
			}
		}
	}
}
