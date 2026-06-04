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

type LeafKind1Config struct {
	ASN             int
	SpineIP         string
	IPv4ListenRange string
	IPv6ListenRange string
}

func main() {
	var (
		leafName        = flag.String("leaf", "", "LeafKind name (e.g., leafkind1, leafkind2)")
		asn             = flag.Int("asn", 0, "BGP ASN for leafkind node")
		spineIP         = flag.String("spine-ip", "", "Spine IP address")
		ipv4ListenRange = flag.String("ipv4-listen-range", "", "BGP IPv4 listen range (CIDR format)")
		ipv6ListenRange = flag.String("ipv6-listen-range", "", "BGP IPv6 listen range (CIDR format)")
		outputDir       = flag.String("output", "", "Output directory (default: ../{leaf_name})")
		templateFile    = flag.String("template", "frr_template/leafkind.conf.template", "Template file path")
	)
	flag.Parse()

	if *leafName == "" || *asn == 0 || *spineIP == "" || *ipv4ListenRange == "" || *ipv6ListenRange == "" {
		fmt.Println("Usage: generate_leafkind -leaf <name> -asn <asn> -spine-ip <ip> -ipv4-listen-range <cidr> -ipv6-listen-range <cidr> [options]")
		fmt.Println("Example: generate_leafkind -leaf leafkind1 -asn 64512 -spine-ip 192.168.1.4 -ipv4-listen-range 192.168.11.0/24 -ipv6-listen-range 2001:db8:11::/64")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *outputDir == "" {
		*outputDir = filepath.Join("..", *leafName)
	}

	config := LeafKind1Config{
		ASN:             *asn,
		SpineIP:         *spineIP,
		IPv4ListenRange: *ipv4ListenRange,
		IPv6ListenRange: *ipv6ListenRange,
	}

	if err := GenerateFromTemplate(*templateFile, *outputDir, config); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
