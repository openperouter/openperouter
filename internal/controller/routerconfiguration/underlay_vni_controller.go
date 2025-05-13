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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/pods"
	v1 "k8s.io/api/core/v1"
)

type PERouterReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	MyNode      string
	MyNamespace string
	FRRConfig   string
	ReloadPort  int
	PodRuntime  *pods.Runtime
	LogLevel    string
	Logger      *slog.Logger
}

type requestKey string

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=vnis,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=vnis/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=vnis/finalizers,verbs=update
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=underlays,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=underlays/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=openpe.openperouter.github.io,resources=underlays/finalizers,verbs=update

func (r *PERouterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("controller", "RouterConfiguration", "request", req.String())
	logger.Info("start reconcile")
	defer logger.Info("end reconcile")

	ctx = context.WithValue(ctx, requestKey("request"), req.String())

	nodeIndex, err := nodeIndex(ctx, r.Client, r.MyNode)
	if err != nil {
		slog.Error("failed to fetch node index", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}
	routerPod, err := routerPodForNode(ctx, r.Client, r.MyNode)
	if err != nil {
		slog.Error("failed to fetch router pod", "node", r.MyNode, "error", err)
		return ctrl.Result{}, err
	}
	routerPodIsReady := PodIsReady(routerPod)

	if !routerPodIsReady {
		logger.Info("router pod", "Pod", routerPod.Name, "event", "is not ready, waiting for it to be ready before configuring")
		return ctrl.Result{}, nil
	}

	var underlays v1alpha1.UnderlayList
	if err := r.List(ctx, &underlays); err != nil {
		slog.Error("failed to list underlays", "error", err)
		return ctrl.Result{}, err
	}

	if err := conversion.ValidateUnderlays(underlays.Items); err != nil {
		slog.Error("failed to validate underlays", "error", err)
		return ctrl.Result{}, nil
	}

	var vnis v1alpha1.VNIList
	if err := r.List(ctx, &vnis); err != nil {
		slog.Error("failed to list vnis", "error", err)
		return ctrl.Result{}, err
	}
	if err := conversion.ValidateVNIs(vnis.Items); err != nil {
		slog.Error("failed to validate vnis", "error", err)
		return ctrl.Result{}, nil
	}
	logger.Debug("using config", "vnis", vnis.Items, "underlays", underlays.Items)

	if err := configureFRR(ctx, frrConfigData{
		configFile: r.FRRConfig,
		address:    routerPod.Status.PodIP,
		port:       r.ReloadPort,
		nodeIndex:  nodeIndex,
		underlays:  underlays.Items,
		logLevel:   r.LogLevel,
		vnis:       vnis.Items,
	}); err != nil {
		slog.Error("failed to reload frr config", "error", err)
		return ctrl.Result{}, err
	}

	err = configureInterfaces(ctx, interfacesConfiguration{
		RouterPodUUID: string(routerPod.UID),
		PodRuntime:    *r.PodRuntime,
		NodeIndex:     nodeIndex,
		Underlays:     underlays.Items,
		Vnis:          vnis.Items,
	})

	if nonRecoverableHostError(err) {
		logger.Info("breaking configuration change", "killing pod", routerPod.Name)
		if err := r.Delete(ctx, routerPod); err != nil && !errors.IsNotFound(err) {
			slog.Error("failed to delete router pod", "error", err)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if err != nil {
		slog.Error("failed to configure the host", "error", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PERouterReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
				return false
			case *v1.Pod: // handle only status updates
				old := e.ObjectOld.(*v1.Pod)
				if PodIsReady(old) != PodIsReady(o) {
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
		Watches(&v1.Pod{}, &handler.EnqueueRequestForObject{}).
		Watches(&v1alpha1.VNI{}, &handler.EnqueueRequestForObject{}).
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

// PodIsReady returns the given pod's PodReady and ContainersReady condition.
func PodIsReady(p *v1.Pod) bool {
	return podConditionStatus(p, v1.PodReady) == v1.ConditionTrue && podConditionStatus(p, v1.ContainersReady) == v1.ConditionTrue
}

// podConditionStatus returns the status of the condition for a given pod.
func podConditionStatus(p *v1.Pod, condition v1.PodConditionType) v1.ConditionStatus {
	if p == nil {
		return v1.ConditionUnknown
	}

	for _, c := range p.Status.Conditions {
		if c.Type == condition {
			return c.Status
		}
	}

	return v1.ConditionUnknown
}
