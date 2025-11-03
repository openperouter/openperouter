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
	http.HandleFunc("/", reloadHandler(args.frrConfigPath))

	server := &http.Server{
		ReadHeaderTimeout: 3 * time.Second,
	}
	if err := server.Serve(listener); err != nil {
		return err
	}
	return nil
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
