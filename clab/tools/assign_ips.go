// SPDX-License-Identifier:Apache-2.0

package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	inputFile := flag.String("file", "", "Input CSV file with IP assignments")
	containerEngine := flag.String("engine", "docker", "Container engine to use (docker, podman, etc.)")

	// Parse command line arguments
	flag.Parse()

	// Validate input file parameter
	if *inputFile == "" {
		fmt.Println("Error: Input file is required")
		fmt.Println("Usage: assign_ips -file=<input_file> [-engine=<container_engine>]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// #nosec G304
	file, err := os.Open(*inputFile)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close() // nolint:errcheck

	reader := csv.NewReader(bufio.NewReader(file))
	for {
		record, err := reader.Read()
		if err != nil {
			break
		}

		if len(record) < 3 || strings.HasPrefix(record[0], "#") {
			continue
		}

		containerName := record[0]
		interfaceName := record[1]
		ipAddress := record[2]

		fmt.Printf("Assigning IP %s to interface %s in container %s...\n", ipAddress, interfaceName, containerName)

		// #nosec G204
		cmdAdd := exec.Command(*containerEngine, "exec", containerName, "ip", "addr", "add", ipAddress, "dev", interfaceName)
		if err := cmdAdd.Run(); err != nil {
			fmt.Printf("Error assigning IP: %v \n", err)
			continue
		}

		// #nosec G204
		cmdUp := exec.Command(*containerEngine, "exec", containerName, "ip", "link", "set", interfaceName, "up")
		if err := cmdUp.Run(); err != nil {
			fmt.Printf("Error bringing interface up: %v\n", err)
			continue
		}
	}
}
