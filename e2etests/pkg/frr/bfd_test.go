// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"testing"
)

func TestBFDSessions(t *testing.T) {
	jsonData := `[
		{
			"peer": "192.168.1.4",
			"status": "Up"
		},
		{
			"peer": "192.168.1.5",
			"status": "Down"
		}
	]`

	var sessions []BFDPeer
	err := json.Unmarshal([]byte(jsonData), &sessions)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("Expected 2 sessions, got %d", len(sessions))
	}

	if sessions[0].Peer != "192.168.1.4" {
		t.Errorf("Expected first peer to be 192.168.1.4, got %s", sessions[0].Peer)
	}

	if sessions[0].Status != "Up" {
		t.Errorf("Expected first status to be Up, got %s", sessions[0].Status)
	}

	if sessions[1].Peer != "192.168.1.5" {
		t.Errorf("Expected second peer to be 192.168.1.5, got %s", sessions[1].Peer)
	}

	if sessions[1].Status != "Down" {
		t.Errorf("Expected second status to be Down, got %s", sessions[1].Status)
	}
}
