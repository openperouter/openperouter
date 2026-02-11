// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openperouter/openperouter/api/static"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"k8s.io/utils/ptr"
)

func TestReadNodeConfig(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		expected    *static.NodeConfig
		expectError bool
	}{
		{
			name:     "valid yaml config",
			content:  "nodeIndex: 42\nlogLevel: debug\n",
			expected: &static.NodeConfig{NodeIndex: 42, LogLevel: "debug"},
		},
		{
			name:     "valid yaml with zero value",
			content:  "nodeIndex: 0\nlogLevel: info\n",
			expected: &static.NodeConfig{NodeIndex: 0, LogLevel: "info"},
		},
		{
			name:     "valid yaml with only nodeIndex",
			content:  "nodeIndex: 1\n",
			expected: &static.NodeConfig{NodeIndex: 1, LogLevel: ""},
		},
		{
			name:        "invalid yaml",
			content:     "invalid: [unclosed\n",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "node-config.yaml")

			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test config file: %v", err)
			}

			config, err := ReadNodeConfig(configPath)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if config.NodeIndex != tt.expected.NodeIndex {
				t.Errorf("expected NodeIndex %d, got %d", tt.expected.NodeIndex, config.NodeIndex)
			}

			if config.LogLevel != tt.expected.LogLevel {
				t.Errorf("expected LogLevel %s, got %s", tt.expected.LogLevel, config.LogLevel)
			}
		})
	}
}

func TestReadNodeConfig_NonExistentFile(t *testing.T) {
	_, err := ReadNodeConfig("/nonexistent/path/node-config.yaml")
	if err == nil {
		t.Errorf("expected error for non-existent file, got: %v", err)
	}
}

