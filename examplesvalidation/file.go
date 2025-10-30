// SPDX-License-Identifier:Apache-2.0

package examplesvalidation

import (
	"os"
	"path/filepath"
	"strings"
)

// discoverFiles recursively walks through contentDir and returns paths to all files
// that match any of the provided suffixes. Directories are skipped during traversal.
// Returns a slice of file paths and any error encountered during the walk.
func discoverFiles(contentDir string, suffixes []string) ([]string, error) {
	var files []string

	err := filepath.Walk(contentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !matchSuffixes(info.Name(), suffixes) {
			return nil
		}

		files = append(files, filepath.Join(contentDir, path))
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// matchSuffixes checks if a filename ends with any of the provided suffixes.
// It returns true if a match is found, false otherwise.
func matchSuffixes(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
