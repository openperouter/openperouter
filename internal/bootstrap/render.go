// SPDX-License-Identifier:Apache-2.0

package bootstrap

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Host paths of the rendered files.
const (
	QuadletDir                    = "/etc/containers/systemd/openperouter"
	FRRDaemonsPath                = "/var/lib/openperouter/frr/daemons"
	runtimeConfigurationMountPath = "/etc/openperouter-routing"
	quadletFileMode               = 0o644
	directoryMode                 = 0o755
	templateFileSuffix            = ".tmpl"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

// frrDaemons is the FRR daemons file mounted into the FRR container. It
// matches the file baked into the router image.
//
//go:embed files/daemons
var frrDaemons []byte

var templates = template.Must(template.New("").Option("missingkey=error").ParseFS(templatesFS, "templates/*.tmpl"))

// File is a rendered host file.
type File struct {
	Path    string
	Content []byte
	Mode    fs.FileMode
}

type templateData struct {
	Deployment
	RuntimeConfigurationMountPath string
	ControllerArgs                string
	FRRDaemonsPath                string
}

// Render produces the full set of files for the given configuration.
// Output is deterministic: the same configuration always yields the same bytes.
func Render(deployment Deployment) ([]File, error) {
	data := templateData{
		Deployment:                    deployment,
		RuntimeConfigurationMountPath: runtimeConfigurationMountPath,
		ControllerArgs:                strings.Join(controllerArgs(deployment), " "),
		FRRDaemonsPath:                FRRDaemonsPath,
	}

	entries, err := fs.Glob(templatesFS, "templates/*"+templateFileSuffix)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(entries)+2)
	for _, entry := range entries {
		name := filepath.Base(entry)
		var buf bytes.Buffer
		if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
			return nil, fmt.Errorf("render: %s: %w", name, err)
		}
		files = append(files, File{
			Path:    filepath.Join(QuadletDir, strings.TrimSuffix(name, templateFileSuffix)),
			Content: buf.Bytes(),
			Mode:    quadletFileMode,
		})
	}

	files = append(files, File{Path: FRRDaemonsPath, Content: frrDaemons, Mode: quadletFileMode})
	return files, nil
}

func controllerArgs(deployment Deployment) []string {
	args := []string{
		"--frrconfig /etc/perouter/frr/frr.conf",
		"--pid-path /etc/perouter/frr/frr.pid",
		"--reloader-socket /etc/perouter/frr/frr.socket",
		"--mode host",
		"--namespace openperouter-system",
		"--cni-plugin-dirs=/opt/openperouter/cni/bin/",
		"--cni-cache-dir=/var/lib/openperouter/cni/cache",
		"--datapath=" + datapathKernel,
		"--loglevel=" + deployment.LogLevel,
		fmt.Sprintf("--health-probe-bind-address=:%d", deployment.HealthProbePort),
		fmt.Sprintf("--bgplistenlimit=%d", deployment.BGPListenLimit),
		"--ovssocket=" + deployment.OVSSocketPath,
	}
	if deployment.ControllerMaxReconcileBackoffSeconds != nil {
		args = append(args, fmt.Sprintf("--max-reconcile-backoff=%ds", *deployment.ControllerMaxReconcileBackoffSeconds))
	}
	args = append(args, "--host-configuration-dir="+runtimeConfigurationMountPath)
	return args
}

// Write writes files under root, creating parent directories. Each file is
// written to a temporary file and renamed in place. Files whose content is
// unchanged are left untouched.
func Write(root string, files []File) error {
	for _, f := range files {
		if err := writeFile(filepath.Join(root, f.Path), f.Content, f.Mode); err != nil {
			return fmt.Errorf("write: %s: %w", f.Path, err)
		}
	}
	return nil
}

func writeFile(path string, content []byte, mode fs.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return os.Chmod(path, mode)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, directoryMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(content)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if err := errors.Join(err, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
