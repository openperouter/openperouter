// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
)

// GenerateFromTemplate reads a template file, executes it with the given config,
// and writes the result to the output directory.
func GenerateFromTemplate(templateFile, outputDir string, config any) error {
	tmplContent, err := os.ReadFile(templateFile)
	if err != nil {
		return fmt.Errorf("error reading template file: %w", err)
	}

	tmpl, err := template.New("frr").Parse(string(tmplContent))
	if err != nil {
		return fmt.Errorf("error parsing template: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("error creating output directory: %w", err)
	}

	outputFile := filepath.Join(outputDir, "frr.conf")
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Warning: error closing output file: %v", err)
		}
	}()

	if err := tmpl.Execute(file, config); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	return nil
}
