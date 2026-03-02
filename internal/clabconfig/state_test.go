// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveLoadStateRoundTrip(t *testing.T) {
	original := &TopologyState{
		InputHash:    "abc123",
		TopologyName: "test-topo",
		Nodes: map[string]*NodeState{
			"leaf1": {
				Name:           "leaf1",
				MatchedPattern: "leaf*",
				Role:           "leaf",
				RouterID:       "10.0.0.1",
				VTEPIP:         "10.0.1.1",
				Interfaces: map[string]*InterfaceState{
					"eth1": {
						Name:          "eth1",
						PeerNode:      "spine1",
						PeerInterface: "eth1",
						IPv4:          "10.0.0.1/31",
						IPv6:          "fd00::1/127",
						LinkType:      "p2p",
					},
				},
				VRFs: map[string]*VRFState{
					"tenant1": {
						Name:                  "tenant1",
						VNI:                   100,
						Interfaces:            []string{"vxlan100"},
						RedistributeConnected: true,
						MACAddress:            "aa:bb:cc:dd:ee:ff",
						BridgeID:              100,
					},
				},
				BGP: &BGPState{
					ASN: 65001,
					Peers: []BGPPeerState{
						{
							NodeName:    "spine1",
							ASN:         65000,
							IPv4Address: "10.0.0.0",
							IPv6Address: "fd00::",
							EVPNEnabled: true,
							BFDEnabled:  true,
						},
					},
				},
			},
		},
		Links: []LinkState{
			{
				NodeA:      "leaf1",
				InterfaceA: "eth1",
				NodeB:      "spine1",
				InterfaceB: "eth1",
				IPv4Subnet: "10.0.0.0/31",
				IPv6Subnet: "fd00::/127",
				Type:       "p2p",
			},
		},
		BroadcastNetworks: []BroadcastNetwork{
			{
				SwitchName: "br-mgmt",
				IPv4Subnet: "192.168.1.0/24",
				IPv6Subnet: "fd01::/64",
				Members: []BroadcastMember{
					{
						NodeName:      "leaf1",
						InterfaceName: "eth0",
						IPv4:          "192.168.1.1",
						IPv6:          "fd01::1",
					},
				},
			},
		},
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	if err := original.SaveState(statePath); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify the file was created.
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file not found after SaveState: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if !reflect.DeepEqual(original, loaded) {
		t.Fatalf("round-trip mismatch:\noriginal: %+v\nloaded:   %+v", original, loaded)
	}
}

func TestSaveStateInvalidPath(t *testing.T) {
	state := &TopologyState{TopologyName: "test"}
	err := state.SaveState("/nonexistent/directory/state.json")
	if err == nil {
		t.Fatal("expected error when saving to invalid path, got nil")
	}
}

func TestLoadStateFileNotFound(t *testing.T) {
	_, err := LoadState("/nonexistent/file.json")
	if err == nil {
		t.Fatal("expected error when loading nonexistent file, got nil")
	}
}

func TestLoadStateInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	badFile := filepath.Join(tmpDir, "bad.json")
	if err := os.WriteFile(badFile, []byte("not json"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadState(badFile)
	if err == nil {
		t.Fatal("expected error when loading invalid JSON, got nil")
	}
}

func TestSaveLoadEmptyState(t *testing.T) {
	original := &TopologyState{
		TopologyName: "empty",
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "empty.json")

	if err := original.SaveState(statePath); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if !reflect.DeepEqual(original, loaded) {
		t.Fatalf("round-trip mismatch for empty state:\noriginal: %+v\nloaded:   %+v", original, loaded)
	}
}

func TestSummary(t *testing.T) {
	state := &TopologyState{
		TopologyName: "dc-fabric",
		Nodes: map[string]*NodeState{
			"leaf1": {
				Name:     "leaf1",
				Role:     "edge-leaf",
				RouterID: "10.0.0.1",
				VTEPIP:   "10.0.1.1",
				Interfaces: map[string]*InterfaceState{
					"eth1": {
						Name:          "eth1",
						PeerNode:      "spine1",
						PeerInterface: "eth1",
						IPv4:          "10.0.0.1/31",
						IPv6:          "fd00::1/127",
						LinkType:      "p2p",
					},
					"eth2": {
						Name:          "eth2",
						PeerNode:      "spine2",
						PeerInterface: "eth1",
						IPv4:          "10.0.0.3/31",
						LinkType:      "p2p",
					},
				},
				VRFs: map[string]*VRFState{
					"tenant1": {
						Name: "tenant1",
						VNI:  100,
					},
					"tenant2": {
						Name: "tenant2",
						VNI:  200,
					},
				},
				BGP: &BGPState{
					ASN: 65001,
					Peers: []BGPPeerState{
						{
							NodeName:    "spine1",
							ASN:         65000,
							IPv4Address: "10.0.0.0",
							IPv6Address: "fd00::",
						},
					},
				},
			},
			"spine1": {
				Name:     "spine1",
				Role:     "spine",
				RouterID: "10.0.0.100",
				Interfaces: map[string]*InterfaceState{
					"eth1": {
						Name:          "eth1",
						PeerNode:      "leaf1",
						PeerInterface: "eth1",
						IPv4:          "10.0.0.0/31",
						IPv6:          "fd00::/127",
						LinkType:      "p2p",
					},
				},
				BGP: &BGPState{
					ASN: 65000,
					Peers: []BGPPeerState{
						{
							NodeName:    "leaf1",
							ASN:         65001,
							IPv4Address: "10.0.0.1",
						},
					},
				},
			},
		},
		Links: []LinkState{
			{
				NodeA:      "leaf1",
				InterfaceA: "eth1",
				NodeB:      "spine1",
				InterfaceB: "eth1",
				IPv4Subnet: "10.0.0.0/31",
				IPv6Subnet: "fd00::/127",
				Type:       "p2p",
			},
		},
		BroadcastNetworks: []BroadcastNetwork{
			{
				SwitchName: "br-mgmt",
				IPv4Subnet: "192.168.1.0/24",
			},
		},
	}

	summary := state.Summary()

	// Check topology overview section
	expectedSubstrings := []string{
		"=== Topology Summary ===",
		"Name:               dc-fabric",
		"Nodes:              2",
		"Links:              1",
		"Broadcast Networks: 1",
		// Node details section
		"=== Node Details ===",
		// leaf1 details (should appear before spine1 due to sorting)
		"--- leaf1 ---",
		"Role:      edge-leaf",
		"Router ID: 10.0.0.1",
		"VTEP IP:   10.0.1.1",
		// leaf1 interfaces
		"eth1 -> spine1:eth1",
		"IPv4: 10.0.0.1/31",
		"IPv6: fd00::1/127",
		"eth2 -> spine2:eth1",
		"IPv4: 10.0.0.3/31",
		// leaf1 VRFs
		"tenant1 (VNI: 100)",
		"tenant2 (VNI: 200)",
		// leaf1 BGP
		"BGP (ASN: 65001)",
		"Peer: spine1 (ASN: 65000) IPv4: 10.0.0.0",
		// spine1 details
		"--- spine1 ---",
		"Role:      spine",
		"Router ID: 10.0.0.100",
		"BGP (ASN: 65000)",
		"Peer: leaf1 (ASN: 65001) IPv4: 10.0.0.1",
		// Resource allocation
		"=== Resource Allocation ===",
		// leaf1: eth1 has IPv4+IPv6 (2), eth2 has IPv4 (1); spine1: eth1 has IPv4+IPv6 (2) = 5
		"Total IPs allocated: 5",
	}

	for _, expected := range expectedSubstrings {
		if !strings.Contains(summary, expected) {
			t.Errorf("summary missing expected substring: %q\n\nFull summary:\n%s", expected, summary)
		}
	}

	// Verify spine1 does NOT have VTEP IP line (since it's empty)
	lines := strings.Split(summary, "\n")
	inSpine := false
	for _, line := range lines {
		if strings.Contains(line, "--- spine1 ---") {
			inSpine = true
		}
		if inSpine && strings.Contains(line, "VTEP IP") {
			t.Error("spine1 should not have VTEP IP in summary")
		}
		if inSpine && strings.Contains(line, "--- leaf1 ---") {
			// We've gone past spine1 section (but leaf1 comes first alphabetically, so this won't happen)
			break
		}
	}
}

// testTopology returns a TopologyState used across query method tests.
func testTopology() *TopologyState {
	return &TopologyState{
		TopologyName: "test-fabric",
		Nodes: map[string]*NodeState{
			"leaf1": {
				Name:     "leaf1",
				Role:     "edge-leaf",
				RouterID: "10.0.0.1",
				VTEPIP:   "10.0.1.1",
				Interfaces: map[string]*InterfaceState{
					"eth1": {
						Name:          "eth1",
						PeerNode:      "spine1",
						PeerInterface: "eth1",
						IPv4:          "10.0.0.1/31",
						IPv6:          "fd00::1/127",
						LinkType:      "p2p",
					},
				},
			},
			"leaf2": {
				Name:     "leaf2",
				Role:     "edge-leaf",
				RouterID: "10.0.0.2",
				VTEPIP:   "10.0.1.2",
				Interfaces: map[string]*InterfaceState{
					"eth1": {
						Name:          "eth1",
						PeerNode:      "spine1",
						PeerInterface: "eth2",
						IPv4:          "10.0.0.3/31",
						IPv6:          "fd00::3/127",
						LinkType:      "p2p",
					},
				},
			},
			"spine1": {
				Name:     "spine1",
				Role:     "spine",
				RouterID: "10.0.0.100",
				Interfaces: map[string]*InterfaceState{
					"eth1": {
						Name:          "eth1",
						PeerNode:      "leaf1",
						PeerInterface: "eth1",
						IPv4:          "10.0.0.0/31",
						IPv6:          "fd00::/127",
						LinkType:      "p2p",
					},
					"eth2": {
						Name:          "eth2",
						PeerNode:      "leaf2",
						PeerInterface: "eth1",
						IPv4:          "10.0.0.2/31",
						IPv6:          "fd00::2/127",
						LinkType:      "p2p",
					},
				},
			},
			"spine2": {
				Name:     "spine2",
				Role:     "spine",
				RouterID: "10.0.0.101",
				Interfaces: map[string]*InterfaceState{
					"eth1": {
						Name:          "eth1",
						PeerNode:      "leaf1",
						PeerInterface: "eth2",
						IPv4:          "10.0.0.4/31",
						LinkType:      "p2p",
					},
				},
			},
		},
	}
}

func TestGetNodeVTEP(t *testing.T) {
	state := testTopology()

	tests := []struct {
		name      string
		nodeName  string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "found leaf with VTEP",
			nodeName: "leaf1",
			want:     "10.0.1.1",
		},
		{
			name:     "found another leaf with VTEP",
			nodeName: "leaf2",
			want:     "10.0.1.2",
		},
		{
			name:      "spine without VTEP",
			nodeName:  "spine1",
			wantErr:   true,
			errSubstr: "does not have a VTEP IP",
		},
		{
			name:      "node not found",
			nodeName:  "nonexistent",
			wantErr:   true,
			errSubstr: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := state.GetNodeVTEP(tt.nodeName)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetLinkIP(t *testing.T) {
	state := testTopology()

	tests := []struct {
		name      string
		nodeName  string
		peerName  string
		family    IPFamily
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "IPv4 leaf1 facing spine1",
			nodeName: "leaf1",
			peerName: "spine1",
			family:   IPv4,
			want:     "10.0.0.1/31",
		},
		{
			name:     "IPv6 leaf1 facing spine1",
			nodeName: "leaf1",
			peerName: "spine1",
			family:   IPv6,
			want:     "fd00::1/127",
		},
		{
			name:     "IPv4 spine1 facing leaf2",
			nodeName: "spine1",
			peerName: "leaf2",
			family:   IPv4,
			want:     "10.0.0.2/31",
		},
		{
			name:      "no IPv6 on spine2 facing leaf1",
			nodeName:  "spine2",
			peerName:  "leaf1",
			family:    IPv6,
			wantErr:   true,
			errSubstr: "no IPv6 address",
		},
		{
			name:      "node not found",
			nodeName:  "nonexistent",
			peerName:  "spine1",
			family:    IPv4,
			wantErr:   true,
			errSubstr: "not found",
		},
		{
			name:      "peer not found",
			nodeName:  "leaf1",
			peerName:  "nonexistent",
			family:    IPv4,
			wantErr:   true,
			errSubstr: "no interface",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := state.GetLinkIP(tt.nodeName, tt.peerName, tt.family)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindIPOwner(t *testing.T) {
	state := testTopology()

	tests := []struct {
		name      string
		ip        string
		wantNode  string
		wantIface string
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "find by IPv4",
			ip:        "10.0.0.1",
			wantNode:  "leaf1",
			wantIface: "eth1",
		},
		{
			name:      "find by IPv6",
			ip:        "fd00::3",
			wantNode:  "leaf2",
			wantIface: "eth1",
		},
		{
			name:      "not found",
			ip:        "192.168.99.99",
			wantErr:   true,
			errSubstr: "no interface found",
		},
		{
			name:      "empty string matches everything - finds something",
			ip:        "10.0.0.0",
			wantNode:  "spine1",
			wantIface: "eth1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, iface, err := state.FindIPOwner(tt.ip)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if node != tt.wantNode {
				t.Fatalf("got node %q, want %q", node, tt.wantNode)
			}
			if iface != tt.wantIface {
				t.Fatalf("got iface %q, want %q", iface, tt.wantIface)
			}
		})
	}
}

func TestGetNodesByPattern(t *testing.T) {
	state := testTopology()

	tests := []struct {
		name      string
		pattern   string
		want      []string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "match all leaves",
			pattern: "^leaf",
			want:    []string{"leaf1", "leaf2"},
		},
		{
			name:    "match all spines",
			pattern: "^spine",
			want:    []string{"spine1", "spine2"},
		},
		{
			name:    "match all nodes",
			pattern: ".*",
			want:    []string{"leaf1", "leaf2", "spine1", "spine2"},
		},
		{
			name:    "match specific node",
			pattern: "^leaf1$",
			want:    []string{"leaf1"},
		},
		{
			name:    "no matches",
			pattern: "^border",
			want:    nil,
		},
		{
			name:      "invalid regex",
			pattern:   "[invalid",
			wantErr:   true,
			errSubstr: "invalid pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := state.GetNodesByPattern(tt.pattern)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
