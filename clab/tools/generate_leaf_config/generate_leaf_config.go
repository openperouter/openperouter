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
	NeighborIP                       string
	NetworkToAdvertise               string
	RedistributeConnectedFromVRFs    bool
	RedistributeConnectedFromDefault bool
}

func main() {
	var (
		leafName                      = flag.String("leaf", "", "Leaf name (e.g., leafA, leafB)")
		neighborIP                    = flag.String("neighbor", "", "Neighbor IP address")
		networkToAdvertise            = flag.String("network", "", "Network to advertise (CIDR format)")
		redistributeConnectedFromVRFs = flag.Bool("redistribute-connected-from-vrfs", false,
			"Add redistribute connected to VRF address families")
		redistributeConnectedDefault = flag.Bool("redistribute-connected-from-default", false,
			"Add redistribute connected to default address families")
		outputDir    = flag.String("output", "", "Output directory (default: ../{leaf_name})")
		templateFile = flag.String("template", "frr_template/frr.conf.template", "Template file path")
	)
	flag.Parse()

	if *leafName == "" || *neighborIP == "" || *networkToAdvertise == "" {
		fmt.Println("Usage: generate_leaf_config -leaf <name> -neighbor <ip> -network <cidr> [options]")
		fmt.Println("Example: generate_leaf_config -leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32")
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
		NeighborIP:                       *neighborIP,
		NetworkToAdvertise:               *networkToAdvertise,
		RedistributeConnectedFromVRFs:    *redistributeConnectedFromVRFs,
		RedistributeConnectedFromDefault: *redistributeConnectedDefault,
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
