// SPDX-License-Identifier:Apache-2.0

// groutdra is a POC kubelet DRA plugin. See kubevirt#18444 for the
// registration / ResourceSlice / CDI pattern this copies.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"

	"github.com/openperouter/openperouter/internal/groutdra"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error("groutdra", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return errf("NODE_NAME is required")
	}
	client, err := kubeClient()
	if err != nil {
		return err
	}
	pluginDir := "/var/lib/kubelet/plugins/" + groutdra.DriverName
	if err := os.MkdirAll(pluginDir, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(groutdra.CDIDir, 0o755); err != nil {
		return err
	}

	d := groutdra.New(groutdra.GroutSockPath)
	helper, err := kubeletplugin.Start(ctx, d,
		kubeletplugin.DriverName(groutdra.DriverName),
		kubeletplugin.KubeClient(client),
		kubeletplugin.NodeName(nodeName),
		kubeletplugin.PluginDataDirectoryPath(pluginDir),
	)
	if err != nil {
		return err
	}
	defer helper.Stop()

	if err := helper.PublishResources(ctx, groutdra.PoolResources()); err != nil {
		return err
	}
	slog.Info("groutdra started", "node", nodeName, "driver", groutdra.DriverName)
	<-ctx.Done()
	return nil
}

func kubeClient() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = "/etc/kubernetes/kubelet.conf"
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(cfg)
}

type constError string

func (e constError) Error() string { return string(e) }

func errf(s string) error { return constError(s) }
