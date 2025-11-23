// SPDX-License-Identifier:Apache-2.0

package examplesvalidation

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// ExtractYAMLFromMarkdown extracts YAML code blocks from a markdown file.
// It scans the file for fenced code blocks marked with ```yaml or ```yml,
// extracts their content, and filters to include only blocks containing
// OpenPERouter custom resources.
// Returns a slice of YAML content strings and any error encountered.
func extractYAMLFromMarkdown(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var yamlBlocks []string
	var currentBlock strings.Builder
	inYAMLBlock := false

	// Regex patterns to detect YAML code block delimiters
	// Matches ```yaml or ```yml with optional surrounding whitespace
	yamlBlockStart := regexp.MustCompile(`^\s*` + "```" + `\s*ya?ml\s*$`)
	yamlBlockEnd := regexp.MustCompile(`^\s*` + "```" + `\s*$`)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if !inYAMLBlock && yamlBlockStart.MatchString(line) {
			inYAMLBlock = true
			currentBlock.Reset()
			continue
		}

		if inYAMLBlock && yamlBlockEnd.MatchString(line) {
			inYAMLBlock = false
			yamlContent := currentBlock.String()
			if containsOpenPERouterCR(yamlContent) {
				yamlBlocks = append(yamlBlocks, yamlContent)
			}
			continue
		}

		if inYAMLBlock {
			currentBlock.WriteString(line)
			currentBlock.WriteString("\n")
		}
	}

	return yamlBlocks, nil
}
