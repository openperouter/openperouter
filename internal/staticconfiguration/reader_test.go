// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/openperouter/openperouter/api/static"
	"github.com/openperouter/openperouter/api/v1alpha1"
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

	t.Run("comprehensive testdata files", func(t *testing.T) {
		// Use the actual testdata directory with comprehensive resource types
		testdataDir := "./testdata"

		configs, err := ReadRouterConfigs(testdataDir)
		if err != nil {
			t.Fatalf("unexpected error reading testdata: %v", err)
		}

		if len(configs) != 4 {
			t.Fatalf("expected 4 config files, got %d", len(configs))
		}

		// Aggregate all configs to verify we have all resource types
		for _, cfg := range configs {
			verifyUnderlay(t, cfg)
			verifyL3VNIs(t, cfg)
			verifyL2VNIs(t, cfg)
			verifyBGPPassthrough(t, cfg)
		}
	})
}

func verifyUnderlay(t *testing.T, cfg *static.PERouterConfig) {
	t.Helper()
	if len(cfg.Underlays) == 0 {
		return
	}
	if len(cfg.Underlays) != 1 {
		t.Errorf("expected 1 underlay, got %d", len(cfg.Underlays))
	}
	underlay := cfg.Underlays[0]
	if underlay.ASN != 64514 {
		t.Errorf("expected underlay ASN 64514, got %d", underlay.ASN)
	}
	if len(underlay.Nics) < 2 {
		t.Errorf("expected at least 2 NICs in underlay, got %d", len(underlay.Nics))
	}
	if len(underlay.Neighbors) < 2 {
		t.Errorf("expected at least 2 neighbors in underlay, got %d", len(underlay.Neighbors))
	}
	if underlay.EVPN == nil {
		t.Error("expected EVPN config in underlay")
	}
}

func verifyL3VNI(t *testing.T, vni v1alpha1.L3VNISpec) {
	t.Helper()
	if vni.VRF == "" {
		t.Error("expected VRF name in L3VNI")
	}
	if vni.VNI == 0 {
		t.Error("expected non-zero VNI")
	}
	if vni.HostSession == nil {
		t.Error("expected HostSession in L3VNI")
		return
	}
	if vni.HostSession.ASN == 0 {
		t.Error("expected non-zero ASN in HostSession")
	}
	if vni.HostSession.LocalCIDR.IPv4 == "" && vni.HostSession.LocalCIDR.IPv6 == "" {
		t.Error("expected at least one LocalCIDR in HostSession")
	}
}

func verifyL3VNIs(t *testing.T, cfg *static.PERouterConfig) {
	t.Helper()
	if len(cfg.L3VNIs) == 0 {
		return
	}
	if len(cfg.L3VNIs) != 2 {
		t.Errorf("expected 2 L3VNIs, got %d", len(cfg.L3VNIs))
	}
	for _, vni := range cfg.L3VNIs {
		verifyL3VNI(t, vni)
	}
}

func verifyL2VNI(t *testing.T, vni v1alpha1.L2VNISpec) {
	t.Helper()
	if vni.VNI == 0 {
		t.Error("expected non-zero VNI in L2VNI")
	}
	if vni.HostMaster == nil {
		t.Error("expected HostMaster in L2VNI")
	}
}

func verifyL2VNIs(t *testing.T, cfg *static.PERouterConfig) {
	t.Helper()
	if len(cfg.L2VNIs) == 0 {
		return
	}
	if len(cfg.L2VNIs) != 2 {
		t.Errorf("expected 2 L2VNIs, got %d", len(cfg.L2VNIs))
	}
	for _, vni := range cfg.L2VNIs {
		verifyL2VNI(t, vni)
	}
}

func verifyBGPPassthrough(t *testing.T, cfg *static.PERouterConfig) {
	t.Helper()
	if cfg.BGPPassthrough.HostSession.ASN == 0 {
		return
	}
	if cfg.BGPPassthrough.HostSession.LocalCIDR.IPv4 == "" && cfg.BGPPassthrough.HostSession.LocalCIDR.IPv6 == "" {
		t.Error("expected at least one LocalCIDR in BGPPassthrough")
	}
}
