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
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/go-logr/logr"
	"github.com/open-policy-agent/cert-controller/pkg/rotator"
	"github.com/openperouter/openperouter/internal/buildversion"
	"github.com/openperouter/openperouter/internal/logging"
	"github.com/openperouter/openperouter/internal/tlsconfig"
)

const (
	caName                   = "cert"
	caOrganization           = "openperouter.io"
	fieldOwner               = "openpe"
	webhookConfigurationName = "openpe-validating-webhook-configuration"
	webhookSecretName        = "openpe-webhook-server-cert" // #nosec G101
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,resourceNames="openpe-validating-webhook-configuration",verbs=update

func main() {
	args := struct {
		metricsAddr     string
		secureMetrics   bool
		enableHTTP2     bool
		tlsOpts         []func(*tls.Config)
		probeAddr       string
		namespace       string
		logLevel        string
		certServiceName string
		certDir         string
	}{}

	flag.StringVar(&args.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&args.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&args.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics server")
	flag.StringVar(&args.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&args.namespace, "namespace", "",
		"The namespace holding the webhook Service and its certificate Secret.")
	flag.StringVar(&args.logLevel, "loglevel", "info", "Set the logging level (debug, info, warn, error).")
	flag.StringVar(&args.certServiceName, "cert-service-name", "openpe-webhook-service",
		"The service name used to generate the TLS cert's hostname")
	flag.StringVar(&args.certDir, "cert-dir", "/tmp/k8s-webhook-server/serving-certs",
		"The directory where the webhook certificate Secret is mounted.")

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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   args.metricsAddr,
			SecureServing: args.secureMetrics,
			TLSOpts:       args.tlsOpts,
		},
		HealthProbeBindAddress: args.probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	// The rotator closes this channel once the certificate is mounted in
	// certDir and the CA bundle is injected in the webhook configuration.
	certsReady := make(chan struct{})

	if err := rotator.AddRotator(mgr, &rotator.CertRotator{
		SecretKey: types.NamespacedName{
			Namespace: args.namespace,
			Name:      webhookSecretName,
		},
		CAName:         caName,
		CAOrganization: caOrganization,
		DNSName:        fmt.Sprintf("%s.%s.svc", args.certServiceName, args.namespace),
		// The rotator exits unless it finds the certificate in this directory.
		CertDir: args.certDir,
		IsReady: certsReady,
		Webhooks: []rotator.WebhookInfo{
			{
				Name: webhookConfigurationName,
				Type: rotator.Validating,
			},
		},
		FieldOwner: fieldOwner,
	}); err != nil {
		setupLog.Error(err, "unable to set up cert rotation")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", func(_ *http.Request) error {
		select {
		case <-certsReady:
			return nil
		default:
			return errors.New("certificate not ready")
		}
	}); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
