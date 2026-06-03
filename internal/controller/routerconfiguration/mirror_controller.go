// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/staticconfiguration"
)

// MirrorController mirrors static configuration files to Kubernetes CRDs.
type MirrorController struct {
	client.Client
	Scheme      *runtime.Scheme
	Logger      *slog.Logger
	MyNode      string
	MyNamespace string
	ConfigDir   string
	TriggerChan chan event.GenericEvent
}

func (r *MirrorController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("controller", "MirrorController", "request", req.String())
	logger.Info("start reconcile")
	defer logger.Info("end reconcile")

	// Read static config from files (returns resources with names, namespace, labels, node selector already set)
	var noConfigErr *staticconfiguration.NoConfigAvailable
	staticConfig, err := readStaticConfigs(r.ConfigDir, r.MyNode, r.MyNamespace)
	if errors.As(err, &noConfigErr) {
		logger.Info("no static configuration available, cleaning up mirrored resources", "dir", r.ConfigDir)
		staticConfig = conversion.APIConfigData{}
	} else if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to read static configs from %s: %w", r.ConfigDir, err)
	}

	if err := r.mirrorAndClean(ctx, &v1alpha1.UnderlayList{}, toObjects(staticConfig.Underlays), logger, "underlay"); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.mirrorAndClean(ctx, &v1alpha1.L3VNIList{}, toObjects(staticConfig.L3VNIs), logger, "l3vni"); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.mirrorAndClean(ctx, &v1alpha1.L2VNIList{}, toObjects(staticConfig.L2VNIs), logger, "l2vni"); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.mirrorAndClean(ctx, &v1alpha1.L3PassthroughList{}, toObjects(staticConfig.L3Passthrough), logger, "l3passthrough"); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.mirrorAndClean(ctx, &v1alpha1.RawFRRConfigList{}, toObjects(staticConfig.RawFRRConfigs), logger, "rawfrrconfig"); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// mirrorAndClean handles mirror + delete for a single resource type.
// It lists existing mirrored resources and skips create/update if the resource already matches.
func (r *MirrorController) mirrorAndClean(ctx context.Context, listObj client.ObjectList, desired []client.Object, logger *slog.Logger, kind string) error {
	matchLabels := client.MatchingLabels{
		StaticSourceLabel: StaticSourceValue,
		StaticNodeLabel:   r.MyNode,
	}
	inNamespace := client.InNamespace(r.MyNamespace)

	if err := r.List(ctx, listObj, matchLabels, inNamespace); err != nil {
		return fmt.Errorf("failed to list mirrored %ss: %w", kind, err)
	}

	existing := extractItems(listObj)
	existingByName := map[string]client.Object{}
	for _, item := range existing {
		existingByName[item.GetName()] = item
	}

	desiredNames := map[string]bool{}
	for _, d := range desired {
		desiredNames[d.GetName()] = true
		if ex, ok := existingByName[d.GetName()]; ok && resourceMatchesDesired(ex, d) {
			continue
		}
		if _, err := r.createOrUpdateMirrored(ctx, d, logger, kind); err != nil {
			return err
		}
	}

	for _, item := range existing {
		if desiredNames[item.GetName()] {
			continue
		}
		logger.Info("deleting stale mirrored resource", "kind", kind, "name", item.GetName())
		if err := r.Delete(ctx, item); err != nil {
			return fmt.Errorf("failed to delete stale %s %s: %w", kind, item.GetName(), err)
		}
	}

	return nil
}

// createOrUpdateMirrored creates or updates a single mirrored K8s resource.
// It uses type assertion to copy the spec from the desired object.
func (r *MirrorController) createOrUpdateMirrored(ctx context.Context, desired client.Object, logger *slog.Logger, kind string) (controllerutil.OperationResult, error) {
	obj := newEmptyObject(desired)
	obj.SetName(desired.GetName())
	obj.SetNamespace(desired.GetNamespace())
	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.SetLabels(desired.GetLabels())
		copySpec(desired, obj)
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("failed to create/update %s %s: %w", kind, desired.GetName(), err)
	}
	logger.Info("mirrored resource", "kind", kind, "name", desired.GetName(), "result", result)
	return result, nil
}

