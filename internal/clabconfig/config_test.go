// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	validYAML := `
ipRanges:
  pointToPoint:
    ipv4: "192.168.0.0/24"
    ipv6: "fd00::/64"
  broadcast:
    ipv4: "10.0.0.0/24"
    ipv6: "fd01::/64"
  vtep: "172.16.0.0/24"
  routerID: "1.1.1.0/24"
nodes:
  - pattern: "leaf-.*"
    role: "edge-leaf"
    evpnEnabled: true
    vrfs:
      tenant1:
        redistributeConnected: true
        interfaces:
          - "eth1"
          - "eth2"
        vni: 100
    bgp:
      asn: 65001
      peers:
        - pattern: "spine-.*"
          evpnEnabled: true
          bfdEnabled: true
  - pattern: "spine-.*"
    role: "transit"
    bgp:
      asn: 65000
      peers:
        - pattern: "leaf-.*"
          evpnEnabled: false
          bfdEnabled: false
`

	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		wantErr  bool
		validate func(t *testing.T, cfg *EnvironmentConfig)
	}{
		{
			name: "valid config",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				path := filepath.Join(dir, "config.yaml")
				if err := os.WriteFile(path, []byte(validYAML), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
				return path
			},
			wantErr: false,
			validate: func(t *testing.T, cfg *EnvironmentConfig) {
				t.Helper()
				if cfg.IPRanges.PointToPoint.IPv4 != "192.168.0.0/24" {
					t.Errorf("unexpected pointToPoint IPv4: got %s, want 192.168.0.0/24", cfg.IPRanges.PointToPoint.IPv4)
				}
				if cfg.IPRanges.PointToPoint.IPv6 != "fd00::/64" {
					t.Errorf("unexpected pointToPoint IPv6: got %s, want fd00::/64", cfg.IPRanges.PointToPoint.IPv6)
				}
				if cfg.IPRanges.Broadcast.IPv4 != "10.0.0.0/24" {
					t.Errorf("unexpected broadcast IPv4: got %s, want 10.0.0.0/24", cfg.IPRanges.Broadcast.IPv4)
				}
				if cfg.IPRanges.VTEP != "172.16.0.0/24" {
					t.Errorf("unexpected VTEP: got %s, want 172.16.0.0/24", cfg.IPRanges.VTEP)
				}
				if cfg.IPRanges.RouterID != "1.1.1.0/24" {
					t.Errorf("unexpected routerID: got %s, want 1.1.1.0/24", cfg.IPRanges.RouterID)
				}
				if len(cfg.Nodes) != 2 {
					t.Fatalf("unexpected number of nodes: got %d, want 2", len(cfg.Nodes))
				}

				leaf := cfg.Nodes[0]
				if leaf.Pattern != "leaf-.*" {
					t.Errorf("unexpected node pattern: got %s, want leaf-.*", leaf.Pattern)
				}
				if leaf.Role != "edge-leaf" {
					t.Errorf("unexpected node role: got %s, want edge-leaf", leaf.Role)
				}
				if !leaf.EVPNEnabled {
					t.Error("expected evpnEnabled to be true for leaf node")
				}
				if len(leaf.VRFs) != 1 {
					t.Fatalf("unexpected number of VRFs: got %d, want 1", len(leaf.VRFs))
				}
				vrf, ok := leaf.VRFs["tenant1"]
				if !ok {
					t.Fatal("expected VRF tenant1 to exist")
				}
				if !vrf.RedistributeConnected {
					t.Error("expected redistributeConnected to be true")
				}
				if len(vrf.Interfaces) != 2 {
					t.Errorf("unexpected number of interfaces: got %d, want 2", len(vrf.Interfaces))
				}
				if vrf.VNI != 100 {
					t.Errorf("unexpected VNI: got %d, want 100", vrf.VNI)
				}
				if leaf.BGP.ASN != 65001 {
					t.Errorf("unexpected ASN: got %d, want 65001", leaf.BGP.ASN)
				}
				if len(leaf.BGP.Peers) != 1 {
					t.Fatalf("unexpected number of peers: got %d, want 1", len(leaf.BGP.Peers))
				}
				if !leaf.BGP.Peers[0].EVPNEnabled {
					t.Error("expected peer evpnEnabled to be true")
				}
				if !leaf.BGP.Peers[0].BFDEnabled {
					t.Error("expected peer bfdEnabled to be true")
				}

				spine := cfg.Nodes[1]
				if spine.Role != "transit" {
					t.Errorf("unexpected node role: got %s, want transit", spine.Role)
				}
				if spine.EVPNEnabled {
					t.Error("expected evpnEnabled to default to false for spine node")
				}
				if spine.VRFs != nil {
					t.Error("expected VRFs to be nil for spine node")
				}
			},
		},
		{
			name: "invalid YAML",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				path := filepath.Join(dir, "invalid.yaml")
				content := "invalid: yaml: [unterminated"
				if err := os.WriteFile(path, []byte(content), 0644); err != nil {
					t.Fatalf("failed to write test file: %v", err)
				}
				return path
			},
			wantErr: true,
		},
		{
			name: "file not found",
			setup: func(t *testing.T) string {
				return "/nonexistent/path/config.yaml"
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)
			cfg, err := LoadConfig(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.validate != nil && err == nil {
				tt.validate(t, cfg)
			}
		})
	}
}
