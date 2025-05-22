// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"runtime"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
)

const (
	HostARedIP  = "192.168.20.2"
	HostABlueIP = "192.168.21.2"
	HostBRedIP  = "192.169.20.2"
	HostBBlueIP = "192.169.21.2"
)

var (
	LeafAConfig = Leaf{
		VTEPIP:       "100.64.0.1",
		SpineAddress: "192.168.1.0",
		Container:    LeafAContainer,
	}
	LeafBConfig = Leaf{
		VTEPIP:       "100.64.0.2",
		SpineAddress: "192.168.1.2",
		Container:    LeafBContainer,
	}
)

type LeafConfiguration struct {
	Leaf
	Red  Addresses
	Blue Addresses
}

type Addresses struct {
	RedistributeConnected bool
	IPV4                  []string
	IPV6                  []string
}

type Leaf struct {
	VTEPIP       string
	SpineAddress string
	frr.Container
}

func (l Leaf) VTEPPrefix() string {
	return l.VTEPIP + "/32"
}

// LeafConfigToFRR reads a Go template from the testdata directory and generates a string.
func LeafConfigToFRR(config LeafConfiguration) (string, error) {
	_, currentFile, _, _ := runtime.Caller(0) // current file's path
	templatePath := filepath.Join(filepath.Dir(currentFile), "testdata", "leaf.tmpl")

	// Read the template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("leaf.tmpl").Parse(string(tmplContent))
	if err != nil {
		return "", err
	}

	var result bytes.Buffer
	if err := tmpl.Execute(&result, config); err != nil {
		return "", err
	}

	return result.String(), nil
}
