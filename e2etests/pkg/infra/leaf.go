// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"runtime"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	corev1 "k8s.io/api/core/v1"
)

const (
	HostARedIPv4     = "192.168.20.2"
	HostABlueIPv4    = "192.168.21.2"
	HostADefaultIPv4 = "192.168.22.2"
	HostBRedIPv4     = "192.169.20.2"
	HostBBlueIPv4    = "192.169.21.2"

	HostARedIPv6  = "2001:db8:20::2"
	HostABlueIPv6 = "2001:db8:21::2"
	HostBRedIPv6  = "2001:db8:169:20::2"
	HostBBlueIPv6 = "2001:db8:169:21::2"
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
	LeafKindConfig = LeafKind{
		Container: KindLeafContainer,
	}
)

type LeafConfiguration struct {
	Leaf
	Red     Addresses
	Blue    Addresses
	Default Addresses
}

type LeafKindConfiguration struct {
	EnableBFD bool
	Neighbors []string
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

type LeafKind struct {
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

// LeafKindConfigToFRR reads a Go template from the testdata directory and generates a string for leafkind.
func LeafKindConfigToFRR(config LeafKindConfiguration) (string, error) {
	_, currentFile, _, _ := runtime.Caller(0) // current file's path
	templatePath := filepath.Join(filepath.Dir(currentFile), "testdata", "leafkind.tmpl")

	// Read the template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("leafkind.tmpl").Parse(string(tmplContent))
	if err != nil {
		return "", err
	}

	var result bytes.Buffer
	if err := tmpl.Execute(&result, config); err != nil {
		return "", err
	}

	return result.String(), nil
}

const EnableBFD = true

// UpdateLeafKindConfig updates the leafkind configuration file with the given configuration.
// It takes nodes and automatically builds the neighbors list from their IPs.
func UpdateLeafKindConfig(nodes []corev1.Node, enableBFD bool) error {
	neighbors := []string{}
	for _, node := range nodes {
		neighborIP, err := NeighborIP(KindLeaf, node.Name)
		if err != nil {
			return err
		}
		neighbors = append(neighbors, neighborIP)
	}

	config := LeafKindConfiguration{
		EnableBFD: enableBFD,
		Neighbors: neighbors,
	}

	configString, err := LeafKindConfigToFRR(config)
	if err != nil {
		return err
	}

	return LeafKindConfig.ReloadConfig(configString)
}
