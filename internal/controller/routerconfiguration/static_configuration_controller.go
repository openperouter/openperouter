// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/openperouter/openperouter/internal/frrconfig"
)

// StaticConfigReconciler reconciles configuration from a static file.
// It's designed for host mode where Kubernetes API may not be available.
type StaticConfigReconciler struct {
	Scheme          *runtime.Scheme
	Logger          *slog.Logger
	NodeIndex       int
	LogLevel        string
	FRRConfigPath   string
	FRRReloadSocket string
	GroutEnabled    bool
	GroutSocketPath string
	RouterProvider  RouterProvider
	ConfigDir       string

	TriggerChan chan event.GenericEvent
}

func (r *StaticConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.With("controller", "StaticConfigReconciler", "request", req.String())
	logger.Info("start reconcile")
	defer logger.Info("end reconcile")

	logger.Info("using config dir", "dir", r.ConfigDir)
	// Read and merge router configs from directory
	apiConfig, err := readStaticConfigs(r.ConfigDir)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to read static router configurations from %s: %w", r.ConfigDir, err)
	}

	logger.Info("using config",
		"nodeIndex", r.NodeIndex,
		"logLevel", r.LogLevel,
		"l3vnis", len(apiConfig.L3VNIs),
		"l2vnis", len(apiConfig.L2VNIs),
		"underlays", len(apiConfig.Underlays),
		"l3passthrough", len(apiConfig.L3Passthrough))

	router, err := r.RouterProvider.New(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get router instance: %w", err)
	}

	canReconcile, err := router.CanReconcile(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("canReconcile error: %w", err)
	}
	if !canReconcile {
		logger.Info("router not ready for reconciliation, will retry", "retryAfter", "10s")
		return ctrl.Result{Requeue: true, RequeueAfter: 2 * time.Second}, nil
	}

	targetNS, err := router.TargetNS(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to retrieve target namespace: %w", err)
	}

	updater := frrconfig.UpdaterForSocket(r.FRRReloadSocket, r.FRRConfigPath)

	err = Reconcile(ctx, apiConfig, false, r.GroutEnabled, r.GroutSocketPath, r.NodeIndex, r.LogLevel, r.FRRConfigPath, targetNS, updater)
	if nonRecoverableHostError(err) {
		if err := router.HandleNonRecoverableError(ctx); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to handle non recoverable error: %w", err)
		}
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to configure the host: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *StaticConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.TriggerChan = make(chan event.GenericEvent, 1)

	go func(triggerChan chan<- event.GenericEvent) {
		triggerChan <- event.GenericEvent{
			Object: &metav1.PartialObjectMetadata{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-config-trigger",
					Namespace: "default",
				},
			},
		}
	}(r.TriggerChan)

	return ctrl.NewControllerManagedBy(mgr).
		Named("static-config-controller").
		WatchesRawSource(source.Channel(r.TriggerChan, &handler.EnqueueRequestForObject{})).
		Complete(r)
}

func (r *StaticConfigReconciler) TriggerReconcile() {
	select {
	case r.TriggerChan <- event.GenericEvent{
		Object: &metav1.PartialObjectMetadata{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "static-config-trigger",
				Namespace: "default",
			},
		},
	}:
		r.Logger.Info("triggered static config reconciliation")
	default:
		r.Logger.Debug("reconciliation already queued, skipping trigger")
	}
}
