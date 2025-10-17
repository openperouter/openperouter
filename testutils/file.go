// SPDX-License-Identifier:Apache-2.0

// Package testutils provides utility functions for testing, including file discovery
// and resource validation helpers.
package testutils

import (
	"os"
	"path/filepath"
	"strings"
)

// matchSuffix checks if a filename ends with any of the provided suffixes.
// It returns true if a match is found, false otherwise.
func matchSuffix(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// DiscoverFiles recursively walks through contentDir and returns paths to all files
// that match any of the provided suffixes. Directories are skipped during traversal.
// Returns a slice of file paths and any error encountered during the walk.
func DiscoverFiles(contentDir string, suffixes []string) ([]string, error) {
	var files []string

	err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip directories
		if info.IsDir() {
			return nil
		}
		// Only include files matching the specified suffixes
		if !matchSuffix(info.Name(), suffixes) {
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
