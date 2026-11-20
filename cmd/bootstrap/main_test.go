// SPDX-License-Identifier:Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapCLI(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "bootstrap")
	output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput()
	require.NoError(t, err, string(output))
	const valid = `image: quay.io/openperouter/router:main
logLevel: info
runtimeConfiguration:
  directory: /etc/openperouter
`
	tests := []struct {
		name    string
		config  string
		args    []string
		wantErr string
	}{
		{name: "render", config: valid},
		{name: "unsupported log level", config: strings.Replace(valid, "logLevel: info", "logLevel: none", 1),
			wantErr: "logLevel"},
		{name: "multiple documents", config: valid + "---\npassword: topsecret\n", wantErr: "exactly one YAML document"},
		{name: "unknown flag", config: valid, args: []string{"--unknown"}, wantErr: "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "etc/openperouter/openpe_boot.yaml")
			require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
			require.NoError(t, os.WriteFile(configPath, []byte(tt.config), 0o600))
			args := append([]string{"--root", root}, tt.args...)
			output, err := exec.Command(binary, args...).CombinedOutput()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, string(output), tt.wantErr)
				assert.NotContains(t, string(output), "topsecret")
				_, err = os.Stat(filepath.Join(root, "etc/containers"))
				assert.ErrorIs(t, err, os.ErrNotExist)
				_, err = os.Stat(filepath.Join(root, "var"))
				assert.ErrorIs(t, err, os.ErrNotExist)
				return
			}
			require.NoError(t, err, string(output))
			assert.Contains(t, string(output), "rendered 7 files")
			controller, err := os.ReadFile(filepath.Join(root, "etc/containers/systemd/openperouter/controller.container"))
			require.NoError(t, err)
			assert.Contains(t, string(controller), "Image=quay.io/openperouter/router:main")
			_, err = os.Stat(filepath.Join(root, "var/lib/openperouter/node-config.yaml"))
			assert.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}
