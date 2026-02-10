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
	"errors"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/go-logr/logr"
	"github.com/openperouter/openperouter/api/static"
	periov1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/controller/routerconfiguration"
	"github.com/openperouter/openperouter/internal/filewatcher"
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
	k8sWaitInterval       time.Duration
	hostContainerPidPath  string
	configurationDir      string
	nodeConfigPath        string
	systemdSocketPath     string
	routerHealthCheckPort int
}

type k8sModeParameters struct {
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
	namespace          string
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
	flag.StringVar(&args.namespace, "namespace", "", "The namespace the controller runs in")
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
	flag.IntVar(&hostModeParams.routerHealthCheckPort, "router-health-check-port",
		9080, "the port for router health check endpoint")

	flag.Parse()

	// Initialize OVS socket path for the hostnetwork package
	hostnetwork.OVSSocketPath = args.ovsSocketPath

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

	if err := validateParameters(args, hostModeParams); err != nil {
		fmt.Printf("validation error: %v\n", err)
		os.Exit(1)
	}

	if args.mode == modeK8s {
		runK8sMode(ctx, args, k8sModeParams, logger)
		return
	}

	runHostMode(ctx, args, hostModeParams, logger)
}

func runK8sMode(
	ctx context.Context,
	args parameters,
	k8sModeParams k8sModeParameters,
	logger *slog.Logger,
) {
	// K8s mode: setup k8s-based reconciler and start
	k8sConfig, err := config.GetConfig()
	if err != nil {
		logger.Error("unable to get kubernetes config", "error", err)
		os.Exit(1)
	}
	// runK8sConfigReconciler is blocking so when running in k8s mode we should stop here
	if err := runK8sConfigReconciler(
		ctx, args, k8sModeParams, k8sConfig, logger, args.probeAddr,
	); err != nil {
		logger.Error("failed to enable k8s reconciler", "error", err)
		os.Exit(1)
	}
}

func runHostMode(
	ctx context.Context,
	args parameters,
	hostModeParams hostModeParameters,
	logger *slog.Logger,
) {
	// host mode: run the host reconciler and keep polling until the k8s api is available.
	nodeConfig, err := staticconfiguration.ReadNodeConfig(hostModeParams.nodeConfigPath)
	if err != nil {
		logger.Error("failed to load the node configuration file", "error", err)
		os.Exit(1)
	}
	if err := overrideHostMode(&args, *nodeConfig); err != nil {
		logger.Error("failed to override host mode arguments", "error", err)
		os.Exit(1)
	}

	staticControllerCtx, stopStaticReconciler := context.WithCancel(ctx)
	defer stopStaticReconciler()

	go func() {
		logger.Info("creating static configuration controller for host mode")
		err := runStaticConfigReconciler(
			staticControllerCtx, args, hostModeParams, nodeConfig, logger, args.probeAddr,
		)
		if errors.Is(err, context.Canceled) {
			logger.Info("static config reconciler stopped (API became available)")
			return
		}
		if err != nil {
			logger.Error("failed to run static config reconciler", "error", err)
		}
	}()

	// Wait for K8s API in main thread (blocking)
	logger.Info("waiting for kubernetes API")
	k8sConfig, err := waitForKubernetes(ctx, hostModeParams.k8sWaitInterval)
	if err != nil {
		logger.Error("failed to connect to kubernetes API, will continue with static config only", "error", err)
		<-ctx.Done()
		return
	}

	logger.Info("kubernetes API is now available, stopping static reconciler and starting k8s reconciler")

	stopStaticReconciler()

	// Start API reconciler in main thread (blocking) - keeps process alive
	if err := runK8sConfigReconcilerHostMode(
		ctx, args, hostModeParams, nodeConfig, k8sConfig, logger,
	); err != nil {
		logger.Error("failed to enable k8s reconciler", "error", err)
		os.Exit(1)
	}
}

func runK8sConfigReconcilerHostMode(ctx context.Context,
	args parameters,
	hostModeParams hostModeParameters,
	nodeConfig *static.NodeConfig,
	k8sConfig *rest.Config,
	logger *slog.Logger) error {

	mgr, err := createK8sManager(k8sConfig, args.nodeName, args.namespace)
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	routerProvider := &routerconfiguration.RouterHostProvider{
		FRRConfigPath:         args.frrConfigPath,
		RouterPidFilePath:     hostModeParams.hostContainerPidPath,
		CurrentNodeIndex:      nodeConfig.NodeIndex,
		SystemdSocketPath:     hostModeParams.systemdSocketPath,
		RouterHealthCheckPort: hostModeParams.routerHealthCheckPort,
	}

	// Create trigger channel for file watcher
	triggerChan := make(chan event.GenericEvent, 1)

	apiReconciler := &routerconfiguration.PERouterReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		LogLevel:        args.logLevel,
		Logger:          logger,
		MyNode:          args.nodeName,
		FRRReloadSocket: args.reloaderSocket,
		FRRConfigPath:   args.frrConfigPath,
		RouterProvider:  routerProvider,
		StaticConfigDir: hostModeParams.configurationDir,
		NodeConfigPath:  hostModeParams.nodeConfigPath,
		TriggerChan:     triggerChan,
	}

	if err := apiReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	// Setup file watcher to trigger API reconciler on static file changes
	fw, err := filewatcher.New(hostModeParams.configurationDir, triggerChan, logger)
	if err != nil {
		return fmt.Errorf("unable to create file watcher for API reconciler: %w", err)
	}

	if err := fw.Start(ctx); err != nil {
		return fmt.Errorf("unable to start file watcher for API reconciler: %w", err)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}
	return nil
}

