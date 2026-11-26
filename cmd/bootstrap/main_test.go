// SPDX-License-Identifier:Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestRun(t *testing.T) {
	const valid = `image: quay.io/openperouter/router:main
logLevel: info
`
	tests := []struct {
		name       string
		config     string
		rootPath   string
		configLink bool
		outputLink bool
		wantErr    string
	}{
		{name: "render default", config: valid},
		{name: "unsupported log level", config: strings.Replace(valid, "logLevel: info", "logLevel: none", 1),
			wantErr: "logLevel"},
		{name: "multiple documents", config: valid + "---\nlogLevel: debug\n", wantErr: "exactly one YAML document"},
		{name: "missing root", rootPath: "missing", wantErr: "open host root"},
		{name: "config symlink outside root", config: valid, configLink: true, wantErr: "read boot configuration"},
		{name: "output symlink outside root", config: valid, outputLink: true, wantErr: "write:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			root := filepath.Join(t.TempDir(), tt.rootPath)
			if tt.config != "" {
				setupConfig(t, root, tt.config, tt.configLink)
			}
			if tt.outputLink {
				g.Expect(os.Symlink(t.TempDir(), filepath.Join(root, "etc"))).To(Succeed())
			}
			err := run(root)
			if tt.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				_, err = os.Stat(filepath.Join(root, "etc/containers"))
				g.Expect(err).To(MatchError(os.ErrNotExist))
				_, err = os.Stat(filepath.Join(root, "var/lib/openperouter/frr"))
				g.Expect(err).To(MatchError(os.ErrNotExist))
				return
			}
			g.Expect(err).To(Succeed())
			g.Expect(filepath.Join(root, "etc/containers/systemd/openperouter/controller.container")).To(BeAnExistingFile())
			g.Expect(filepath.Join(root, "var/lib/openperouter/frr/daemons")).To(BeAnExistingFile())
			_, err = os.Stat(filepath.Join(root, "var/lib/openperouter/node-config.yaml"))
			g.Expect(err).To(MatchError(os.ErrNotExist))
		})
	}
}

func setupConfig(t *testing.T, root, config string, symlinkOutside bool) {
	t.Helper()
	g := NewWithT(t)
	configDir := filepath.Join(root, "var/lib/openperouter")
	if symlinkOutside {
		outside := t.TempDir()
		g.Expect(os.MkdirAll(filepath.Dir(configDir), 0o755)).To(Succeed())
		g.Expect(os.Symlink(outside, configDir)).To(Succeed())
		configDir = outside
	} else {
		g.Expect(os.MkdirAll(configDir, 0o755)).To(Succeed())
	}
	g.Expect(os.WriteFile(filepath.Join(configDir, "boot.yaml"), []byte(config), 0o600)).To(Succeed())
}
