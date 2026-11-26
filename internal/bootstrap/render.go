// SPDX-License-Identifier:Apache-2.0

package bootstrap

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Host paths of the rendered files.
const (
	quadletDir         = "/etc/containers/systemd/openperouter"
	frrDaemonsPath     = "/var/lib/openperouter/frr/daemons"
	quadletFileMode    = 0o644
	directoryMode      = 0o755
	templateFileSuffix = ".tmpl"
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
	// Path is the absolute host path of the rendered file.
	Path    string
	content []byte
	mode    fs.FileMode
}

type templateData struct {
	Deployment
	ControllerArgs string
	FRRDaemonsPath string
}

// Render produces the full set of files for the given configuration.
func Render(deployment Deployment) ([]File, error) {
	data := templateData{
		Deployment:     deployment,
		ControllerArgs: strings.Join(controllerArgs(deployment), " "),
		FRRDaemonsPath: frrDaemonsPath,
	}

	entries, err := fs.Glob(templatesFS, "templates/*"+templateFileSuffix)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(entries)+1)
	for _, entry := range entries {
		name := filepath.Base(entry)
		var buf bytes.Buffer
		if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
			return nil, fmt.Errorf("render: %s: %w", name, err)
		}
		files = append(files, File{
			Path:    filepath.Join(quadletDir, strings.TrimSuffix(name, templateFileSuffix)),
			content: buf.Bytes(),
			mode:    quadletFileMode,
		})
	}

	files = append(files, File{Path: frrDaemonsPath, content: frrDaemons, mode: quadletFileMode})
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
		"--datapath=" + deployment.Datapath,
		"--loglevel=" + deployment.LogLevel,
		fmt.Sprintf("--health-probe-bind-address=:%d", deployment.HealthProbePort),
		fmt.Sprintf("--bgplistenlimit=%d", deployment.BGPListenLimit),
		"--ovssocket=" + deployment.OVSSocketPath,
	}
	if deployment.ControllerMaxReconcileBackoffSeconds != nil {
		args = append(args, fmt.Sprintf("--max-reconcile-backoff=%ds", *deployment.ControllerMaxReconcileBackoffSeconds))
	}
	return append(args, "--host-configuration-dir=/etc/openperouter/configs")
}

// Write writes files under root, creating parent directories as needed.
// Files with matching content keep their modification times.
func Write(root *os.Root, files []File) error {
	for _, f := range files {
		if err := writeFile(root, strings.TrimPrefix(f.Path, "/"), f.content, f.mode); err != nil {
			return fmt.Errorf("write: %s: %w", f.Path, err)
		}
	}
	return nil
}

func writeFile(root *os.Root, path string, content []byte, mode fs.FileMode) error {
	existing, err := root.Open(path)
	if err == nil {
		data, readErr := io.ReadAll(existing)
		if readErr == nil && bytes.Equal(data, content) {
			return errors.Join(existing.Chmod(mode), existing.Close())
		}
		_ = existing.Close()
	}
	dir := filepath.Dir(path)
	if err := root.MkdirAll(dir, directoryMode); err != nil {
		return err
	}
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		return errors.Join(err, file.Close())
	}
	return errors.Join(file.Chmod(mode), file.Close())
}
