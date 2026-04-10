// SPDX-License-Identifier:Apache-2.0

package main

// check_veths recreates veths if necessary. It monitors network link events in
// real-time by subscribing to netlink updates, reacting immediately to link
// changes.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.yaml.in/yaml/v2"
)

var fileName = flag.String("f", "-", "Configuration file (use '-' for STDIN)")

// configuration holds the list of veth pairs to monitor and maintain.
type configuration struct {
	Interfaces []vethPair
}

// printHelp displays usage information and examples.
func printHelp() {
	fmt.Println(`Usage: check_veths <name of configuration file>

Create and monitor veth pairs, attach them to containers or bridges and assign IP addresses whenever they are deleted.

IMPORTANT: For each veth pair, the left veth is the interface that is monitored and it must be attached to either:
  - bridge "leafkind-switch", OR
  - container "clab-kind-leafkind"
IMPORTANT: All links that shall be monitored must exist prior to starting this script.

  YAML Format for configuration file:

    interfaces:
    - left: <veth>
      right: <veth>
    - left: <veth>
      right: <veth>
  ...

  Each veth is specified as a YAML object with the following fields:
    - name      (string, required): interface name
    - container (string, required*): container name to attach to
    - bridge    (string, required*): bridge name to attach to
    - ips       (array, optional):  IP addresses to assign (e.g., ["192.168.1.1/24", "2001:db8::1/64"])

  * Either "container" OR "bridge" must be set (but not both).
    A veth attached to a bridge cannot have IP addresses assigned.

Examples:

  # Veth pair: bridge-attached to container-attached
  interfaces:
  - left:
      bridge: "leafkind-switch"
      name: "kindctrlpl"
    right:
      container: "pe-kind-control-plane"
      name: "toswitch"
      ips: ["192.168.11.3/24", "2001:db8:11::3/64"]


  # Veth pair: container-attached to container-attached
  interfaces:
  - left:
      container: "clab-kind-leafkind"
      name: "tokindworker"
    right:
      container: "pe-kind-worker"
      name: "toleafkind"

Environment Variables:
  CONTAINER_ENGINE_CLI: Container engine to use (default: "docker")

Parameters:
  -f <configuration file> (use '-' for STDIN)
  `)
}

func main() {
	flag.Usage = printHelp
	flag.Parse()

	var err error
	file := os.Stdin
	if *fileName != "-" {
		file, err = os.Open(*fileName)
		if err != nil {
			log.Fatalf("cannot open file %q for reading, err: %v", *fileName, err)
		}
	}
	input, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("cannot read from input source, err: %v", err)
	}

	config := &configuration{}
	if err := yaml.Unmarshal(input, config); err != nil {
		log.Fatalf("cannot parse configuration from input source, err: %v", err)
	}

	for _, pair := range config.Interfaces {
		if err := verifyVethPair(pair); err != nil {
			log.Fatal(err.Error())
		}
		if err := prepareVethPair(pair); err != nil {
			log.Fatal(err.Error())
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Order vethPairs by namespace.
	pairsForContainer := make(map[string][]vethPair)
	for _, pair := range config.Interfaces {
		pairsForContainer[pair.Left.Container] = append(pairsForContainer[pair.Left.Container], pair)
	}

	// Monitor links in container namespaces.
	for container, pairs := range pairsForContainer {
		if container == "" {
			log.Print("Spawning link monitor in host namespace")
		} else {
			log.Printf("Spawning link monitor in namespace of container %s", container)
		}
		if err := reconcile(ctx, container, pairs); err != nil {
			log.Fatal(err)
		}
	}
	<-ctx.Done()
}