func newEmptyObject(obj client.Object) client.Object {
	switch obj.(type) {
	case *v1alpha1.Underlay:
		return &v1alpha1.Underlay{}
	case *v1alpha1.L3VNI:
		return &v1alpha1.L3VNI{}
	case *v1alpha1.L2VNI:
		return &v1alpha1.L2VNI{}
	case *v1alpha1.L3Passthrough:
		return &v1alpha1.L3Passthrough{}
	case *v1alpha1.RawFRRConfig:
		return &v1alpha1.RawFRRConfig{}
	}
	return nil
}

func copySpec(src, dst client.Object) {
	switch s := src.(type) {
	case *v1alpha1.Underlay:
		dst.(*v1alpha1.Underlay).Spec = s.Spec
	case *v1alpha1.L3VNI:
		dst.(*v1alpha1.L3VNI).Spec = s.Spec
	case *v1alpha1.L2VNI:
		dst.(*v1alpha1.L2VNI).Spec = s.Spec
	case *v1alpha1.L3Passthrough:
		dst.(*v1alpha1.L3Passthrough).Spec = s.Spec
	case *v1alpha1.RawFRRConfig:
		dst.(*v1alpha1.RawFRRConfig).Spec = s.Spec
	}
}

func resourceMatchesDesired(existing, desired client.Object) bool {
	if !reflect.DeepEqual(existing.GetLabels(), desired.GetLabels()) {
		return false
	}
	switch e := existing.(type) {
	case *v1alpha1.Underlay:
		return reflect.DeepEqual(e.Spec, desired.(*v1alpha1.Underlay).Spec)
	case *v1alpha1.L3VNI:
		return reflect.DeepEqual(e.Spec, desired.(*v1alpha1.L3VNI).Spec)
	case *v1alpha1.L2VNI:
		return reflect.DeepEqual(e.Spec, desired.(*v1alpha1.L2VNI).Spec)
	case *v1alpha1.L3Passthrough:
		return reflect.DeepEqual(e.Spec, desired.(*v1alpha1.L3Passthrough).Spec)
	case *v1alpha1.RawFRRConfig:
		return reflect.DeepEqual(e.Spec, desired.(*v1alpha1.RawFRRConfig).Spec)
	}
	return false
}

// toObjects converts a typed slice of K8s resources to a slice of client.Object.
func toObjects[T any, PT interface {
	*T
	client.Object
}](items []T) []client.Object {
	result := make([]client.Object, len(items))
	for i := range items {
		result[i] = PT(&items[i])
	}
	return result
}

// extractItems extracts the Items field from any ObjectList using reflection.
func extractItems(listObj client.ObjectList) []client.Object {
	v := reflect.ValueOf(listObj).Elem()
	itemsField := v.FieldByName("Items")
	if !itemsField.IsValid() {
		return nil
	}

	items := make([]client.Object, itemsField.Len())
	for i := 0; i < itemsField.Len(); i++ {
		items[i] = itemsField.Index(i).Addr().Interface().(client.Object)
	}
	return items
}

// SetupWithManager registers the mirror controller with the manager.
func (r *MirrorController) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate that matches only resources with the static source label for this node
	staticSourcePredicate := predicate.NewPredicateFuncs(func(object client.Object) bool {
		labels := object.GetLabels()
		if labels == nil {
			return false
		}
		return labels[StaticSourceLabel] == StaticSourceValue && labels[StaticNodeLabel] == r.MyNode
	})

	b := ctrl.NewControllerManagedBy(mgr).
		Named("mirror-controller").
		WatchesRawSource(source.Channel(r.TriggerChan, &handler.EnqueueRequestForObject{})).
		Watches(&v1alpha1.Underlay{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(staticSourcePredicate)).
		Watches(&v1alpha1.L3VNI{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(staticSourcePredicate)).
		Watches(&v1alpha1.L2VNI{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(staticSourcePredicate)).
		Watches(&v1alpha1.L3Passthrough{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(staticSourcePredicate)).
		Watches(&v1alpha1.RawFRRConfig{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(staticSourcePredicate))

	return b.Complete(r)
}
