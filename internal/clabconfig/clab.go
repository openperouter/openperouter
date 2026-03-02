// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ClabTopology represents the top-level containerlab topology file.
type ClabTopology struct {
	Name     string               `yaml:"name"`
	Topology ClabTopologySection  `yaml:"topology"`
}

// ClabTopologySection contains the nodes and links of a topology.
type ClabTopologySection struct {
	Nodes map[string]ClabNode `yaml:"nodes"`
	Links []ClabLink          `yaml:"links"`
}

// ClabNode represents a single node in the containerlab topology.
type ClabNode struct {
	Kind  string   `yaml:"kind"`
	Image string   `yaml:"image"`
	Binds []string `yaml:"binds"`
}

// ClabLink represents a link between two nodes.
type ClabLink struct {
	Endpoints []string `yaml:"endpoints"`
}

// Endpoint represents a parsed endpoint with node name and interface name.
type Endpoint struct {
	Node      string
	Interface string
}

// ParseEndpoints parses the two endpoint strings of a ClabLink into an array
// of two Endpoint values. Each endpoint string must be in the format
// "nodeName:interfaceName".
func (l ClabLink) ParseEndpoints() ([2]Endpoint, error) {
	var result [2]Endpoint

	if len(l.Endpoints) != 2 {
		return result, fmt.Errorf("expected 2 endpoints, got %d", len(l.Endpoints))
	}

	for i, ep := range l.Endpoints {
		parts := strings.SplitN(ep, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return result, fmt.Errorf("invalid endpoint format %q: expected \"nodeName:interfaceName\"", ep)
		}
		result[i] = Endpoint{
			Node:      parts[0],
			Interface: parts[1],
		}
	}

	return result, nil
}

// LoadClab reads a containerlab topology YAML file from the given path and
// returns the parsed ClabTopology.
func LoadClab(path string) (*ClabTopology, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading clab topology file %s: %w", path, err)
	}

	var topo ClabTopology
	if err := yaml.Unmarshal(data, &topo); err != nil {
		return nil, fmt.Errorf("parsing clab topology file %s: %w", path, err)
	}

	return &topo, nil
}
