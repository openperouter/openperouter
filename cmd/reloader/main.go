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
	"net/http"
	"time"

	"github.com/openperouter/openperouter/internal/frrconfig"
	"github.com/openperouter/openperouter/internal/logging"
)

var frrConfigPath string

func main() {
	var bindAddress string
	var logLevel string
	flag.StringVar(&bindAddress, "bindaddress", "0.0.0.0:9080", "The address the reloader endpoint binds to. ")
	flag.StringVar(&frrConfigPath, "frrconfig", "/etc/frr/frr.conf", "The path the frr configuration is at")
	flag.StringVar(&logLevel, "loglevel", "info", "The log level of the process")
	flag.Parse()

	_, err := logging.New(logLevel)
	if err != nil {
		fmt.Println("failed to init logger", err)
	}
	slog.Info("listening", "address", bindAddress)
	http.HandleFunc("/", reloadHandler)
	server := &http.Server{
		Addr:              bindAddress,
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Fatal(server.ListenAndServe())
}

var updateConfig = frrconfig.Update

func reloadHandler(w http.ResponseWriter, req *http.Request) {
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
