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

	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/openperouter/openperouter/internal/frrconfig"
)

func main() {
	var bindAddress string
	flag.StringVar(&bindAddress, "localhost:8080", "8080", "The address the reloader endpoint binds to. ")
	flag.Parse()

	http.HandleFunc("/", reloadHandler)
	log.Fatal(http.ListenAndServe(bindAddress, nil))
}

var updateConfig = frrconfig.Update

// TODO serialize: multiple posts can come together
func reloadHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "invalid method", http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(req.Body)
	defer req.Body.Close()
	var event frrconfig.Event
	err := decoder.Decode(&event)
	if err != nil {
		http.Error(w, "invalid reload event", http.StatusBadRequest)
	}
	err = updateConfig(event)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
