// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata")
}

func loadTestFixtures(t *testing.T) (*ClabTopology, *EnvironmentConfig) {
	t.Helper()
	dir := testdataDir()
	clab, err := LoadClab(filepath.Join(dir, "basic.clab.yml"))
	if err != nil {
		t.Fatalf("failed to load clab fixture: %v", err)
	}
	config, err := LoadConfig(filepath.Join(dir, "basic-config.yaml"))
	if err != nil {
		t.Fatalf("failed to load config fixture: %v", err)
	}
	return clab, config
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name            string
		modifyClab      func(*ClabTopology)
		modifyConfig    func(*EnvironmentConfig)
		wantErr         string
		wantWarnings    []string
		wantNoWarnings  bool
	}{
		{
			name: "valid config from fixtures",
			wantWarnings: []string{
				`node "hostA_red" does not match any config pattern`,
				`node "hostB_red" does not match any config pattern`,
			},
		},
		{
			name: "overlapping patterns",
			modifyConfig: func(config *EnvironmentConfig) {
				config.Nodes = append(config.Nodes, NodeConfig{
					Pattern: "leaf.*",
					Role:    "edge-leaf",
					BGP:     BGPConfig{ASN: 65099},
				})
			},
			wantErr: "matches multiple patterns",
		},
		{
			name: "invalid regex in node pattern",
			modifyConfig: func(config *EnvironmentConfig) {
				config.Nodes[0].Pattern = "leaf[invalid"
			},
			wantErr: "invalid node pattern",
		},
		{
			name: "invalid regex in peer pattern",
			modifyConfig: func(config *EnvironmentConfig) {
				config.Nodes[0].BGP.Peers[0].Pattern = "spine[bad"
			},
			wantErr: "invalid peer pattern",
		},
		{
			name: "interface not in topology",
			modifyConfig: func(config *EnvironmentConfig) {
				vrf := config.Nodes[0].VRFs["red"]
				vrf.Interfaces = []string{"ethred", "nonexistent"}
				config.Nodes[0].VRFs["red"] = vrf
			},
			wantErr: `interface "nonexistent"`,
		},
		{
			name: "duplicate VNI",
			modifyConfig: func(config *EnvironmentConfig) {
				// Add a VRF to leafkind with the same VNI as the leaf[AB] red VRF.
				config.Nodes[2].Role = "edge-leaf" // change from transit so VRFs are allowed
				config.Nodes[2].VRFs = map[string]VRFConfig{
					"blue": {
						Interfaces: []string{"toswitch"},
						VNI:        100,
					},
				}
			},
			wantErr: "duplicate VNI 100",
		},
		{
			name: "transit node with VRFs",
			modifyConfig: func(config *EnvironmentConfig) {
				config.Nodes[1].VRFs = map[string]VRFConfig{
					"bad": {VNI: 999, Interfaces: []string{"eth1"}},
				}
			},
			wantErr: "transit node pattern",
		},
		{
			name: "invalid CIDR in IP ranges",
			modifyConfig: func(config *EnvironmentConfig) {
				config.IPRanges.VTEP = "not-a-cidr"
			},
			wantErr: "invalid CIDR",
		},
		{
			name: "unmatched pattern warning",
			modifyConfig: func(config *EnvironmentConfig) {
				config.Nodes = append(config.Nodes, NodeConfig{
					Pattern: "nonexistent-node",
					Role:    "edge-leaf",
					BGP:     BGPConfig{ASN: 65099},
				})
			},
			wantWarnings: []string{
				`config pattern "nonexistent-node" does not match any clab node`,
			},
		},
		{
			name: "bridge nodes are skipped for overlap detection",
			modifyClab: func(clab *ClabTopology) {
				// Add a bridge node whose name matches leaf pattern - should not cause overlap.
				clab.Topology.Nodes["leafBridge"] = ClabNode{Kind: "bridge"}
			},
			// No error expected - bridges are skipped.
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clab, config := loadTestFixtures(t)

			if tt.modifyClab != nil {
				tt.modifyClab(clab)
			}
			if tt.modifyConfig != nil {
				tt.modifyConfig(config)
			}

			warnings, err := Validate(clab, config)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, want := range tt.wantWarnings {
				found := false
				for _, w := range warnings {
					if strings.Contains(w, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected warning containing %q, got warnings: %v", want, warnings)
				}
			}

			if tt.wantNoWarnings && len(warnings) > 0 {
				t.Errorf("expected no warnings, got: %v", warnings)
			}
		})
	}
}
