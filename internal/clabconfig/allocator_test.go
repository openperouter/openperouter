// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"encoding/json"
	"testing"
)

func TestAllocate_BasicTopology(t *testing.T) {
	clab, err := LoadClab("testdata/basic.clab.yml")
	if err != nil {
		t.Fatalf("failed to load clab topology: %v", err)
	}
	config, err := LoadConfig("testdata/basic-config.yaml")
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	state, warnings, err := Allocate(clab, config)
	if err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}

	// Log warnings for visibility.
	for _, w := range warnings {
		t.Logf("warning: %s", w)
	}

	// Verify node roles.
	expectedRoles := map[string]string{
		"leafA":    "edge-leaf",
		"leafB":    "edge-leaf",
		"spine":    "transit",
		"leafkind": "transit",
	}
	for name, expectedRole := range expectedRoles {
		ns, ok := state.Nodes[name]
		if !ok {
			t.Errorf("expected node %q in state", name)
			continue
		}
		if ns.Role != expectedRole {
			t.Errorf("node %q: expected role %q, got %q", name, expectedRole, ns.Role)
		}
	}

	// Verify all matched nodes have router IDs.
	for name, ns := range state.Nodes {
		if ns.RouterID == "" {
			t.Errorf("node %q has no router ID", name)
		}
	}

	// Verify only edge-leaf nodes have VTEP IPs.
	for name, ns := range state.Nodes {
		if ns.Role == "edge-leaf" {
			if ns.VTEPIP == "" {
				t.Errorf("edge-leaf node %q has no VTEP IP", name)
			}
		} else {
			if ns.VTEPIP != "" {
				t.Errorf("non-edge-leaf node %q should not have VTEP IP, got %q", name, ns.VTEPIP)
			}
		}
	}

	// Verify links have allocated addresses.
	if len(state.Links) == 0 {
		t.Error("expected at least one link in state")
	}
	for i, link := range state.Links {
		if link.IPv4Subnet == "" {
			t.Errorf("link %d has no IPv4 subnet", i)
		}
		if link.IPv6Subnet == "" {
			t.Errorf("link %d has no IPv6 subnet", i)
		}
	}

	// Verify BGP peers are resolved for leafA.
	leafA := state.Nodes["leafA"]
	if leafA.BGP == nil {
		t.Fatal("leafA has no BGP state")
	}
	if leafA.BGP.ASN != 64520 {
		t.Errorf("leafA BGP ASN: expected 64520, got %d", leafA.BGP.ASN)
	}
	foundSpinePeer := false
	for _, peer := range leafA.BGP.Peers {
		if peer.NodeName == "spine" {
			foundSpinePeer = true
			if peer.IPv4Address == "" {
				t.Error("leafA BGP peer spine has no IPv4 address")
			}
			if peer.IPv6Address == "" {
				t.Error("leafA BGP peer spine has no IPv6 address")
			}
			if !peer.EVPNEnabled {
				t.Error("leafA BGP peer spine should have EVPN enabled")
			}
			if !peer.BFDEnabled {
				t.Error("leafA BGP peer spine should have BFD enabled")
			}
		}
	}
	if !foundSpinePeer {
		t.Error("leafA should have spine as a BGP peer")
	}

	// Verify spine has leaf peers.
	spineNode := state.Nodes["spine"]
	if spineNode.BGP == nil {
		t.Fatal("spine has no BGP state")
	}
	if len(spineNode.BGP.Peers) < 2 {
		t.Errorf("spine should have at least 2 BGP peers (leafA, leafB, leafkind), got %d", len(spineNode.BGP.Peers))
	}

	// Verify VRFs have MAC addresses on edge-leaf nodes.
	for _, name := range []string{"leafA", "leafB"} {
		ns := state.Nodes[name]
		if len(ns.VRFs) == 0 {
			t.Errorf("edge-leaf node %q should have VRFs", name)
			continue
		}
		for vrfName, vrf := range ns.VRFs {
			if vrf.MACAddress == "" {
				t.Errorf("node %q VRF %q has no MAC address", name, vrfName)
			}
		}
	}

	// Verify idempotency: call Allocate again and compare.
	state2, _, err := Allocate(clab, config)
	if err != nil {
		t.Fatalf("second Allocate failed: %v", err)
	}

	json1, _ := json.Marshal(state)
	json2, _ := json.Marshal(state2)
	if string(json1) != string(json2) {
		t.Error("Allocate is not idempotent: two calls produced different results")
	}
}

