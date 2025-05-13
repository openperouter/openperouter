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
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/go-logr/logr"
	periov1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/controller/nodeindex"
	"github.com/openperouter/openperouter/internal/logging"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(periov1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	args := struct {
		metricsAddr string
		probeAddr   string
		namespace   string
		logLevel    string
	}{}

	flag.StringVar(&args.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&args.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&args.namespace, "namespace", "",
		"The namespace to watch for resources. Leave empty for all namespaces.")
	flag.StringVar(&args.logLevel, "loglevel", "info", "Set the logging level (debug, info, warn, error).")

	flag.Parse()

	logger, err := logging.New(args.logLevel)
	if err != nil {
		fmt.Println("unable to init logger", err)
		os.Exit(1)
	}
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

	/* TODO: to be used for the metrics endpoints while disabiling
	http2
	tlsOpts = append(tlsOpts, func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	})*/
	build, _ := debug.ReadBuildInfo()
	setupLog.Info("version", "version", build.Main.Version)
	setupLog.Info("arguments", "args", fmt.Sprintf("%+v", args))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: args.probeAddr,
		Cache:                  cache.Options{},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	signalHandlerContext := ctrl.SetupSignalHandler()
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

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(signalHandlerContext); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
