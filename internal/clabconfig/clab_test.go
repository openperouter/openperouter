// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClab(t *testing.T) {
	const validYAML = `name: test-topology
topology:
  nodes:
    node1:
      kind: linux
      image: alpine:latest
      binds:
        - /tmp/config:/config
    node2:
      kind: bridge
  links:
    - endpoints:
        - node1:eth1
        - node2:eth1
`

	tests := []struct {
		name      string
		yaml      string
		wantName  string
		wantNodes int
		wantLinks int
		wantErr   bool
	}{
		{
			name:      "valid topology",
			yaml:      validYAML,
			wantName:  "test-topology",
			wantNodes: 2,
			wantLinks: 1,
		},
		{
			name:    "invalid yaml",
			yaml:    ":\n  :\n  - :\n\t{invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			path := filepath.Join(tmpDir, "topo.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write temp file: %v", err)
			}

			topo, err := LoadClab(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if topo.Name != tt.wantName {
				t.Errorf("name = %q, want %q", topo.Name, tt.wantName)
			}
			if got := len(topo.Topology.Nodes); got != tt.wantNodes {
				t.Errorf("nodes count = %d, want %d", got, tt.wantNodes)
			}
			if got := len(topo.Topology.Links); got != tt.wantLinks {
				t.Errorf("links count = %d, want %d", got, tt.wantLinks)
			}
		})
	}
}

func TestLoadClabFileNotFound(t *testing.T) {
	_, err := LoadClab("/nonexistent/path/topo.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestParseEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []string
		wantNode0 string
		wantIf0   string
		wantNode1 string
		wantIf1   string
		wantErr   bool
	}{
		{
			name:      "valid endpoints",
			endpoints: []string{"node1:eth0", "node2:eth1"},
			wantNode0: "node1",
			wantIf0:   "eth0",
			wantNode1: "node2",
			wantIf1:   "eth1",
		},
		{
			name:      "endpoint with colon in interface",
			endpoints: []string{"node1:eth0:1", "node2:eth1"},
			wantNode0: "node1",
			wantIf0:   "eth0:1",
			wantNode1: "node2",
			wantIf1:   "eth1",
		},
		{
			name:      "wrong number of endpoints",
			endpoints: []string{"node1:eth0"},
			wantErr:   true,
		},
		{
			name:      "empty endpoints",
			endpoints: []string{},
			wantErr:   true,
		},
		{
			name:      "missing colon separator",
			endpoints: []string{"node1eth0", "node2:eth1"},
			wantErr:   true,
		},
		{
			name:      "empty node name",
			endpoints: []string{":eth0", "node2:eth1"},
			wantErr:   true,
		},
		{
			name:      "empty interface name",
			endpoints: []string{"node1:", "node2:eth1"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := ClabLink{Endpoints: tt.endpoints}
			result, err := link.ParseEndpoints()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result[0].Node != tt.wantNode0 {
				t.Errorf("endpoint[0].Node = %q, want %q", result[0].Node, tt.wantNode0)
			}
			if result[0].Interface != tt.wantIf0 {
				t.Errorf("endpoint[0].Interface = %q, want %q", result[0].Interface, tt.wantIf0)
			}
			if result[1].Node != tt.wantNode1 {
				t.Errorf("endpoint[1].Node = %q, want %q", result[1].Node, tt.wantNode1)
			}
			if result[1].Interface != tt.wantIf1 {
				t.Errorf("endpoint[1].Interface = %q, want %q", result[1].Interface, tt.wantIf1)
			}
		})
	}
}
