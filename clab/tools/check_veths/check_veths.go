// SPDX-License-Identifier:Apache-2.0

package main

// check_veths recreates veths if necessary. It monitors network link events in
// real-time by subscribing to netlink updates, reacting immediately to link
// changes.

import (
	"context"
	"flag"
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
