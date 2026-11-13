// SPDX-License-Identifier:Apache-2.0

package bootstrap

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/staticconfiguration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

func renderFixture(t *testing.T, name string) []File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
	require.NoError(t, err)
	cfg, err := ParseConfig(data)
	require.NoError(t, err)
	files, err := Render(cfg)
	require.NoError(t, err)
	return files
}

func TestRenderGolden(t *testing.T) {
	for _, fixture := range []string{"minimal", "customized"} {
		t.Run(fixture, func(t *testing.T) {
			files := renderFixture(t, fixture)
			goldenDir := filepath.Join("testdata", "golden", fixture)
			if *update {
				require.NoError(t, os.RemoveAll(goldenDir))
				require.NoError(t, Write(goldenDir, files))
			}
			for _, f := range files {
				want, err := os.ReadFile(filepath.Join(goldenDir, f.Path))
				require.NoError(t, err, "missing golden file, run with -update")
				assert.Equal(t, string(want), string(f.Content), f.Path)
			}
		})
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	assert.Equal(t, renderFixture(t, "customized"), renderFixture(t, "customized"))
}

func TestRenderQuadletInvariants(t *testing.T) {
	for _, fixture := range []string{"minimal", "customized"} {
		for _, f := range renderFixture(t, fixture) {
			if filepath.Dir(f.Path) != QuadletDir {
				continue
			}
			content := string(f.Content)
			assert.NotContains(t, content, "[Install]", f.Path)
			assert.NotContains(t, content, "network-online", f.Path)
			assert.Contains(t, content, "DefaultDependencies=false", f.Path)
			if strings.HasSuffix(f.Path, ".container") {
				assert.Contains(t, content, "Pull=missing", f.Path)
			}
		}
	}
}

func TestRenderControllerKnobs(t *testing.T) {
	controller := fileContent(t, renderFixture(t, "customized"), "controller.container")
	for _, want := range []string{
		"Image=quay.io/openperouter/router:v0.1.0",
		"--loglevel=debug",
		"--health-probe-bind-address=:9181",
		"--bgplistenlimit=4096",
		"--max-reconcile-backoff=30s",
		"--ovssocket=unix:/run/openvswitch/db.sock",
		"Volume=/run/openvswitch:/run/openvswitch:rshared",
		"http://127.0.0.1:9181/healthz",
	} {
		assert.Contains(t, controller, want)
	}
	assert.Contains(t, controller, "--host-configuration-dir=/etc/openperouter-routing")

	minimal := fileContent(t, renderFixture(t, "minimal"), "controller.container")
	assert.NotContains(t, minimal, "--max-reconcile-backoff")
	assert.Contains(t, minimal, "--host-configuration-dir=/etc/openperouter-routing")
	assert.Contains(t, minimal, "Volume=/etc/openperouter:/etc/openperouter-routing:ro")
}

// TestRenderQuadletGenerator runs the installed Podman Quadlet generator
// against the rendered units when it is available.
func TestRenderQuadletGenerator(t *testing.T) {
	const generator = "/usr/lib/systemd/system-generators/podman-system-generator"
	if _, err := os.Stat(generator); err != nil {
		t.Skip("podman quadlet generator not installed")
	}
	for _, fixture := range []string{"minimal", "customized"} {
		t.Run(fixture, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Write(root, renderFixture(t, fixture)))
			cmd := exec.Command(generator, "--dryrun")
			cmd.Env = append(os.Environ(), "QUADLET_UNIT_DIRS="+filepath.Join(root, QuadletDir))
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			for _, unit := range []string{"routerpod-pod.service", "controllerpod-pod.service", "controller.service",
				"frr.service", "reloader.service", "frr-sockets-volume.service"} {
				assert.Contains(t, string(out), "---"+unit+"---")
			}
			assert.NotContains(t, string(out), "network-online")
		})
	}
}

func TestWrite(t *testing.T) {
	root := t.TempDir()
	files := renderFixture(t, "customized")
	require.NoError(t, Write(root, files))

	for _, f := range files {
		info, err := os.Stat(filepath.Join(root, f.Path))
		require.NoError(t, err)
		assert.Equal(t, f.Mode, info.Mode().Perm(), f.Path)
	}

	// Unchanged content leaves files untouched.
	before, err := os.Stat(filepath.Join(root, FRRDaemonsPath))
	require.NoError(t, err)
	require.NoError(t, Write(root, files))
	after, err := os.Stat(filepath.Join(root, FRRDaemonsPath))
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime())

	// No temporary files are left behind.
	entries, err := os.ReadDir(filepath.Join(root, QuadletDir))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "."), e.Name())
	}
}

func fileContent(t *testing.T, files []File, name string) string {
	t.Helper()
	for _, f := range files {
		if filepath.Base(f.Path) == name {
			return string(f.Content)
		}
	}
	t.Fatalf("file %s not rendered", name)
	return ""
}

// TestRenderedFilesReadByRuntime checks separate boot and runtime files can share a directory.
func TestRenderedFilesReadByRuntime(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "etc/openperouter")
	require.NoError(t, os.MkdirAll(runtimeDir, 0o755))
	runtimeConfig, err := os.ReadFile("testdata/openpe_underlay.yaml")
	require.NoError(t, err)
	runtimePath := filepath.Join(runtimeDir, "openpe_underlay.yaml")
	require.NoError(t, os.WriteFile(runtimePath, runtimeConfig, 0o600))
	bootConfig, err := os.ReadFile("testdata/minimal.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runtimeDir, "openpe_boot.yaml"), bootConfig, 0o600))
	nodePath := filepath.Join(root, "var/lib/openperouter/node-config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(nodePath), 0o755))
	nodeData := []byte("nodeIndex: {index: 0}\nlogLevel: info\n")
	require.NoError(t, os.WriteFile(nodePath, nodeData, 0o600))
	require.NoError(t, Write(root, renderFixture(t, "minimal")))
	gotNode, err := os.ReadFile(nodePath)
	require.NoError(t, err)
	assert.Equal(t, nodeData, gotNode)

	// Rendering must not overwrite administrator runtime configuration.
	got, err := os.ReadFile(runtimePath)
	require.NoError(t, err)
	assert.Equal(t, runtimeConfig, got)
	configs, err := staticconfiguration.ReadRouterConfigs(runtimeDir)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	require.Len(t, configs[0].Underlays, 1)
	assert.Equal(t, int64(64514), configs[0].Underlays[0].ASN)
	require.Len(t, configs[0].Underlays[0].Neighbors, 1)
	assert.Equal(t, "secret", *configs[0].Underlays[0].Neighbors[0].Password)
}

func TestRenderFRRDaemons(t *testing.T) {
	files := renderFixture(t, "minimal")
	assert.Contains(t, fileContent(t, files, "frr.container"), "Volume="+FRRDaemonsPath+":/etc/frr/daemons:ro,z")
	assert.Equal(t, string(frrDaemons), fileContent(t, files, "daemons"))
}

// TestFRRDaemonsMatchesImage keeps the embedded file in sync with
// the daemons file baked into the router image.
func TestFRRDaemonsMatchesImage(t *testing.T) {
	image, err := os.ReadFile("../../systemdmode/frrconfig/daemons")
	require.NoError(t, err)
	assert.Equal(t, string(image), string(frrDaemons))
}
