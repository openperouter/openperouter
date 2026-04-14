// SPDX-License-Identifier:Apache-2.0

package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
)

type LeafConfig struct {
	RouterID                         string
	NeighborIP                       string
	NetworkToAdvertise               string
	RedistributeConnectedFromVRFs    bool
	RedistributeConnectedFromDefault bool
	ISISNet                          string
	UpdateSource                     string
	SRV6Prefix                       string
}

func main() {
	var (
		leafName                      = flag.String("leaf", "", "Leaf name (e.g., leafA, leafB)")
		routerID                      = flag.String("router-id", "", "Router ID")
		neighborIP                    = flag.String("neighbor", "", "Neighbor IP address")
		networkToAdvertise            = flag.String("network", "", "Network to advertise (CIDR format)")
		redistributeConnectedFromVRFs = flag.Bool("redistribute-connected-from-vrfs", false,
			"Add redistribute connected to VRF address families")
		redistributeConnectedDefault = flag.Bool("redistribute-connected-from-default", false,
			"Add redistribute connected to default address families")

		isisNet      = flag.String("isis-net", "", "ISIS net address")
		updateSource = flag.String("update-source", "", "BGP update source")
		srv6Prefix   = flag.String("srv6-prefix", "", "SRV6 locator prefix")

		outputDir    = flag.String("output", "", "Output directory (default: ../{leaf_name})")
		templateFile = flag.String("template", "frr_template/frr.conf.template", "Template file path")
	)
	flag.Parse()

	if *leafName == "" {
		fmt.Println("Usage: generate_leaf_config " +
			"-leaf <name> -neighbor <ip> -network <cidr> -router-id <router ID> [options]")
		fmt.Println("Example: generate_leaf_config " +
			"-leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32 -router-id 100.64.0.1")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *outputDir == "" {
		*outputDir = filepath.Join("..", *leafName)
	}

	tmplContent, err := os.ReadFile(*templateFile)
	if err != nil {
		log.Fatalf("Error reading template file: %v", err)
	}

	tmpl, err := template.New("frr").Parse(string(tmplContent))
	if err != nil {
		log.Fatalf("Error parsing template: %v", err)
	}

	config := LeafConfig{
		RouterID:                         *routerID,
		NeighborIP:                       *neighborIP,
		NetworkToAdvertise:               *networkToAdvertise,
		RedistributeConnectedFromVRFs:    *redistributeConnectedFromVRFs,
		RedistributeConnectedFromDefault: *redistributeConnectedDefault,

		ISISNet:      *isisNet,
		UpdateSource: *updateSource,
		SRV6Prefix:   *srv6Prefix,
	}

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	outputFile := filepath.Join(*outputDir, "frr.conf")
	file, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("Error creating output file: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Fatalf("Error closing output file: %v", err)
		}
	}()

	if err := tmpl.Execute(file, config); err != nil {
		log.Fatalf("Error executing template: %v", err)
	}
}
