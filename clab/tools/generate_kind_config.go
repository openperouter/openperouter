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
	IncludeNetworking bool
}

func main() {
	var (
		includeNetworking = flag.Bool("include-networking", false, "Include networking section in kind config")
		outputFile        = flag.String("output", "../kind-configuration-registry.yaml", "Kind configuration output file")
		templateFile      = flag.String("template", "kind-configuration-registry.yaml.template", "Kind template file path")
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
		IncludeNetworking: *includeNetworking,
	}

	outputDir := filepath.Dir(*outputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Error creating output directory: %v", err)
	}

	file, err := os.Create(*outputFile)
	if err != nil {
		log.Fatalf("Error creating output file: %v", err)
	}
	defer file.Close()

	if err := tmpl.Execute(file, config); err != nil {
		log.Fatalf("Error executing template: %v", err)
	}
}
