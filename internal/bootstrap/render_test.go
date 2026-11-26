// SPDX-License-Identifier:Apache-2.0

package bootstrap

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderGolden(t *testing.T) {
	for _, fixture := range []string{"minimal", "customized"} {
		t.Run(fixture, func(t *testing.T) {
			g := NewWithT(t)
			files := renderFixture(t, fixture)
			goldenDir := filepath.Join("testdata", "golden", fixture)
			if *update {
				g.Expect(os.RemoveAll(goldenDir)).To(Succeed())
				g.Expect(os.MkdirAll(goldenDir, 0o755)).To(Succeed())
				g.Expect(Write(openTestRoot(t, goldenDir), files)).To(Succeed())
			}
			for _, f := range files {
				want, err := os.ReadFile(filepath.Join(goldenDir, f.Path))
				g.Expect(err).To(Succeed(), "missing golden file, run with -update")
				g.Expect(string(f.content)).To(Equal(string(want)), f.Path)
			}
		})
	}
}

func TestRenderControllerKnobs(t *testing.T) {
	g := NewWithT(t)
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
		g.Expect(controller).To(ContainSubstring(want))
	}
	g.Expect(controller).To(ContainSubstring("--host-configuration-dir=/etc/openperouter/configs"))

	minimal := fileContent(t, renderFixture(t, "minimal"), "controller.container")
	g.Expect(minimal).NotTo(ContainSubstring("--max-reconcile-backoff"))
	g.Expect(minimal).To(ContainSubstring("--host-configuration-dir=/etc/openperouter/configs"))
}

func TestRenderFRRDaemons(t *testing.T) {
	g := NewWithT(t)
	files := renderFixture(t, "minimal")
	g.Expect(fileContent(t, files, "frr.container")).To(ContainSubstring("Volume=" + frrDaemonsPath + ":/etc/frr/daemons:ro,z"))
	g.Expect(fileContent(t, files, "daemons")).To(Equal(string(frrDaemons)))
}

// TestFRRDaemonsMatchesImage keeps the embedded file in sync with
// the daemons file baked into the router image.
func TestFRRDaemonsMatchesImage(t *testing.T) {
	g := NewWithT(t)
	image, err := os.ReadFile("../../systemdmode/frrconfig/daemons")
	g.Expect(err).To(Succeed())
	g.Expect(string(frrDaemons)).To(Equal(string(image)))
}

func renderFixture(t *testing.T, name string) []File {
	g := NewWithT(t)
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".yaml"))
	g.Expect(err).To(Succeed())
	cfg, err := ParseConfig(data)
	g.Expect(err).To(Succeed())
	files, err := Render(cfg)
	g.Expect(err).To(Succeed())
	return files
}

func fileContent(t *testing.T, files []File, name string) string {
	t.Helper()
	for _, f := range files {
		if filepath.Base(f.Path) == name {
			return string(f.content)
		}
	}
	t.Fatalf("file %s not rendered", name)
	return ""
}

func openTestRoot(t *testing.T, path string) *os.Root {
	g := NewWithT(t)
	t.Helper()
	root, err := os.OpenRoot(path)
	g.Expect(err).To(Succeed())
	t.Cleanup(func() { g.Expect(root.Close()).To(Succeed()) })
	return root
}
