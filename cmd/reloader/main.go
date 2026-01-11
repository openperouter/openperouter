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
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/openperouter/openperouter/internal/frr/liveness"
	"github.com/openperouter/openperouter/internal/frr/vtysh"
	"github.com/openperouter/openperouter/internal/frrconfig"
	"github.com/openperouter/openperouter/internal/logging"
)

type Args struct {
	bindAddress   string
	frrConfigPath string
	logLevel      string
	unixSocket    string
}

func main() {
	args := Args{}
	flag.StringVar(&args.bindAddress, "bindaddress", "0.0.0.0:9080", "The address the reloader endpoint binds to. ")
	flag.StringVar(&args.unixSocket, "unixsocket", "", "Unix socket path to listen on")
	flag.StringVar(&args.logLevel, "loglevel", "info", "The log level of the process")
	flag.StringVar(&args.frrConfigPath, "frrconfig", "/etc/frr/frr.conf", "The path the frr configuration is at")
	flag.Parse()

	_, err := logging.New(args.logLevel)
	if err != nil {
		fmt.Println("failed to init logger", err)
	}

	if args.unixSocket == "" {
		fmt.Println("error: unixsocket parameter is required")
		os.Exit(1)
	}

	build, _ := debug.ReadBuildInfo()
	slog.Info("version", "version", build.Main.Version)
	slog.Info("arguments", "args", fmt.Sprintf("%+v", args))
	slog.Info("listening", "address", args.bindAddress)

	if err := serveReload(args); err != nil {
		log.Fatal(err)
	}
}

func serveReload(args Args) error {
	if err := os.Remove(args.unixSocket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unix socket: %w", err)
	}

	listener, err := net.Listen("unix", args.unixSocket)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket %s: %w", args.unixSocket, err)
	}

	unixServer := newServer(
		[]handlerConfig{{pattern: "/", handler: reloadHandler(args.frrConfigPath)}},
	)

	healthServer := newServer(
		[]handlerConfig{
			{pattern: "/healthz", handler: health()},
			{pattern: "/readyz", handler: health()},
		},
		withEndpoint(args.bindAddress),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("starting reloader server", "socket", args.unixSocket)
		if err := unixServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	go func() {
		slog.Info("starting health server", "address", args.bindAddress)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("received signal, shutting down gracefully")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := unixServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("error during unix server shutdown", "error", err)
		}
		if err := healthServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("error during health server shutdown", "error", err)
		}

		if err := os.Remove(args.unixSocket); err != nil {
			slog.Error("failed to remove unix socket", "socket", args.unixSocket, "error", err)
			return err
		}
		slog.Info("removed unix socket", "socket", args.unixSocket)
		return nil
	case err := <-serverErr:
		return err
	}
}

var updateConfig = frrconfig.Update

func reloadHandler(frrConfigPath string) func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "invalid method", http.StatusBadRequest)
			return
		}
		slog.Info("reload handler", "event", "received request")
		err := updateConfig(frrConfigPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		slog.Info("reload handler", "event", "reload successful")
	}
}

func health() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			http.Error(w, "invalid method", http.StatusMethodNotAllowed)
			return
		}

		if err := liveness.PingFrr(vtysh.Run); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("ok"))
		if err != nil {
			slog.Info("health check write failed", "error", err)
		}
	}
}

type handlerConfig struct {
	pattern string
	handler func(http.ResponseWriter, *http.Request)
}

type serverOption func(*http.Server)

func newServer(handlers []handlerConfig, opts ...serverOption) *http.Server {
	const defaultTimeout = 1 * time.Second

	mux := http.NewServeMux()
	for _, h := range handlers {
		mux.HandleFunc(h.pattern, h.handler)
	}

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: defaultTimeout,
	}

	for _, opt := range opts {
		opt(server)
	}
	return server
}

func withEndpoint(addr string) serverOption {
	return func(server *http.Server) {
		server.Addr = addr
	}
}