func TestAllocateP2PIPs_Deterministic(t *testing.T) {
	clab, err := LoadClab("testdata/basic.clab.yml")
	if err != nil {
		t.Fatalf("failed to load clab topology: %v", err)
	}
	config, err := LoadConfig("testdata/basic-config.yaml")
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	state1, _, err := Allocate(clab, config)
	if err != nil {
		t.Fatalf("first Allocate failed: %v", err)
	}
	state2, _, err := Allocate(clab, config)
	if err != nil {
		t.Fatalf("second Allocate failed: %v", err)
	}

	// Verify links are in the same order.
	if len(state1.Links) != len(state2.Links) {
		t.Fatalf("link count differs: %d vs %d", len(state1.Links), len(state2.Links))
	}

	for i := range state1.Links {
		l1 := state1.Links[i]
		l2 := state2.Links[i]
		if l1.NodeA != l2.NodeA || l1.NodeB != l2.NodeB {
			t.Errorf("link %d: nodes differ (%s-%s vs %s-%s)",
				i, l1.NodeA, l1.NodeB, l2.NodeA, l2.NodeB)
		}
		if l1.IPv4Subnet != l2.IPv4Subnet {
			t.Errorf("link %d: IPv4 subnets differ (%s vs %s)", i, l1.IPv4Subnet, l2.IPv4Subnet)
		}
		if l1.IPv6Subnet != l2.IPv6Subnet {
			t.Errorf("link %d: IPv6 subnets differ (%s vs %s)", i, l1.IPv6Subnet, l2.IPv6Subnet)
		}
	}

	// Verify P2P links are sorted alphabetically by (min, max, ifaceA).
	for i := 1; i < len(state1.Links); i++ {
		prev := state1.Links[i-1]
		curr := state1.Links[i]
		if prev.Type != "point-to-point" || curr.Type != "point-to-point" {
			continue
		}
		prevMin, prevMax := prev.NodeA, prev.NodeB
		if prevMin > prevMax {
			prevMin, prevMax = prevMax, prevMin
		}
		currMin, currMax := curr.NodeA, curr.NodeB
		if currMin > currMax {
			currMin, currMax = currMax, currMin
		}
		key1 := prevMin + "|" + prevMax + "|" + prev.InterfaceA
		key2 := currMin + "|" + currMax + "|" + curr.InterfaceA
		if key1 > key2 {
			t.Errorf("links not sorted: link %d (%s) > link %d (%s)", i-1, key1, i, key2)
		}
	}
}

