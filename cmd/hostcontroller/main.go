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
	"crypto/tls"
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

	"github.com/go-logr/logr"
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
	configuration        string
	systemdSocketPath    string
}

type k8sModeParameters struct {
	nodeName  string
	namespace string
	criSocket string
}

func main() {
	hostModeParams := hostModeParameters{}
	k8sModeParams := k8sModeParameters{}

	args := struct {
		probeAddr          string
		tlsOpts            []func(*tls.Config)
		logLevel           string
		frrConfigPath      string
		reloaderSocket     string
		mode               string
		underlayFromMultus bool
		ovsSocketPath      string
	}{}

	flag.StringVar(&args.probeAddr, "health-probe-bind-address", ":9081", "The address the probe endpoint binds to.")
	flag.StringVar(&args.logLevel, "loglevel", "info", "the verbosity of the process")
	flag.StringVar(&args.frrConfigPath, "frrconfig", "/etc/perouter/frr/frr.conf",
		"the location of the frr configuration file")
	flag.BoolVar(&args.underlayFromMultus, "underlay-from-multus", false, "Whether underlay access is built with Multus")
	flag.StringVar(&args.ovsSocketPath, "ovssocket", "unix:/var/run/openvswitch/db.sock",
		"the OVS database socket path")

	flag.StringVar(&args.mode, "mode", modeK8s, "the mode to run in (k8s or host)")

	flag.StringVar(&k8sModeParams.nodeName, "nodename", "", "The name of the node the controller runs on")
	flag.StringVar(&k8sModeParams.namespace, "namespace", "", "The namespace the controller runs in")
	flag.StringVar(&k8sModeParams.criSocket, "crisocket", "/containerd.sock", "the location of the cri socket")

	flag.DurationVar(&hostModeParams.k8sWaitInterval, "k8s-wait-timeout", time.Minute,
		"K8s API server waiting interval time")
	flag.StringVar(&hostModeParams.hostContainerPidPath, "pid-path", "",
		"the path of the pid file of the router container")
	flag.StringVar(&args.reloaderSocket, "reloader-socket", "",
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
		// Restrict client cache/informer to events for the node running this pod.
		// On large clusters, not doing so can overload the API server for daemonsets
		// since nodes receive frequent updates in some environments.
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Node{}: {
					Field:     fields.Set{"metadata.name": k8sModeParams.nodeName}.AsSelector(),
					Transform: cache.TransformStripManagedFields(),
				},
				&corev1.Pod{}: {
					Label: labels.SelectorFromSet(labels.Set{"app": "router"}),
					Field: fields.Set{
						"spec.nodeName":      k8sModeParams.nodeName,
						"metadata.namespace": k8sModeParams.namespace,
					}.AsSelector(),
				},
			},
		},
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
	var nodeName string
	switch args.mode {
	case modeK8s:
		nodeName = k8sModeParams.nodeName
		routerProvider = &routerconfiguration.RouterPodProvider{
			FRRConfigPath: args.frrConfigPath,
			PodRuntime:    podRuntime,
			Client:        mgr.GetClient(),
			Node:          nodeName,
		}
	case modeHost:
		hostConfig, err := staticconfiguration.ReadNodeConfig(hostModeParams.configuration)
		if err != nil {
			setupLog.Error(err, "failed to load the static configuration file")
			os.Exit(1)
		}
		// In host mode, the node name is passed via --nodename parameter
		nodeName = k8sModeParams.nodeName
		routerProvider = &routerconfiguration.RouterHostProvider{
			FRRConfigPath:     args.frrConfigPath,
			RouterPidFilePath: hostModeParams.hostContainerPidPath,
			CurrentNodeIndex:  hostConfig.NodeIndex,
			SystemdSocketPath: hostModeParams.systemdSocketPath,
		}
	}

	if err = (&routerconfiguration.PERouterReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		MyNode:             nodeName,
		LogLevel:           args.logLevel,
		Logger:             logger,
		MyNamespace:        k8sModeParams.namespace,
		FRRConfigPath:      args.frrConfigPath,
		FRRReloadSocket:    args.reloaderSocket,
		RouterProvider:     routerProvider,
		UnderlayFromMultus: args.underlayFromMultus,
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
		if k8sModeParams.nodeName == "" {
			return fmt.Errorf("nodename is required in %s mode", modeK8s)
		}
		if k8sModeParams.namespace == "" {
			return fmt.Errorf("namespace is required in %s mode", modeK8s)
		}
	}

	if mode == modeHost {
		if k8sModeParams.nodeName == "" {
			return fmt.Errorf("nodename is required in %s mode", modeHost)
		}
		if k8sModeParams.namespace != "" {
			return fmt.Errorf("namespace should not be set in %s mode", modeHost)
		}
		if hostModeParams.hostContainerPidPath == "" {
			return fmt.Errorf("pid-path is required in %s mode", modeHost)
		}
	}

	return nil
}
