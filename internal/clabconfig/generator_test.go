// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"strings"
	"testing"
)

func TestGenerate_BasicTopology(t *testing.T) {
	clab, err := LoadClab("testdata/basic.clab.yml")
	if err != nil {
		t.Fatalf("LoadClab: %v", err)
	}
	config, err := LoadConfig("testdata/basic-config.yaml")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	state, _, err := Allocate(clab, config)
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}

	files, err := Generate(state)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Edge-leaf nodes should have both FRR config and setup script.
	for _, name := range []string{"leafA", "leafB"} {
		f, ok := files[name]
		if !ok {
			t.Errorf("no generated files for edge-leaf node %q", name)
			continue
		}
		if f.FRRConfig == "" {
			t.Errorf("empty FRR config for %q", name)
		}
		if f.SetupScript == "" {
			t.Errorf("empty setup script for %q", name)
		}
		if !strings.Contains(f.FRRConfig, "router bgp") {
			t.Errorf("FRR config for %q missing 'router bgp'", name)
		}
		if !strings.Contains(f.FRRConfig, "advertise-all-vni") {
			t.Errorf("FRR config for %q missing EVPN config", name)
		}
		if !strings.Contains(f.FRRConfig, "vrf red") {
			t.Errorf("FRR config for %q missing VRF 'red'", name)
		}
		if !strings.Contains(f.SetupScript, "ip addr add") {
			t.Errorf("setup script for %q missing VTEP IP setup", name)
		}
		if !strings.Contains(f.SetupScript, "type vrf table") {
			t.Errorf("setup script for %q missing VRF creation", name)
		}
		if !strings.Contains(f.SetupScript, "type vxlan") {
			t.Errorf("setup script for %q missing VXLAN setup", name)
		}
	}

	// Transit nodes should have FRR config but no setup script.
	for _, name := range []string{"spine", "leafkind"} {
		f, ok := files[name]
		if !ok {
			t.Errorf("no generated files for transit node %q", name)
			continue
		}
		if f.FRRConfig == "" {
			t.Errorf("empty FRR config for %q", name)
		}
		if f.SetupScript != "" {
			t.Errorf("transit node %q should not have setup script", name)
		}
		if !strings.Contains(f.FRRConfig, "router bgp") {
			t.Errorf("FRR config for %q missing 'router bgp'", name)
		}
		if strings.Contains(f.FRRConfig, "advertise-all-vni") {
			t.Errorf("transit FRR config for %q should not have EVPN VNI config", name)
		}
	}
}