func TestAllocate_AlternateConfig(t *testing.T) {
	clab, err := LoadClab("testdata/basic.clab.yml")
	if err != nil {
		t.Fatalf("failed to load clab topology: %v", err)
	}

	basicConfig, err := LoadConfig("testdata/basic-config.yaml")
	if err != nil {
		t.Fatalf("failed to load basic config: %v", err)
	}
	altConfig, err := LoadConfig("testdata/alternate-config.yaml")
	if err != nil {
		t.Fatalf("failed to load alternate config: %v", err)
	}

	basicState, _, err := Allocate(clab, basicConfig)
	if err != nil {
		t.Fatalf("Allocate with basic config failed: %v", err)
	}
	altState, altWarnings, err := Allocate(clab, altConfig)
	if err != nil {
		t.Fatalf("Allocate with alternate config failed: %v", err)
	}

	for _, w := range altWarnings {
		t.Logf("alternate warning: %s", w)
	}

	// Both should produce the same set of matched nodes.
	for _, name := range []string{"leafA", "leafB", "spine", "leafkind"} {
		if _, ok := basicState.Nodes[name]; !ok {
			t.Errorf("basic state missing node %q", name)
		}
		if _, ok := altState.Nodes[name]; !ok {
			t.Errorf("alternate state missing node %q", name)
		}
	}

	// Verify distinct ASNs.
	if basicState.Nodes["leafA"].BGP.ASN == altState.Nodes["leafA"].BGP.ASN {
		t.Errorf("leafA ASN should differ: basic=%d, alt=%d",
			basicState.Nodes["leafA"].BGP.ASN, altState.Nodes["leafA"].BGP.ASN)
	}
	if basicState.Nodes["spine"].BGP.ASN == altState.Nodes["spine"].BGP.ASN {
		t.Errorf("spine ASN should differ: basic=%d, alt=%d",
			basicState.Nodes["spine"].BGP.ASN, altState.Nodes["spine"].BGP.ASN)
	}
	if altState.Nodes["leafA"].BGP.ASN != 65100 {
		t.Errorf("alternate leafA ASN: expected 65100, got %d", altState.Nodes["leafA"].BGP.ASN)
	}
	if altState.Nodes["spine"].BGP.ASN != 65200 {
		t.Errorf("alternate spine ASN: expected 65200, got %d", altState.Nodes["spine"].BGP.ASN)
	}
	if altState.Nodes["leafkind"].BGP.ASN != 65300 {
		t.Errorf("alternate leafkind ASN: expected 65300, got %d", altState.Nodes["leafkind"].BGP.ASN)
	}

	// Verify distinct VRF names: basic has "red", alternate has "green" and "yellow".
	for _, name := range []string{"leafA", "leafB"} {
		basicVRFs := basicState.Nodes[name].VRFs
		altVRFs := altState.Nodes[name].VRFs

		if _, ok := basicVRFs["red"]; !ok {
			t.Errorf("basic %s should have VRF 'red'", name)
		}
		if _, ok := altVRFs["green"]; !ok {
			t.Errorf("alternate %s should have VRF 'green'", name)
		}
		if _, ok := altVRFs["yellow"]; !ok {
			t.Errorf("alternate %s should have VRF 'yellow'", name)
		}
		if _, ok := altVRFs["red"]; ok {
			t.Errorf("alternate %s should NOT have VRF 'red'", name)
		}
	}

	// Verify distinct VNIs.
	altGreen := altState.Nodes["leafA"].VRFs["green"]
	altYellow := altState.Nodes["leafA"].VRFs["yellow"]
	if altGreen.VNI != 300 {
		t.Errorf("alternate leafA VRF green VNI: expected 300, got %d", altGreen.VNI)
	}
	if altYellow.VNI != 400 {
		t.Errorf("alternate leafA VRF yellow VNI: expected 400, got %d", altYellow.VNI)
	}

	// Verify distinct router IDs (different ranges: 10.0.0.x vs 10.254.0.x).
	for _, name := range []string{"leafA", "leafB", "spine", "leafkind"} {
		if basicState.Nodes[name].RouterID == altState.Nodes[name].RouterID {
			t.Errorf("node %q router IDs should differ: basic=%s, alt=%s",
				name, basicState.Nodes[name].RouterID, altState.Nodes[name].RouterID)
		}
	}

	// Verify distinct VTEP IPs (different ranges: 100.64.0.x vs 100.65.0.x).
	for _, name := range []string{"leafA", "leafB"} {
		if basicState.Nodes[name].VTEPIP == altState.Nodes[name].VTEPIP {
			t.Errorf("node %q VTEP IPs should differ: basic=%s, alt=%s",
				name, basicState.Nodes[name].VTEPIP, altState.Nodes[name].VTEPIP)
		}
	}

	// Verify distinct link subnets (different P2P ranges).
	if len(basicState.Links) > 0 && len(altState.Links) > 0 {
		if basicState.Links[0].IPv4Subnet == altState.Links[0].IPv4Subnet {
			t.Errorf("link IPv4 subnets should differ: basic=%s, alt=%s",
				basicState.Links[0].IPv4Subnet, altState.Links[0].IPv4Subnet)
		}
	}

	// Verify determinism: allocate alternate config twice and compare.
	altState2, _, err := Allocate(clab, altConfig)
	if err != nil {
		t.Fatalf("second Allocate with alternate config failed: %v", err)
	}

	json1, _ := json.Marshal(altState)
	json2, _ := json.Marshal(altState2)
	if string(json1) != string(json2) {
		t.Error("Allocate with alternate config is not deterministic: two calls produced different results")
	}
}

func TestGenerateMAC_Deterministic(t *testing.T) {
	// Same inputs produce same output.
	mac1 := GenerateMAC("leafA", "red")
	mac2 := GenerateMAC("leafA", "red")
	if mac1 != mac2 {
		t.Errorf("GenerateMAC not deterministic: %q != %q", mac1, mac2)
	}

	// Verify locally-administered bit (starts with 02:).
	if mac1[:3] != "02:" {
		t.Errorf("MAC should start with 02:, got %q", mac1[:3])
	}

	// Verify format: 02:xx:xx:xx:xx:xx (17 chars).
	if len(mac1) != 17 {
		t.Errorf("MAC length should be 17, got %d: %q", len(mac1), mac1)
	}

	// Different inputs produce different outputs.
	mac3 := GenerateMAC("leafA", "blue")
	if mac1 == mac3 {
		t.Error("GenerateMAC should produce different MACs for different inputs")
	}

	mac4 := GenerateMAC("leafB", "red")
	if mac1 == mac4 {
		t.Error("GenerateMAC should produce different MACs for different node names")
	}
}