func runK8sConfigReconciler(ctx context.Context,
	args parameters,
	k8sModeParams k8sModeParameters,
	k8sConfig *rest.Config,
	logger *slog.Logger,
	probeAddr string) error {

	mgr, err := createK8sManager(k8sConfig, args.nodeName, args.namespace, func(opts *ctrl.Options) {
		opts.HealthProbeBindAddress = probeAddr
	})
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	podRuntime, err := pods.NewRuntime(k8sModeParams.criSocket, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("failed to connect to crio: %w", err)
	}
	routerProvider := &routerconfiguration.RouterPodProvider{
		FRRConfigPath: args.frrConfigPath,
		PodRuntime:    podRuntime,
		Client:        mgr.GetClient(),
		Node:          args.nodeName,
	}

	apiReconciler := &routerconfiguration.PERouterReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		LogLevel:           args.logLevel,
		Logger:             logger,
		MyNode:             args.nodeName,
		FRRReloadSocket:    args.reloaderSocket,
		FRRConfigPath:      args.frrConfigPath,
		RouterProvider:     routerProvider,
		MyNamespace:        args.namespace,
		UnderlayFromMultus: args.underlayFromMultus,
	}

	if err := apiReconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
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
		FRRConfigPath:         args.frrConfigPath,
		RouterPidFilePath:     hostModeParams.hostContainerPidPath,
		CurrentNodeIndex:      nodeConfig.NodeIndex,
		SystemdSocketPath:     hostModeParams.systemdSocketPath,
		RouterHealthCheckPort: hostModeParams.routerHealthCheckPort,
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

	// Setup file watcher for static configuration changes
	fw, err := filewatcher.New(hostModeParams.configurationDir, staticReconciler.TriggerChan, logger)
	if err != nil {
		return fmt.Errorf("unable to create file watcher: %w", err)
	}

	if err := fw.Start(ctx); err != nil {
		return fmt.Errorf("unable to start file watcher: %w", err)
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

func createK8sManager(
	k8sConfig *rest.Config,
	nodeName string,
	namespace string,
	modifiers ...func(*ctrl.Options),
) (ctrl.Manager, error) {
	opts := ctrl.Options{
		Scheme: scheme,
		// Restrict client cache/informer to events for the node running this pod.
		// On large clusters, not doing so can overload the API server for daemonsets
		// since nodes receive frequent updates in some environments.
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Node{}: {
					Field:     fields.Set{"metadata.name": nodeName}.AsSelector(),
					Transform: cache.TransformStripManagedFields(),
				},
				&corev1.Pod{}: {
					Label: labels.SelectorFromSet(labels.Set{"app": "router"}),
					Field: fields.Set{
						"spec.nodeName":      nodeName,
						"metadata.namespace": namespace,
					}.AsSelector(),
				},
			},
		},
	}

	for _, modifier := range modifiers {
		modifier(&opts)
	}

	res, err := ctrl.NewManager(k8sConfig, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create new manager: %w", err)
	}
	if err := res.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := res.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("unable to set up ready check: %w", err)
	}
	return res, nil
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

func validateParameters(args parameters, hostModeParams hostModeParameters) error {
	if args.mode != modeK8s && args.mode != modeHost {
		return fmt.Errorf("invalid mode %q, must be '%s' or '%s'", args.mode, modeK8s, modeHost)
	}
	if args.namespace == "" {
		return fmt.Errorf("namespace is required")
	}

	if args.mode == modeK8s {
		if hostModeParams.hostContainerPidPath != "" {
			return fmt.Errorf("pid-path should not be set in %s mode", modeK8s)
		}
		if args.nodeName == "" {
			return fmt.Errorf("nodename is required")
		}
	}

	if args.mode == modeHost {
		if hostModeParams.hostContainerPidPath == "" {
			return fmt.Errorf("pid-path is required in %s mode", modeHost)
		}
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
