// SPDX-License-Identifier:Apache-2.0

//go:build ignore

package main

import (
	"flag"
	"fmt"
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
		fmt.Println("Usage: generate_leaf -leaf <name> -neighbor <ip> -network <cidr> [options]")
		fmt.Println("Example: generate_leaf -leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *outputDir == "" {
		*outputDir = filepath.Join("..", *leafName)
	}

	config := LeafConfig{
		NeighborIP:                       *neighborIP,
		NetworkToAdvertise:               *networkToAdvertise,
		RedistributeConnectedFromVRFs:    *redistributeConnectedFromVRFs,
		RedistributeConnectedFromDefault: *redistributeConnectedDefault,
	}

	if err := GenerateFromTemplate(*templateFile, *outputDir, config); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
