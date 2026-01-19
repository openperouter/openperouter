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

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/go-logr/logr"
	"github.com/openperouter/openperouter/api/static"
	periov1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/controller/routerconfiguration"
	"github.com/openperouter/openperouter/internal/hostnetwork"
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
	configurationDir     string
	nodeConfigPath       string
	systemdSocketPath    string
}

type k8sModeParameters struct {
	namespace string
	criSocket string
}

type parameters struct {
	probeAddr          string
	frrConfigPath      string
	reloaderSocket     string
	mode               string
	underlayFromMultus bool
	ovsSocketPath      string
	nodeName           string
	logLevel           string
}

func main() {
	hostModeParams := hostModeParameters{}
	k8sModeParams := k8sModeParameters{}

	args := parameters{}

	flag.StringVar(&args.probeAddr, "health-probe-bind-address", ":9081", "The address the probe endpoint binds to.")
	flag.StringVar(&args.logLevel, "loglevel", "info", "the verbosity of the process")
	flag.StringVar(&args.frrConfigPath, "frrconfig", "/etc/perouter/frr/frr.conf",
		"the location of the frr configuration file")
	flag.BoolVar(&args.underlayFromMultus, "underlay-from-multus", false, "Whether underlay access is built with Multus")
	flag.StringVar(&args.ovsSocketPath, "ovssocket", "unix:/var/run/openvswitch/db.sock",
		"the OVS database socket path")

	flag.StringVar(&args.mode, "mode", modeK8s, "the mode to run in (k8s or host)")

	flag.StringVar(&args.nodeName, "nodename", "", "The name of the node the controller runs on")
	flag.StringVar(&k8sModeParams.namespace, "namespace", "", "The namespace the controller runs in")
	flag.StringVar(&k8sModeParams.criSocket, "crisocket", "/containerd.sock", "the location of the cri socket")

	flag.DurationVar(&hostModeParams.k8sWaitInterval, "k8s-wait-timeout", time.Minute,
		"K8s API server waiting interval time")
	flag.StringVar(&hostModeParams.hostContainerPidPath, "pid-path", "",
		"the path of the pid file of the router container")
	flag.StringVar(&args.reloaderSocket, "reloader-socket", "",
		"the path of socket to trigger frr reload in the router container")
	flag.StringVar(&hostModeParams.configurationDir, "host-configuration-dir",
		"/etc/openperouter/configs", "the directory containing static router configuration files (openpe_*.yaml)")
	flag.StringVar(&hostModeParams.nodeConfigPath, "node-config",
		"/etc/openperouter/node-config.yaml", "the path to node configuration file")
	flag.StringVar(&hostModeParams.systemdSocketPath, "systemd-socket",
		systemdctl.HostDBusSocket, "the path of systemd control socket")

	flag.Parse()

	// Initialize OVS socket path for the hostnetwork package
	hostnetwork.OVSSocketPath = args.ovsSocketPath

	var nodeConfig = &static.NodeConfig{}
	if args.mode == modeHost {
		var err error
		nodeConfig, err = staticconfiguration.ReadNodeConfig(hostModeParams.nodeConfigPath)
		if err != nil {
			setupLog.Error(err, "failed to load the node configuration file")
			os.Exit(1)
		}
		if err := overrideHostMode(&args, *nodeConfig); err != nil {
			setupLog.Error(err, "failed to override host mode arguments")
			os.Exit(1)
		}
	}

	if err := validateParameters(args, hostModeParams, k8sModeParams); err != nil {
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

	// Setup signal handler once for the entire process
	ctx := ctrl.SetupSignalHandler()

	if args.mode == modeK8s {
		// K8s mode: setup k8s-based reconciler and start
		k8sConfig, err := config.GetConfig()
		if err != nil {
			setupLog.Error(err, "unable to get kubernetes config")
			os.Exit(1)
		}
		// runK8sConfigReconciler is blocking so when running in k8s mode we should stop here
		if err := runK8sConfigReconciler(
			ctx, args, hostModeParams, k8sModeParams, nodeConfig, k8sConfig, logger, args.probeAddr,
		); err != nil {
			setupLog.Error(err, "failed to enable k8s reconciler")
			os.Exit(1)
		}
		return
	}

	// host mode: run the host reconciler and keep polling until the k8s api is available.

	// Wait for K8s API and create second manager when available
	go func() {
		setupLog.Info("waiting for kubernetes API")
		k8sConfig, err := waitForKubernetes(context.Background(), hostModeParams.k8sWaitInterval)
		if err != nil {
			setupLog.Error(err, "failed to connect to kubernetes API, will continue with static config only")
			return
		}

		setupLog.Info("kubernetes API is now available, creating second manager for API-based controller")

		if err := runK8sConfigReconciler(
			ctx, args, hostModeParams, k8sModeParams, nodeConfig, k8sConfig, logger, "",
		); err != nil {
			setupLog.Error(err, "failed to enable k8s reconciler")
			return
		}
	}()

	// Host mode: create static manager first, then wait for k8s in background
	setupLog.Info("creating static-only manager for host mode")
	if err := runStaticConfigReconciler(ctx, args, hostModeParams, nodeConfig, logger, args.probeAddr); err != nil {
		setupLog.Error(err, "failed to run static config reconciler")
		os.Exit(1)
	}
}

func waitForKubernetes(ctx context.Context, waitInterval time.Duration) (*rest.Config, error) {
	var config *rest.Config
	err := wait.PollUntilContextCancel(ctx, waitInterval, true, func(ctx context.Context) (bool, error) {
		cfg, err := pingAPIServer()
		if err != nil {
			slog.Debug("ping api server failed", "error", err)
			return false, nil // Keep retrying
		}
		config = cfg
		slog.Info("successfully connected to kubernetes api server")
		return true, nil // Success
	})
	if err != nil {
		return nil, err
	}
	return config, nil
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

	if _, err := clientset.Discovery().ServerVersion(); err != nil {
		return nil, fmt.Errorf("failed to get serverversion %w", err)
	}
	return cfg, nil
}

func validateParameters(args parameters, hostModeParams hostModeParameters, k8sModeParams k8sModeParameters) error {
	if args.mode != modeK8s && args.mode != modeHost {
		return fmt.Errorf("invalid mode %q, must be '%s' or '%s'", args.mode, modeK8s, modeHost)
	}

	if args.nodeName == "" {
		return fmt.Errorf("nodename is required")
	}

	if args.mode == modeK8s {
		if hostModeParams.hostContainerPidPath != "" {
			return fmt.Errorf("pid-path should not be set in %s mode", modeK8s)
		}
		if k8sModeParams.namespace == "" {
			return fmt.Errorf("namespace is required in %s mode", modeK8s)
		}
		return nil
	}

	// host mode
	if k8sModeParams.namespace != "" {
		return fmt.Errorf("namespace should not be set in %s mode", modeHost)
	}
	if hostModeParams.hostContainerPidPath == "" {
		return fmt.Errorf("pid-path is required in %s mode", modeHost)
	}

	return nil
}

// overrideHostMode overrides the values provided by the cli args
// with those provided from the configuration files. This allows do
// have an uniform deployment while being able to provide different
// knobs for different nodes.
func overrideHostMode(args *parameters, nodeConfig static.NodeConfig) error {
	if nodeConfig.LogLevel != "" {
		setupLog.Info("overriding log level from static configuration", "loglevel", nodeConfig.LogLevel)
		args.logLevel = nodeConfig.LogLevel
	}
	if nodeConfig.NodeName != "" {
		setupLog.Info("overriding node name from static configuration", "nodename", nodeConfig.NodeName)
		args.nodeName = nodeConfig.NodeName
		return nil
	}

	var err error
	args.nodeName, err = os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %w", err)
	}
	setupLog.Info("nodename not provided, using hostname", "nodename", args.nodeName)
	return nil
}

