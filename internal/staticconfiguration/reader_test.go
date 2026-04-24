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
		assertNoConfigAvailable(t, err)
	})

	t.Run("non-existent directory", func(t *testing.T) {
		_, err := ReadRouterConfigs("/nonexistent/path")
		assertNoConfigAvailable(t, err)
	})

	t.Run("single file", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeTestFile(t, tmpDir, "openpe_underlay.yaml", "underlays:\n  - asn: 64515\n    routeridcidr: \"10.0.0.0/24\"\n")

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
		writeTestFile(t, tmpDir, "openpe_underlay.yaml", "underlays:\n  - asn: 64515\n    routeridcidr: \"10.0.0.0/24\"\n")
		writeTestFile(t, tmpDir, "openpe_l3vni.yaml", "l3vnis:\n  - vrf: \"vrf-test\"\n    vni: 1000\n")
		writeTestFile(t, tmpDir, "other.yaml", "test: value\n")

		configs, err := ReadRouterConfigs(tmpDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(configs) != 2 {
			t.Fatalf("expected 2 configs, got %d", len(configs))
		}
		assertConfigTypes(t, configs)
	})

	t.Run("invalid file in directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeTestFile(t, tmpDir, "openpe_invalid.yaml", "invalid: [unclosed\n")

		_, err := ReadRouterConfigs(tmpDir)
		if err == nil {
			t.Error("expected error for invalid YAML file")
		}
	})
}

func assertNoConfigAvailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected NoConfigAvailable error, got nil")
	}
	var noConfigErr *NoConfigAvailable
	if !errors.As(err, &noConfigErr) {
		t.Errorf("expected NoConfigAvailable error, got: %v", err)
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config file %s: %v", name, err)
	}
}

func assertConfigTypes(t *testing.T, configs []*static.PERouterConfig) {
	t.Helper()
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
				ASN:     ptr.To(int64(64512)),
				Address: ptr.To("192.168.11.2"),
			},
			{
				ASN:     ptr.To(int64(64512)),
				Address: ptr.To("192.168.11.3"),
				BFD: &v1alpha1.BFDSettings{
					ReceiveInterval:  ptr.To(int32(300)),
					TransmitInterval: ptr.To(int32(300)),
					DetectMultiplier: ptr.To(int32(3)),
				},
			},
		},
		EVPN: &v1alpha1.EVPNConfig{
			VTEPCIDR: ptr.To("100.65.0.0/24"),
		},
	}

	// openpe_l3vni.yaml
	wantL3VNIs := []v1alpha1.L3VNISpec{
		{
			VRF: ptr.To("red"),
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: ptr.To(int64(64515)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: ptr.To("192.169.10.0/24"),
					IPv6: ptr.To("2001:db8:1::/64"),
				},
			},
			VNI: ptr.To(int64(100)),
		},
		{
			VRF: ptr.To("blue"),
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: ptr.To(int64(64516)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: ptr.To("192.169.11.0/24"),
					IPv6: ptr.To("2001:db8:2::/64"),
				},
			},
			VNI: ptr.To(int64(200)),
		},
	}

	// openpe_l2vni.yaml
	wantL2VNIs := []v1alpha1.L2VNISpec{
		{
			VRF:       ptr.To("storage"),
			VNI:       ptr.To(int64(300)),
			VXLanPort: ptr.To(int32(4789)),
			HostMaster: &v1alpha1.HostMaster{
				Type: "linux-bridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Name: ptr.To("br-storage"),
				},
			},
		},
		{
			VRF:       ptr.To("management"),
			VNI:       ptr.To(int64(400)),
			VXLanPort: ptr.To(int32(4789)),
			HostMaster: &v1alpha1.HostMaster{
				Type: "ovs-bridge",
				OVSBridge: &v1alpha1.OVSBridgeConfig{
					Name: ptr.To("ovsbr0"),
				},
			},
		},
	}

	// openpe_bgppassthrough.yaml
	wantBGPPassthrough := v1alpha1.L3PassthroughSpec{
		HostSession: v1alpha1.HostSession{
			ASN:     64514,
			HostASN: ptr.To(int64(64517)),
			LocalCIDR: v1alpha1.LocalCIDRConfig{
				IPv4: ptr.To("192.169.100.0/24"),
				IPv6: ptr.To("2001:db8:100::/64"),
			},
		},
	}

	sortNeighbors := cmpopts.SortSlices(func(a, b v1alpha1.Neighbor) bool {
		if a.Address == nil || b.Address == nil {
			return false
		}
		return *a.Address < *b.Address
	})
	sortL3VNIs := cmpopts.SortSlices(func(a, b v1alpha1.L3VNISpec) bool {
		if a.VRF == nil || b.VRF == nil {
			return false
		}
		return *a.VRF < *b.VRF
	})
	sortL2VNIs := cmpopts.SortSlices(func(a, b v1alpha1.L2VNISpec) bool {
		if a.VNI == nil || b.VNI == nil {
			return false
		}
		return *a.VNI < *b.VNI
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
