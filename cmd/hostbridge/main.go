// SPDX-License-Identifier:Apache-2.0

package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/openperouter/openperouter/internal/hostcredentials"
)

type Config struct {
	OutputPath     string
	K8sPort        int
	APIServer      string
	HostConfigPath string
	NodeName       string
}

func main() {
	var (
		outputPath     = flag.String("output-path", "/shared", "Path to write credentials")
		k8sPort        = flag.Int("k8s-port", 443, "Kubernetes API server port")
		apiServer      = flag.String("api-server", "", "Kubernetes API server address (if empty, will be resolved)")
		hostConfigPath = flag.String("config-path", "/etc/openperouter/node-config.yaml", "Path to static configuration file")
	)
	flag.Parse()

	nodeName := os.Getenv("NODE_NAME")

	config := Config{
		OutputPath:     *outputPath,
		K8sPort:        *k8sPort,
		APIServer:      *apiServer,
		HostConfigPath: *hostConfigPath,
		NodeName:       nodeName,
	}

	slog.Info("Starting hostbridge with configuration", "config", config)

	ctx := context.Background()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := annotateCurrentNode(ctx, config.HostConfigPath, config.NodeName); err != nil {
		slog.Error("Failed to add node index annotation",
			"error", err, "config_path", config.HostConfigPath, "node", config.NodeName)
		os.Exit(1)
	}

	slog.Info("Successfully added node index annotation", "config_path", config.HostConfigPath, "node", config.NodeName)

	apiServerURL, err := getAPIServer(config)
	if err != nil {
		slog.Error("failed to get api server url", "error", err)
		os.Exit(1)
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			credentials, err := hostcredentials.ReadCredentials(hostcredentials.ServiceAccountDir)
			if err != nil {
				slog.Error("Failed to read credentials", "error", err)
				continue
			}

			if err := hostcredentials.ExportCredentials(credentials, apiServerURL, config.OutputPath); err != nil {
				slog.Error("Failed to export credentials", "error", err)
				continue
			}
		case sig := <-sigChan:
			slog.Info("Received signal, shutting down", "signal", sig)
			return
		}
	}
}

func getAPIServer(config Config) (string, error) {
	if config.APIServer != "" {
		res := "https://" + net.JoinHostPort(config.APIServer, strconv.Itoa(config.K8sPort))
		return res, nil
	}
	res, err := hostcredentials.ApiServerAddress(config.K8sPort)
	if err != nil {
		return "", err
	}

	return res, nil
}