func runK8sConfigReconciler(ctx context.Context,
	args parameters,
	hostModeParams hostModeParameters,
	k8sModeParams k8sModeParameters,
	nodeConfig *static.NodeConfig,
	k8sConfig *rest.Config,
	logger *slog.Logger,
	probeAddr string) error {

	mgr, err := ctrl.NewManager(k8sConfig, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		// Restrict client cache/informer to events for the node running this pod.
		// On large clusters, not doing so can overload the API server for daemonsets
		// since nodes receive frequent updates in some environments.
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Node{}: {
					Field:     fields.Set{"metadata.name": args.nodeName}.AsSelector(),
					Transform: cache.TransformStripManagedFields(),
				},
				&corev1.Pod{}: {
					Label: labels.SelectorFromSet(labels.Set{"app": "router"}),
					Field: fields.Set{
						"spec.nodeName":      args.nodeName,
						"metadata.namespace": k8sModeParams.namespace,
					}.AsSelector(),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	var routerProvider routerconfiguration.RouterProvider
	routerProvider = &routerconfiguration.RouterHostProvider{
		FRRConfigPath:     args.frrConfigPath,
		RouterPidFilePath: hostModeParams.hostContainerPidPath,
		CurrentNodeIndex:  nodeConfig.NodeIndex,
		SystemdSocketPath: hostModeParams.systemdSocketPath,
	}
	if args.mode == modeK8s {
		podRuntime, err := pods.NewRuntime(k8sModeParams.criSocket, 5*time.Minute)
		if err != nil {
			setupLog.Error(err, "connect to crio")
			os.Exit(1)
		}
		routerProvider = &routerconfiguration.RouterPodProvider{
			FRRConfigPath: args.frrConfigPath,
			PodRuntime:    podRuntime,
			Client:        mgr.GetClient(),
			Node:          args.nodeName,
		}
	}

	apiReconciler := &routerconfiguration.PERouterReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		LogLevel:        args.logLevel,
		Logger:          logger,
		MyNode:          args.nodeName,
		FRRReloadSocket: args.reloaderSocket,
		FRRConfigPath:   args.frrConfigPath,
		RouterProvider:  routerProvider,
	}
	if args.mode == modeHost {
		apiReconciler.StaticConfigDir = hostModeParams.configurationDir
		apiReconciler.NodeConfigPath = hostModeParams.nodeConfigPath
	}
	if args.mode == modeK8s {
		apiReconciler.MyNamespace = k8sModeParams.namespace
		apiReconciler.UnderlayFromMultus = args.underlayFromMultus
	}

	if err := apiReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}
	return nil
}

