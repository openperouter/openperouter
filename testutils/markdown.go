// SPDX-License-Identifier:Apache-2.0

package testutils

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MarkdownFile represents a discovered markdown file with metadata.
// RelativePath contains the file path relative to the search root.
// TestName contains the derived test name from the file.
type MarkdownFile struct {
	RelativePath string
	TestName     string
}

// ExtractYAMLFromMarkdown extracts YAML code blocks from a markdown file.
// It scans the file for fenced code blocks marked with ```yaml or ```yml,
// extracts their content, and filters to include only blocks containing
// OpenPERouter custom resources.
// Returns a slice of YAML content strings and any error encountered.
func ExtractYAMLFromMarkdown(filePath string) ([]string, error) {
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

		// Check for YAML block start
		if !inYAMLBlock && yamlBlockStart.MatchString(line) {
			inYAMLBlock = true
			currentBlock.Reset()
			continue
		}

		// Check for YAML block end
		if inYAMLBlock && yamlBlockEnd.MatchString(line) {
			inYAMLBlock = false
			yamlContent := currentBlock.String()
			// Only include blocks that contain OpenPERouter CRs
			if containsOpenPERouterCR(yamlContent) {
				yamlBlocks = append(yamlBlocks, yamlContent)
			}
			continue
		}

		// Accumulate lines within a YAML block
		if inYAMLBlock {
			currentBlock.WriteString(line)
			currentBlock.WriteString("\n")
		}
	}

	return yamlBlocks, nil
}

// DiscoverMarkdownFiles recursively walks through contentDir and returns paths
// to all markdown files (.md extension). Directories are skipped during traversal.
// Returns a slice of markdown file paths and any error encountered during the walk.
func DiscoverMarkdownFiles(contentDir string) ([]string, error) {
	var files []string

	err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-markdown files
		if info.IsDir() || (!strings.HasSuffix(info.Name(), ".md")) {
			return nil
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}
