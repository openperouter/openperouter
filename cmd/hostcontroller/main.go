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
	"time"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/go-logr/logr"
	periov1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/controller/routerconfiguration"
	"github.com/openperouter/openperouter/internal/logging"
	"github.com/openperouter/openperouter/internal/pods"
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
		metricsAddr   string
		probeAddr     string
		secureMetrics bool
		enableHTTP2   bool
		nodeName      string
		namespace     string
		logLevel      string
		frrConfigPath string
		reloadPort    int
		criSocket     string
	}{}
	flag.StringVar(&args.metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&args.probeAddr, "health-probe-bind-address", ":9081", "The address the probe endpoint binds to.")
	flag.BoolVar(&args.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&args.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&args.nodeName, "nodename", "", "The name of the node the controller runs on")
	flag.StringVar(&args.namespace, "namespace", "", "The namespace the controller runs in")
	flag.StringVar(&args.logLevel, "loglevel", "info", "the verbosity of the process")
	flag.StringVar(&args.frrConfigPath, "frrconfig", "/etc/perouter/frr/frr.conf",
		"the location of the frr configuration file")
	flag.IntVar(&args.reloadPort, "reloadport", 9080, "the port of the reloader process")
	flag.StringVar(&args.criSocket, "crisocket", "/var/run/containerd/containerd.sock", "the location of the cri socket")

	flag.Parse()

	logger, err := logging.New(args.logLevel)
	if err != nil {
		fmt.Println("unable to init logger", err)
		os.Exit(1)
	}
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))
	build, _ := debug.ReadBuildInfo()
	setupLog.Info("version", "version", build.Main.Version)
	setupLog.Info("arguments", "args", fmt.Sprintf("%+v", args))

	/* TODO: to be used for the metrics endpoints while disabiling
	http2
	tlsOpts = append(tlsOpts, func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	})*/

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: args.probeAddr,
		Cache:                  cache.Options{},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	podRuntime, err := pods.NewRuntime(args.criSocket, 5*time.Minute)
	if err != nil {
		setupLog.Error(err, "connect to crio")
		os.Exit(1)
	}

	if err = (&routerconfiguration.PERouterReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		MyNode:      args.nodeName,
		FRRConfig:   args.frrConfigPath,
		ReloadPort:  args.reloadPort,
		PodRuntime:  podRuntime,
		LogLevel:    args.logLevel,
		Logger:      logger,
		MyNamespace: args.namespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Underlay")
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
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
