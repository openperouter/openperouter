// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"bytes"
	"embed"
	"fmt"
	"sort"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// GeneratedFiles holds the generated configuration files for a single node.
type GeneratedFiles struct {
	FRRConfig   string
	SetupScript string // empty for transit nodes
}

// setupVRF is a wrapper around VRFState with an additional TableID field
// needed by the setup script template.
type setupVRF struct {
	*VRFState
	TableID int
}

// nodeSetupData is the template data for the setup script, extending NodeState
// with sorted VRFs that include table IDs.
type nodeSetupData struct {
	*NodeState
	SortedVRFs []namedSetupVRF
}

type namedSetupVRF struct {
	Name string
	setupVRF
}

// sortedVRFData extends NodeState with sorted VRFs for the FRR template.
type sortedVRFData struct {
	*NodeState
	VRFs []namedVRF
}

type namedVRF struct {
	Name string
	*VRFState
}

// Generate produces FRR configurations and setup scripts for all matched nodes
// in the topology state.
func Generate(state *TopologyState) (map[string]GeneratedFiles, error) {
	edgeLeafTmpl, err := template.ParseFS(templateFS, "templates/edge-leaf.frr.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing edge-leaf template: %w", err)
	}

	transitTmpl, err := template.ParseFS(templateFS, "templates/transit.frr.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing transit template: %w", err)
	}

	setupTmpl, err := template.ParseFS(templateFS, "templates/setup.sh.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parsing setup script template: %w", err)
	}

	result := make(map[string]GeneratedFiles)

	for nodeName, ns := range state.Nodes {
		var files GeneratedFiles

		switch ns.Role {
		case "edge-leaf":
			frrData := buildSortedVRFData(ns)
			frr, execErr := executeTemplate(edgeLeafTmpl, frrData)
			if execErr != nil {
				return nil, fmt.Errorf("generating FRR config for %s: %w", nodeName, execErr)
			}
			files.FRRConfig = frr

			setupData := buildSetupData(ns)
			setup, execErr := executeTemplate(setupTmpl, setupData)
			if execErr != nil {
				return nil, fmt.Errorf("generating setup script for %s: %w", nodeName, execErr)
			}
			files.SetupScript = setup

		case "transit":
			frr, execErr := executeTemplate(transitTmpl, ns)
			if execErr != nil {
				return nil, fmt.Errorf("generating FRR config for %s: %w", nodeName, execErr)
			}
			files.FRRConfig = frr

		default:
			continue
		}

		result[nodeName] = files
	}

	return result, nil
}

func buildSortedVRFData(ns *NodeState) *sortedVRFData {
	data := &sortedVRFData{
		NodeState: ns,
	}

	names := make([]string, 0, len(ns.VRFs))
	for name := range ns.VRFs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		data.VRFs = append(data.VRFs, namedVRF{
			Name:     name,
			VRFState: ns.VRFs[name],
		})
	}

	return data
}

func buildSetupData(ns *NodeState) *nodeSetupData {
	data := &nodeSetupData{
		NodeState: ns,
	}

	names := make([]string, 0, len(ns.VRFs))
	for name := range ns.VRFs {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		data.SortedVRFs = append(data.SortedVRFs, namedSetupVRF{
			Name: name,
			setupVRF: setupVRF{
				VRFState: ns.VRFs[name],
				TableID:  1100 + i,
			},
		})
	}

	return data
}

func executeTemplate(tmpl *template.Template, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
