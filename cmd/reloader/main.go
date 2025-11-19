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

	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/openperouter/openperouter/internal/frrconfig"
	"github.com/openperouter/openperouter/internal/logging"
)

type Args struct {
	bindAddress   string
	unixSocket    string
	logLevel      string
	frrConfigPath string
}

func main() {
	args := Args{}
	flag.StringVar(&args.bindAddress, "bindaddress", "", "The address the reloader endpoint binds to. ")
	flag.StringVar(&args.unixSocket, "unixsocket", "", "Unix socket path to listen on (overrides bindaddress if set)")
	flag.StringVar(&args.logLevel, "loglevel", "info", "The log level of the process")
	flag.StringVar(&args.frrConfigPath, "frrconfig", "/etc/frr/frr.conf", "The path the frr configuration is at")
	flag.Parse()

	_, err := logging.New(args.logLevel)
	if err != nil {
		fmt.Println("failed to init logger", err)
	}

	build, _ := debug.ReadBuildInfo()
	slog.Info("version", "version", build.Main.Version)
	slog.Info("arguments", "args", fmt.Sprintf("%+v", args))

	http.HandleFunc("/", reloadHandler(args.frrConfigPath))

	listener, err := listenerFor(args)
	if err != nil {
		slog.Error("failed to create listener, exiting", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		ReadHeaderTimeout: 3 * time.Second,
	}
	log.Fatal(server.Serve(listener))
}

func listenerFor(args Args) (net.Listener, error) {
	if args.unixSocket != "" && args.bindAddress != "" {
		return nil, fmt.Errorf("unix socket and bind address are mutually exclusive, can't have both")
	}
	if args.unixSocket != "" {
		if err := os.Remove(args.unixSocket); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to remove unix socket: %w", err)
		}

		listener, err := net.Listen("unix", args.unixSocket)
		if err != nil {
			return nil, fmt.Errorf("failed to listen on unix socket %s: %w", args.unixSocket, err)
		}
		return listener, nil
	}

	listener, err := net.Listen("tcp", args.bindAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on address %s: %w", args.bindAddress, err)
	}
	return listener, nil
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
	}
}
