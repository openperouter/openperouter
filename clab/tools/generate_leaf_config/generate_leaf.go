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

const (
	defaultTemplate = "generate_leaf_config/frr_template/frr.conf.template"
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

func showHelp() {
	fmt.Println("Usage: generate_leaf " +
		"-leaf <name> -neighbor <ip> -network <cidr> -router-id <router ID> [options]")
	fmt.Println("Example: generate_leaf " +
		"-leaf leafA -neighbor 192.168.1.0 -network 100.64.0.1/32 -router-id 100.64.0.1")
	flag.PrintDefaults()
	os.Exit(1)
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
		templateFile = flag.String("template", defaultTemplate, "Template file path")
	)
	flag.Parse()

	if *leafName == "" {
		showHelp()
	}
	if *templateFile == defaultTemplate && (*neighborIP == "" || *networkToAdvertise == "") {
		showHelp()
	}

	if *outputDir == "" {
		*outputDir = filepath.Join("..", *leafName)
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

	if err := GenerateFromTemplate(*templateFile, *outputDir, config); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
