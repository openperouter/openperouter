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

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/go-logr/logr"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/buildversion"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/logging"
	"github.com/openperouter/openperouter/internal/tlsconfig"
	"github.com/openperouter/openperouter/internal/webhooks"
)

const datapathGrout = "grout"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=network.openperouter.io,resources=l2vnis;l3passthroughs;l3vnis;l3vpns;rawfrrconfigs;underlays,verbs=get;list;watch

func main() {
	args := struct {
		metricsAddr   string
		secureMetrics bool
		enableHTTP2   bool
		tlsOpts       []func(*tls.Config)
		namespace     string
		logLevel      string
		webhookPort   int
		certDir       string
		datapath      string
	}{}

	flag.StringVar(&args.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&args.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&args.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&args.namespace, "namespace", "",
		"The namespace OpenPERouter is deployed in. Defaults to the POD_NAMESPACE environment variable.")
	flag.StringVar(&args.logLevel, "loglevel", "info", "Set the logging level (debug, info, warn, error).")
	flag.StringVar(&args.certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs",
		"The directory where certs are stored")
	flag.IntVar(&args.webhookPort, "webhook-port", 9443, "the port of the webhook service")
	flag.StringVar(&args.datapath, "datapath", "kernel", "The datapath to use (kernel or grout)")

	flag.Parse()

	logger, err := logging.New(args.logLevel)
	if err != nil {
		fmt.Println("unable to init logger", err)
		os.Exit(1)
	}
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

	setupLog.Info("version", "version", buildversion.Version())
	setupLog.Info("arguments", "args", fmt.Sprintf("%+v", args))

	if !args.enableHTTP2 {
		args.tlsOpts = append(args.tlsOpts, tlsconfig.DisableHTTP2())
	}

	operatorNamespace := args.namespace
	if operatorNamespace == "" {
		operatorNamespace = os.Getenv("POD_NAMESPACE")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   args.metricsAddr,
			SecureServing: args.secureMetrics,
			TLSOpts:       args.tlsOpts,
		},
		// The readiness and liveness endpoints are served by the webhook
		// server, so a replica is ready only once it can answer admission
		// requests.
		HealthProbeBindAddress: "0",
		WebhookServer: webhook.NewServer(
			webhook.Options{
				Port:    args.webhookPort,
				CertDir: args.certDir,
				TLSOpts: args.tlsOpts,
			},
		),
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	var datapathConfigValidator conversion.DatapathConfigValidator = &conversion.KernelDatapathConfigValidator{}
	if args.datapath == datapathGrout {
		datapathConfigValidator = &conversion.GroutDatapathConfigValidator{}
	}

	if err := setupWebhooks(mgr, logger, datapathConfigValidator, operatorNamespace); err != nil {
		setupLog.Error(err, "unable to create webhooks")
		os.Exit(1)
	}
	webhooks.SetupHealth(mgr)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setupWebhooks(
	mgr manager.Manager,
	logger *slog.Logger,
	configValidator conversion.DatapathConfigValidator,
	operatorNamespace string,
) error {
	webhooks.Logger = logger
	webhooks.WebhookClient = mgr.GetAPIReader()
	webhooks.DatapathConfigValidator = configValidator

	if err := webhooks.SetupL3VNI(mgr); err != nil {
		return fmt.Errorf("setting up the L3VNI webhook: %w", err)
	}
	if err := webhooks.SetupL2VNI(mgr); err != nil {
		return fmt.Errorf("setting up the L2VNI webhook: %w", err)
	}
	if err := webhooks.SetupL3VPN(mgr); err != nil {
		return fmt.Errorf("setting up the L3VPN webhook: %w", err)
	}
	if err := webhooks.SetupUnderlay(mgr); err != nil {
		return fmt.Errorf("setting up the Underlay webhook: %w", err)
	}
	if err := webhooks.SetupL3Passthrough(mgr); err != nil {
		return fmt.Errorf("setting up the L3Passthrough webhook: %w", err)
	}
	if err := webhooks.SetupRawFRRConfig(mgr, operatorNamespace); err != nil {
		return fmt.Errorf("setting up the RawFRRConfig webhook: %w", err)
	}
	return nil
}
