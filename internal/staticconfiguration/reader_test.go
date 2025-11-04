// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openperouter/openperouter/api/static"
)

func TestReadFromFile(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expected    *static.PERouterConfig
		expectError bool
	}{
		{
			name:     "valid yaml config",
			content:  "nodeIndex: 42\n",
			expected: &static.PERouterConfig{NodeIndex: 42},
		},
		{
			name:     "valid yaml with zero value",
			content:  "nodeIndex: 0\n",
			expected: &static.PERouterConfig{NodeIndex: 0},
		},
		{
			name:        "invalid yaml",
			content:     "invalid: [unclosed\n",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")

			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test config file: %v", err)
			}

			config, err := ReadFromFile(configPath)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config.NodeIndex != tt.expected.NodeIndex {
				t.Errorf("expected NodeIndex %d, got %d", tt.expected.NodeIndex, config.NodeIndex)
			}
		})
	}
}

func TestReadFromFile_NonExistentFile(t *testing.T) {
	_, err := ReadFromFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error when reading non-existent file")
	}
}
