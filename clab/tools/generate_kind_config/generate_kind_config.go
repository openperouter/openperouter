// SPDX-License-Identifier:Apache-2.0

package main

import (
	"flag"
	"html/template"
	"log"
	"os"
	"path/filepath"
)

type KindConfig struct {
	DisableDefaultCNI bool
	ClusterName       string
}

func main() {
	var (
		disableDefaultCNI = flag.Bool("disable-default-cni", false, "Disable default CNI in kind config")
		clusterName       = flag.String("cluster-name", "pe-kind", "Name of the kind cluster")
		outputFile        = flag.String("output", "../kind-configuration-registry.yaml", "Kind configuration output file")
		templateFile      = flag.String("template",
			"../kind_template/kind-configuration-registry.yaml.template", "Kind template file path")
	)
	flag.Parse()

	tmplContent, err := os.ReadFile(*templateFile)
	if err != nil {
		log.Fatalf("Error reading template file: %v", err)
	}

	tmpl, err := template.New("kind").Parse(string(tmplContent))
	if err != nil {
		log.Fatalf("Error parsing template: %v", err)
	}

	config := KindConfig{
		DisableDefaultCNI: *disableDefaultCNI,
		ClusterName:       *clusterName,
	}

	outputDir := filepath.Dir(*outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	file, err := os.Create(*outputFile)
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