func runStaticConfigReconciler(ctx context.Context,
	args parameters,
	hostModeParams hostModeParameters,
	nodeConfig *static.NodeConfig,
	logger *slog.Logger,
	probeAddr string) error {
	mgr, err := ctrl.NewManager(&rest.Config{}, ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
		Metrics: server.Options{
			BindAddress: "0", // disable metrics
		},
	})
	if err != nil {
		return fmt.Errorf("unable to start static manager: %w", err)
	}

	staticRouterProvider := &routerconfiguration.RouterHostProvider{
		FRRConfigPath:     args.frrConfigPath,
		RouterPidFilePath: hostModeParams.hostContainerPidPath,
		CurrentNodeIndex:  nodeConfig.NodeIndex,
		SystemdSocketPath: hostModeParams.systemdSocketPath,
	}

	staticReconciler := &routerconfiguration.StaticConfigReconciler{
		Scheme:          mgr.GetScheme(),
		Logger:          logger,
		NodeIndex:       nodeConfig.NodeIndex,
		LogLevel:        args.logLevel,
		FRRConfigPath:   args.frrConfigPath,
		FRRReloadSocket: args.reloaderSocket,
		RouterProvider:  staticRouterProvider,
		ConfigDir:       hostModeParams.configurationDir,
	}
	if err = staticReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}
	return nil
}
