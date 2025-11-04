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
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

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
	"github.com/openperouter/openperouter/internal/staticconfiguration"
	"github.com/openperouter/openperouter/internal/systemdctl"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	// +kubebuilder:scaffold:imports
)

const (
	modeK8s  = "k8s"
	modeHost = "host"
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

type hostModeParameters struct {
	k8sWaitInterval      time.Duration
	hostContainerPidPath string
	hostFRRReloadSocket  string
	configuration        string
	systemdSocketPath    string
}

type k8sModeParameters struct {
	nodeName   string
	namespace  string
	reloadPort int
	criSocket  string
}

func main() {
	hostModeParams := hostModeParameters{}
	k8sModeParams := k8sModeParameters{}

	args := struct {
		metricsAddr   string
		probeAddr     string
		secureMetrics bool
		enableHTTP2   bool
		logLevel      string
		frrConfigPath string
		mode          string
	}{}

	flag.StringVar(&args.metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&args.probeAddr, "health-probe-bind-address", ":9081", "The address the probe endpoint binds to.")
	flag.BoolVar(&args.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&args.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.StringVar(&args.logLevel, "loglevel", "info", "the verbosity of the process")
	flag.StringVar(&args.frrConfigPath, "frrconfig", "/etc/perouter/frr/frr.conf",
		"the location of the frr configuration file")
	flag.StringVar(&args.mode, "mode", modeK8s, "the mode to run in (k8s or host)")

	flag.StringVar(&k8sModeParams.nodeName, "nodename", "", "The name of the node the controller runs on")
	flag.StringVar(&k8sModeParams.namespace, "namespace", "", "The namespace the controller runs in")
	flag.IntVar(&k8sModeParams.reloadPort, "reloadport", 9080, "the port of the reloader process")
	flag.StringVar(&k8sModeParams.criSocket, "crisocket", "/containerd.sock", "the location of the cri socket")

	flag.DurationVar(&hostModeParams.k8sWaitInterval, "k8s-wait-timeout", time.Minute,
		"K8s API server waiting interval time")
	flag.StringVar(&hostModeParams.hostContainerPidPath, "pid-path", "",
		"the path of the pid file of the router container")
	flag.StringVar(&hostModeParams.hostFRRReloadSocket, "frr-socket", "",
		"the path of socket to trigger frr reload in the router container")
	flag.StringVar(&hostModeParams.configuration, "host-configuration",
		"/etc/openperouter/config.yaml", "the path of host configuration")
	flag.StringVar(&hostModeParams.systemdSocketPath, "systemd-socket",
		systemdctl.HostDBusSocket, "the path of systemd control socket")

	flag.Parse()

	if err := validateParameters(args.mode, hostModeParams, k8sModeParams); err != nil {
		fmt.Printf("validation error: %v\n", err)
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

	/* TODO: to be used for the metrics endpoints while disabiling
	http2
	tlsOpts = append(tlsOpts, func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	})*/

	k8sConfig, err := waitForKubernetes(context.Background(), hostModeParams.k8sWaitInterval)
	if err != nil {
		setupLog.Error(err, "failed to connect to kubernetes api server")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(k8sConfig, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: args.probeAddr,
		Cache:                  cache.Options{},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	podRuntime, err := pods.NewRuntime(k8sModeParams.criSocket, 5*time.Minute)
	if err != nil {
		setupLog.Error(err, "connect to crio")
		os.Exit(1)
	}

	var routerProvider routerconfiguration.RouterProvider
	switch args.mode {
	case modeK8s:
		routerProvider = &routerconfiguration.RouterPodProvider{
			FRRConfigPath: args.frrConfigPath,
			FRRReloadPort: k8sModeParams.reloadPort,
			PodRuntime:    podRuntime,
			Client:        mgr.GetClient(),
			Node:          k8sModeParams.nodeName,
		}
	case modeHost:
		hostConfig, err := staticconfiguration.ReadFromFile(hostModeParams.configuration)
		if err != nil {
			setupLog.Error(err, "failed to load the static configuration file")
			os.Exit(1)
		}
		routerProvider = &routerconfiguration.RouterHostProvider{
			FRRConfigPath:     args.frrConfigPath,
			FRRReloadSocket:   hostModeParams.hostFRRReloadSocket,
			RouterPidFilePath: hostModeParams.hostContainerPidPath,
			CurrentNodeIndex:  hostConfig.NodeIndex,
			SystemdSocketPath: hostModeParams.systemdSocketPath,
		}
	}

	if err = (&routerconfiguration.PERouterReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		MyNode:         k8sModeParams.nodeName,
		LogLevel:       args.logLevel,
		Logger:         logger,
		MyNamespace:    k8sModeParams.namespace,
		FRRConfigPath:  args.frrConfigPath,
		RouterProvider: routerProvider,
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

func waitForKubernetes(ctx context.Context, waitInterval time.Duration) (*rest.Config, error) {
	attempt := 1
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context canceled")
		default:
			config, err := pingAPIServer()
			if err != nil {
				slog.Debug("ping api server failed", "error", err, "attempt", attempt)
				time.Sleep(waitInterval)
				continue
			}

			slog.Info("successfully connected to kubernetes api server", "attempts", attempt)
			return config, nil
		}
	}
}

func pingAPIServer() (*rest.Config, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get incluster config %w", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get clientset %w", err)
	}

	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get serverversion %w", err)
	}
	return cfg, nil

}

func validateParameters(mode string, hostModeParams hostModeParameters, k8sModeParams k8sModeParameters) error {
	if mode != modeK8s && mode != modeHost {
		return fmt.Errorf("invalid mode %q, must be '%s' or '%s'", mode, modeK8s, modeHost)
	}

	if mode == modeK8s {
		if hostModeParams.hostContainerPidPath != "" {
			return fmt.Errorf("pid-path should not be set in %s mode", modeK8s)
		}
		if hostModeParams.hostFRRReloadSocket != "" {
			return fmt.Errorf("frr-socket should not be set in %s mode", modeK8s)
		}
		if k8sModeParams.nodeName == "" {
			return fmt.Errorf("nodename is required in %s mode", modeK8s)
		}
		if k8sModeParams.namespace == "" {
			return fmt.Errorf("namespace is required in %s mode", modeK8s)
		}
	}

	if mode == modeHost {
		if k8sModeParams.nodeName != "" {
			return fmt.Errorf("nodename should not be set in %s mode", modeHost)
		}
		if k8sModeParams.namespace != "" {
			return fmt.Errorf("namespace should not be set in %s mode", modeHost)
		}
		if hostModeParams.hostContainerPidPath == "" {
			return fmt.Errorf("pid-path is required in %s mode", modeHost)
		}
		if hostModeParams.hostFRRReloadSocket == "" {
			return fmt.Errorf("frr-socket is required in %s mode", modeHost)
		}
	}

	return nil
}
