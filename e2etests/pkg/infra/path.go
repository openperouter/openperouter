// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"os"
	"path/filepath"
	"fmt"
)

// GetProjectRoot finds the project root by searching for the go.mod file.
func GetProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
