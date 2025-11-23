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

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/cert-controller/pkg/rotator"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/controller/nodeindex"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/logging"
	"github.com/openperouter/openperouter/internal/tlsconfig"
	"github.com/openperouter/openperouter/internal/webhooks"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	// Webhook modes
	WebhookModeDisabled    = "disabled"
	WebhookModeEnabled     = "enabled"
	WebhookModeWebhookOnly = "webhookonly"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	args := struct {
		metricsAddr                   string
		secureMetrics                 bool
		enableHTTP2                   bool
		tlsOpts                       []func(*tls.Config)
		namespace                     string
		logLevel                      string
		webhookMode                   string
		webhookPort                   int
		disableCertRotation           bool
		restartOnRotatorSecretRefresh bool
		certDir                       string
		certServiceName               string
	}{}

	flag.StringVar(&args.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&args.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&args.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&args.namespace, "namespace", "",
		"The namespace to watch for resources. Leave empty for all namespaces.")
	flag.StringVar(&args.logLevel, "loglevel", "info", "Set the logging level (debug, info, warn, error).")
	flag.BoolVar(&args.disableCertRotation, "disable-cert-rotation", false,
		"disable automatic generation and rotation of webhook TLS certificates/keys")
	flag.BoolVar(&args.restartOnRotatorSecretRefresh, "restart-on-rotator-secret-refresh", false,
		"Restart the pod when the rotator refreshes its cert.")
	flag.StringVar(&args.certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs",
		"The directory where certs are stored")
	flag.StringVar(&args.certServiceName, "cert-service-name", "openpe-webhook-service",
		"The service name used to generate the TLS cert's hostname")
	flag.IntVar(&args.webhookPort, "webhook-port", 9443, "the port of the webhook service")
	flag.StringVar(&args.webhookMode, "webhookmode", WebhookModeEnabled, "webhook mode: disabled, enabled, or webhookonly")

	flag.Parse()

	switch args.webhookMode {
	case WebhookModeDisabled, WebhookModeEnabled, WebhookModeWebhookOnly:
	default:
		setupLog.Error(nil, "invalid webhook mode", "mode", args.webhookMode,
			"valid_modes", []string{WebhookModeDisabled, WebhookModeEnabled, WebhookModeWebhookOnly})
		os.Exit(1)
	}

	logger, err := logging.New(args.logLevel)
	if err != nil {
		fmt.Println("unable to init logger", err)
		os.Exit(1)
	}
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

	build, _ := debug.ReadBuildInfo()
	setupLog.Info("version", "version", build.Main.Version)
	setupLog.Info("arguments", "args", fmt.Sprintf("%+v", args))

	if !args.enableHTTP2 {
		args.tlsOpts = append(args.tlsOpts, tlsconfig.DisableHTTP2())
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   args.metricsAddr,
		SecureServing: args.secureMetrics,
		TLSOpts:       args.tlsOpts,
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Cache:   cache.Options{},
		Metrics: metricsServerOptions,
		WebhookServer: webhook.NewServer(
			webhook.Options{
				Port:    args.webhookPort,
				TLSOpts: args.tlsOpts,
			},
		),
	})

	startListeners := make(chan struct{})
	if !args.disableCertRotation && args.webhookMode != WebhookModeDisabled {
		setupLog.Info("Starting certs generator")
		if err := setupCertRotation(startListeners, mgr, logger, args.namespace,
			args.certDir, args.certServiceName, args.restartOnRotatorSecretRefresh); err != nil {
			setupLog.Error(err, "unable to set up cert rotator")
			os.Exit(1)
		}
	} else {
		close(startListeners)
	}

	signalHandlerContext := ctrl.SetupSignalHandler()
	go func() {
		<-startListeners

		if args.webhookMode != WebhookModeWebhookOnly {
			setupLog.Info("Starting controllers")
			if err = (&nodeindex.NodesReconciler{
				Client:   mgr.GetClient(),
				Scheme:   mgr.GetScheme(),
				LogLevel: args.logLevel,
				Logger:   logger,
			}).SetupWithManager(signalHandlerContext, mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "NodeReconciler")
				os.Exit(1)
			}
			// +kubebuilder:scaffold:builder
		}

		if args.webhookMode == WebhookModeEnabled || args.webhookMode == WebhookModeWebhookOnly {
			setupLog.Info("Starting webhooks")
			if err := v1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
				logger.Error("unable to add v1alpha1 scheme", "error", err)
			}

			err := setupWebhook(mgr, logger)
			if err != nil {
				setupLog.Error(err, "unable to create", "webhooks")
				os.Exit(1)
			}
			webhooks.SetupHealth(mgr)
		}
	}()

	setupLog.Info("starting manager")

	if err := mgr.Start(signalHandlerContext); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

const (
	caName         = "cert"
	caOrganization = "openperouter.io" //nolint:gosec
)

var (
	webhookName       = "openpe-validating-webhook-configuration"
	webhookSecretName = "openpe-webhook-server-cert" // #nosec G101
)

func setupCertRotation(notifyFinished chan struct{}, mgr manager.Manager, logger *slog.Logger,
	namespace, certDir, certServiceName string, restartOnSecretRefresh bool) error {
	webhooks := []rotator.WebhookInfo{
		{
			Name: webhookName,
			Type: rotator.Validating,
		},
	}

	logger.Info("setting up cert rotation", "op", "startup")
	err := rotator.AddRotator(mgr, &rotator.CertRotator{
		SecretKey: types.NamespacedName{
			Namespace: namespace,
			Name:      webhookSecretName,
		},
		CertDir:                certDir,
		CAName:                 caName,
		CAOrganization:         caOrganization,
		DNSName:                fmt.Sprintf("%s.%s.svc", certServiceName, namespace),
		IsReady:                notifyFinished,
		Webhooks:               webhooks,
		FieldOwner:             "openpe",
		RestartOnSecretRefresh: restartOnSecretRefresh,
	})
	if err != nil {
		logger.Error("unable to set up cert rotation", "error", err)
		return err
	}
	return nil
}

func setupWebhook(mgr manager.Manager, logger *slog.Logger) error {
	logger.Info("webhooks enabled")

	webhooks.Logger = logger
	webhooks.WebhookClient = mgr.GetAPIReader()
	webhooks.ValidateL3VNIs = conversion.ValidateL3VNIs
	webhooks.ValidateL2VNIs = conversion.ValidateL2VNIs
	webhooks.ValidateUnderlays = conversion.ValidateUnderlays
	webhooks.ValidateL3Passthroughs = conversion.ValidatePassthrough

	if err := webhooks.SetupL3VNI(mgr); err != nil {
		logger.Error("unable to create the webook", "error", err, "webhook", "L3VNIs")
		return err
	}
	if err := webhooks.SetupL2VNI(mgr); err != nil {
		logger.Error("unable to create the webook", "error", err, "webhook", "L2VNIs")
		return err
	}
	if err := webhooks.SetupUnderlay(mgr); err != nil {
		logger.Error("unable to create the webook", "error", err, "webhook", "Underlays")
		return err
	}
	if err := webhooks.SetupL3Passthrough(mgr); err != nil {
		logger.Error("unable to create the webook", "error", err, "webhook", "L3Passthroughs")
		return err
	}
	return nil
}