func TestReadRouterConfigs(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := ReadRouterConfigs(tmpDir)
		if err == nil {
			t.Fatal("expected NoConfigAvailable error, got nil")
		}
		var noConfigErr *NoConfigAvailable
		if !errors.As(err, &noConfigErr) {
			t.Errorf("expected NoConfigAvailable error, got: %v", err)
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		_, err := ReadRouterConfigs("/nonexistent/path")
		if err == nil {
			t.Fatal("expected NoConfigAvailable error, got nil")
		}
		var noConfigErr *NoConfigAvailable
		if !errors.As(err, &noConfigErr) {
			t.Errorf("expected NoConfigAvailable error, got: %v", err)
		}
	})

	t.Run("single file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "openpe_underlay.yaml")
		content := `underlays:
  - asn: 64515
    routeridcidr: "10.0.0.0/24"
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test config file: %v", err)
		}

		configs, err := ReadRouterConfigs(tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 1 {
			t.Fatalf("expected 1 config, got %d", len(configs))
		}
		if len(configs[0].Underlays) != 1 {
			t.Errorf("expected 1 underlay, got %d", len(configs[0].Underlays))
		}
	})

	t.Run("multiple files", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create first config file
		configPath1 := filepath.Join(tmpDir, "openpe_underlay.yaml")
		content1 := `underlays:
  - asn: 64515
    routeridcidr: "10.0.0.0/24"
`
		if err := os.WriteFile(configPath1, []byte(content1), 0644); err != nil {
			t.Fatalf("failed to write test config file: %v", err)
		}

		// Create second config file
		configPath2 := filepath.Join(tmpDir, "openpe_l3vni.yaml")
		content2 := `l3vnis:
  - vrf: "vrf-test"
    vni: 1000
`
		if err := os.WriteFile(configPath2, []byte(content2), 0644); err != nil {
			t.Fatalf("failed to write test config file: %v", err)
		}

		// Create a non-matching file (should be ignored)
		nonMatchingPath := filepath.Join(tmpDir, "other.yaml")
		if err := os.WriteFile(nonMatchingPath, []byte("test: value\n"), 0644); err != nil {
			t.Fatalf("failed to write non-matching file: %v", err)
		}

		configs, err := ReadRouterConfigs(tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 2 {
			t.Fatalf("expected 2 configs, got %d", len(configs))
		}

		// Verify contents
		var hasUnderlay, hasL3VNI bool
		for _, cfg := range configs {
			if len(cfg.Underlays) > 0 {
				hasUnderlay = true
			}
			if len(cfg.L3VNIs) > 0 {
				hasL3VNI = true
			}
		}
		if !hasUnderlay {
			t.Error("expected at least one config with underlays")
		}
		if !hasL3VNI {
			t.Error("expected at least one config with l3vnis")
		}
	})

	t.Run("invalid file in directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "openpe_invalid.yaml")
		content := "invalid: [unclosed\n"
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test config file: %v", err)
		}

		_, err := ReadRouterConfigs(tmpDir)
		if err == nil {
			t.Error("expected error for invalid YAML file")
		}
	})
}

func TestReadRouterConfigsFromFiles(t *testing.T) {
	testdataDir := "./testdata"

	configs, err := ReadRouterConfigs(testdataDir)
	if err != nil {
		t.Fatalf("unexpected error reading testdata: %v", err)
	}

	if len(configs) != 4 {
		t.Fatalf("expected 4 config files, got %d", len(configs))
	}

	underlays := make([]v1alpha1.UnderlaySpec, 0, len(configs))
	l3vnis := make([]v1alpha1.L3VNISpec, 0, len(configs))
	l2vnis := make([]v1alpha1.L2VNISpec, 0, len(configs))
	var bgpPassthrough *v1alpha1.L3PassthroughSpec

	for _, cfg := range configs {
		underlays = append(underlays, cfg.Underlays...)
		l3vnis = append(l3vnis, cfg.L3VNIs...)
		l2vnis = append(l2vnis, cfg.L2VNIs...)
		if cfg.BGPPassthrough.HostSession.ASN != 0 {
			bgpPassthrough = &cfg.BGPPassthrough
		}
	}

	// openpe_underlay.yaml
	wantUnderlay := v1alpha1.UnderlaySpec{
		ASN:  64514,
		Nics: []string{"toswitch", "eth0"},
		Neighbors: []v1alpha1.Neighbor{
			{
				ASN:     64512,
				Address: "192.168.11.2",
			},
			{
				ASN:     64512,
				Address: "192.168.11.3",
				BFD: &v1alpha1.BFDSettings{
					ReceiveInterval:  ptr.To(uint32(300)),
					TransmitInterval: ptr.To(uint32(300)),
					DetectMultiplier: ptr.To(uint32(3)),
				},
			},
		},
		EVPN: &v1alpha1.EVPNConfig{
			VTEPCIDR: "100.65.0.0/24",
		},
	}

	// openpe_l3vni.yaml
	wantL3VNIs := []v1alpha1.L3VNISpec{
		{
			VRF: "red",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64515,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.10.0/24",
					IPv6: "2001:db8:1::/64",
				},
			},
			VNI: 100,
		},
		{
			VRF: "blue",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64516,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.11.0/24",
					IPv6: "2001:db8:2::/64",
				},
			},
			VNI: 200,
		},
	}

	// openpe_l2vni.yaml
	wantL2VNIs := []v1alpha1.L2VNISpec{
		{
			VRF:       ptr.To("storage"),
			VNI:       300,
			VXLanPort: 4789,
			HostMaster: &v1alpha1.HostMaster{
				Type: "linux-bridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Name: "br-storage",
				},
			},
		},
		{
			VRF:       ptr.To("management"),
			VNI:       400,
			VXLanPort: 4789,
			HostMaster: &v1alpha1.HostMaster{
				Type: "ovs-bridge",
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name: "ovsbr0",
				},
			},
		},
	}

	// openpe_bgppassthrough.yaml
	wantBGPPassthrough := v1alpha1.L3PassthroughSpec{
		HostSession: v1alpha1.HostSession{
			ASN:     64514,
			HostASN: 64517,
			LocalCIDR: v1alpha1.LocalCIDRConfig{
				IPv4: "192.169.100.0/24",
				IPv6: "2001:db8:100::/64",
			},
		},
	}

	sortNeighbors := cmpopts.SortSlices(func(a, b v1alpha1.Neighbor) bool {
		return a.Address < b.Address
	})
	sortL3VNIs := cmpopts.SortSlices(func(a, b v1alpha1.L3VNISpec) bool {
		return a.VRF < b.VRF
	})
	sortL2VNIs := cmpopts.SortSlices(func(a, b v1alpha1.L2VNISpec) bool {
		return a.VNI < b.VNI
	})

	if len(underlays) != 1 {
		t.Fatalf("expected 1 underlay, got %d", len(underlays))
	}
	if !cmp.Equal(wantUnderlay, underlays[0], sortNeighbors) {
		t.Errorf("underlay mismatch (-want +got):\n%s", cmp.Diff(wantUnderlay, underlays[0], sortNeighbors))
	}

	if !cmp.Equal(wantL3VNIs, l3vnis, sortL3VNIs) {
		t.Errorf("L3VNIs mismatch (-want +got):\n%s", cmp.Diff(wantL3VNIs, l3vnis, sortL3VNIs))
	}

	if !cmp.Equal(wantL2VNIs, l2vnis, sortL2VNIs) {
		t.Errorf("L2VNIs mismatch (-want +got):\n%s", cmp.Diff(wantL2VNIs, l2vnis, sortL2VNIs))
	}

	if bgpPassthrough == nil {
		t.Fatal("expected BGP passthrough configuration")
	}
	if !cmp.Equal(wantBGPPassthrough, *bgpPassthrough) {
		t.Errorf("BGP passthrough mismatch (-want +got):\n%s", cmp.Diff(wantBGPPassthrough, *bgpPassthrough))
	}
}
