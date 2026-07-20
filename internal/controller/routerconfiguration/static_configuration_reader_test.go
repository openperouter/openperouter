// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openperouter/openperouter/api/static"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const defaultRouterIDCIDR = "10.0.0.0/24"

func TestReadStaticConfigs_L2VNI_DefaultVXLanPort(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_l2vni.yaml", `
underlays:
  - asn: 64515
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l2vnis:
  - vni: 300
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "br-storage"
`)

	apiConfig, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() unexpected error: %v", err)
	}

	if len(apiConfig.L2VNIs) != 1 {
		t.Fatalf("expected 1 L2VNI, got %d", len(apiConfig.L2VNIs))
	}
	if ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0) != 4789 {
		t.Errorf("expected VXLanPort=4789 (default), got %d", ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0))
	}
}

func TestReadStaticConfigs_L3VNI_DefaultVXLanPort(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_l3vni.yaml", `
underlays:
  - asn: 64515
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l3vnis:
  - vrf: "red"
    vni: 100
`)

	apiConfig, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() unexpected error: %v", err)
	}

	if len(apiConfig.L3VNIs) != 1 {
		t.Fatalf("expected 1 L3VNI, got %d", len(apiConfig.L3VNIs))
	}
	if ptr.Deref(apiConfig.L3VNIs[0].Spec.VXLanPort, 0) != 4789 {
		t.Errorf("expected VXLanPort=4789 (default), got %d", ptr.Deref(apiConfig.L3VNIs[0].Spec.VXLanPort, 0))
	}
}

func TestReadStaticConfigs_Underlay_DefaultRouterIDCIDR(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_underlay.yaml", `
underlays:
  - asn: 64515
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
`)

	apiConfig, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() unexpected error: %v", err)
	}

	if len(apiConfig.Underlays) != 1 {
		t.Fatalf("expected 1 Underlay, got %d", len(apiConfig.Underlays))
	}
	if ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, "") != defaultRouterIDCIDR {
		t.Errorf("expected RouterIDCIDR=%s (default), got %q", defaultRouterIDCIDR, ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, ""))
	}
}

func TestReadStaticConfigs_AllDefaults(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_all.yaml", `
underlays:
  - asn: 64515
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l3vnis:
  - vrf: "red"
    vni: 100
l2vnis:
  - vni: 300
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "br-storage"
`)

	apiConfig, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() unexpected error: %v", err)
	}

	if ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, "") != defaultRouterIDCIDR {
		t.Errorf("expected Underlay RouterIDCIDR=%s, got %q", defaultRouterIDCIDR, ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, ""))
	}
	if ptr.Deref(apiConfig.L3VNIs[0].Spec.VXLanPort, 0) != 4789 {
		t.Errorf("expected L3VNI VXLanPort=4789, got %d", ptr.Deref(apiConfig.L3VNIs[0].Spec.VXLanPort, 0))
	}
	if ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0) != 4789 {
		t.Errorf("expected L2VNI VXLanPort=4789, got %d", ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0))
	}
}

func TestReadStaticConfigs_ExplicitVXLanPort(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_explicit.yaml", `
underlays:
  - asn: 64515
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l2vnis:
  - vni: 300
    vxlanport: 5000
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "br-storage"
`)

	apiConfig, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() unexpected error: %v", err)
	}

	if ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0) != 5000 {
		t.Errorf("expected VXLanPort=5000 (explicit), got %d", ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0))
	}
}

func TestReadStaticConfigs_ExplicitRouterIDCIDR(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_explicit.yaml", `
underlays:
  - asn: 64515
    routeridcidr: "172.16.0.0/16"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
`)

	apiConfig, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() unexpected error: %v", err)
	}

	if ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, "") != "172.16.0.0/16" {
		t.Errorf("expected RouterIDCIDR=172.16.0.0/16 (explicit), got %q", ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, ""))
	}
}

func TestReadStaticConfigs_MultiFileDefaults(t *testing.T) {
	dir := t.TempDir()

	writeYAMLFile(t, dir, "openpe_underlay.yaml", `
underlays:
  - asn: 64515
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
`)

	writeYAMLFile(t, dir, "openpe_l2vni.yaml", `
l2vnis:
  - vni: 300
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "br-storage"
`)

	apiConfig, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() unexpected error: %v", err)
	}

	if len(apiConfig.Underlays) != 1 {
		t.Fatalf("expected 1 Underlay, got %d", len(apiConfig.Underlays))
	}
	if ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, "") != defaultRouterIDCIDR {
		t.Errorf("expected Underlay RouterIDCIDR=%s (default), got %q", defaultRouterIDCIDR, ptr.Deref(apiConfig.Underlays[0].Spec.RouterIDCIDR, ""))
	}

	if len(apiConfig.L2VNIs) != 1 {
		t.Fatalf("expected 1 L2VNI, got %d", len(apiConfig.L2VNIs))
	}
	if ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0) != 4789 {
		t.Errorf("expected L2VNI VXLanPort=4789 (default), got %d", ptr.Deref(apiConfig.L2VNIs[0].Spec.VXLanPort, 0))
	}
}

func TestReadStaticConfigs_ExistingTestdata(t *testing.T) {
	testdataDir := "../../staticconfiguration/testdata"

	apiConfig, err := readStaticConfigs(testdataDir, "test-node", "test-namespace")
	if err != nil {
		t.Fatalf("readStaticConfigs() with existing testdata unexpected error: %v", err)
	}

	expected := conversion.APIConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				TypeMeta: metav1.TypeMeta{Kind: "Underlay", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-underlay-0",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:          64514,
					RouterIDCIDR: new(defaultRouterIDCIDR),
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type:          "NetworkDevice",
							NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"},
						},
						{
							Type:          "NetworkDevice",
							NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
						},
					},
					Neighbors: []v1alpha1.Neighbor{
						{ASN: new(int64(64512)), Address: new("192.168.11.2")},
						{
							ASN:     new(int64(64512)),
							Address: new("192.168.11.3"),
							BFD: &v1alpha1.BFDSettings{
								ReceiveInterval:  new(int32(300)),
								TransmitInterval: new(int32(300)),
								DetectMultiplier: new(int32(3)),
							},
						},
					},
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"100.65.0.0/24"}},
					NodeSelector:   &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
		},
		L3VNIs: []v1alpha1.L3VNI{
			{
				TypeMeta: metav1.TypeMeta{Kind: "L3VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-l3vni-0",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "red", VNI: 100, VXLanPort: new(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN: 64514, HostASN: new(int64(64515)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.10.0/24"), IPv6: new("2001:db8:1::/64")},
					},
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
			{
				TypeMeta: metav1.TypeMeta{Kind: "L3VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-l3vni-1",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "blue", VNI: 200, VXLanPort: new(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN: 64514, HostASN: new(int64(64516)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.11.0/24"), IPv6: new("2001:db8:2::/64")},
					},
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
		},
		L2VNIs: []v1alpha1.L2VNI{
			{
				TypeMeta: metav1.TypeMeta{Kind: "L2VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-l2vni-0",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L2VNISpec{
					VRF: new("storage"), VNI: 300, VXLanPort: new(int32(4789)),
					HostMaster: &v1alpha1.HostMaster{
						Type:        v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{Name: new("br-storage"), AutoCreate: new(false)},
					},
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
			{
				TypeMeta: metav1.TypeMeta{Kind: "L2VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-l2vni-1",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L2VNISpec{
					VRF: new("management"), VNI: 400, VXLanPort: new(int32(4789)),
					HostMaster: &v1alpha1.HostMaster{
						Type:      v1alpha1.OVSBridge,
						OVSBridge: &v1alpha1.OVSBridgeConfig{Name: new("ovsbr0"), AutoCreate: new(false)},
					},
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
		},
		L3VPNs: []v1alpha1.L3VPN{
			{
				TypeMeta:   metav1.TypeMeta{Kind: "L3VPN", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-l3vpn-0"},
				Spec: v1alpha1.L3VPNSpec{
					VRF:              "red",
					RDAssignedNumber: 100,
					ExportRTs: []v1alpha1.RouteTarget{
						"64514:100",
					},
					ImportRTs: []v1alpha1.RouteTarget{
						"64520:100",
					},
					HostSession: &v1alpha1.HostSession{
						ASN:       64514,
						HostASN:   new(int64(64515)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.10.0/24")},
					},
				},
			},
		},
		L3Passthrough: []v1alpha1.L3Passthrough{
			{
				TypeMeta: metav1.TypeMeta{Kind: "L3Passthrough", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-l3passthrough",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 64514, HostASN: new(int64(64517)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.100.0/24"), IPv6: new("2001:db8:100::/64")},
					},
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
		},
	}

	if diff := cmp.Diff(expected, apiConfig); diff != "" {
		t.Errorf("existing testdata mismatch (-expected +got):\n%s", diff)
	}
}

func TestReadStaticConfigs_CELValidation_L2VNIBridgeNameAndAutoCreate(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_invalid.yaml", `
underlays:
  - asn: 64515
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l2vnis:
  - vni: 300
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "mybr"
        autoCreate: true
`)

	_, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err == nil {
		t.Fatal("expected validation error for L2VNI with bridge name and autoCreate, got nil")
	}
	if !strings.Contains(err.Error(), "either name must be set or autoCreate must be true, but not both") {
		t.Errorf("expected error containing 'either name must be set or autoCreate must be true, but not both', got: %v", err)
	}
}

func TestReadStaticConfigs_ErrorMessageQuality(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantContains string
	}{
		{
			name: "LinuxBridge CEL message is exact",
			yaml: `
underlays:
  - asn: 64515
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l2vnis:
  - vni: 300
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "mybr"
        autoCreate: true
`,
			wantContains: "either name must be set or autoCreate must be true, but not both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeYAMLFile(t, dir, "openpe_invalid.yaml", tc.yaml)

			_, err := readStaticConfigs(dir, "test-node", "test-namespace")
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantContains) {
				t.Errorf("expected error to contain exact CEL message %q, got: %v", tc.wantContains, err)
			}
		})
	}
}

func TestReadStaticConfigs_MultipleErrors(t *testing.T) {
	dir := t.TempDir()
	writeYAMLFile(t, dir, "openpe_multi_invalid.yaml", `
underlays:
  - routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l2vnis:
  - vni: 300
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "mybr"
        autoCreate: true
`)

	_, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err == nil {
		t.Fatal("expected validation errors for invalid underlay AND invalid L2VNI, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "asn") {
		t.Errorf("expected error from underlay missing required ASN field, got: %v", err)
	}
	if !strings.Contains(errMsg, "either name must be set or autoCreate must be true, but not both") {
		t.Errorf("expected error from L2VNI bridge validation, got: %v", err)
	}
}

func TestReadStaticConfigs_AtomicRejection(t *testing.T) {
	dir := t.TempDir()
	// One valid underlay, one invalid L2VNI
	writeYAMLFile(t, dir, "openpe_atomic.yaml", `
underlays:
  - asn: 64515
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
l2vnis:
  - vni: 300
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: "mybr"
        autoCreate: true
`)

	_, err := readStaticConfigs(dir, "test-node", "test-namespace")
	if err == nil {
		t.Fatal("expected error for config with 1 valid underlay and 1 invalid L2VNI, got nil -- partial result should not be returned")
	}

	// Verify the error is about the L2VNI validation, not about the underlay
	if !strings.Contains(err.Error(), "either name must be set or autoCreate must be true, but not both") {
		t.Errorf("expected L2VNI validation error, got: %v", err)
	}
}

func writeYAMLFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write YAML file %s: %v", path, err)
	}
}

func TestStaticConfigToAPIConfig_WithNodeName(t *testing.T) {
	cfg := &static.PERouterConfig{
		Underlays: []v1alpha1.UnderlaySpec{
			{
				ASN: 64514,
				Interfaces: []v1alpha1.UnderlayInterface{
					{
						Type:          "NetworkDevice",
						NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
					},
				},
				Neighbors: []v1alpha1.Neighbor{
					{ASN: new(int64(64512)), Address: new("192.168.11.2")},
				},
			},
			{
				ASN: 64515,
				Interfaces: []v1alpha1.UnderlayInterface{
					{
						Type:          "NetworkDevice",
						NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth1"},
					},
				},
				Neighbors: []v1alpha1.Neighbor{
					{ASN: new(int64(64513)), Address: new("192.168.11.3")},
				},
			},
		},
		L3VNIs: []v1alpha1.L3VNISpec{
			{
				VRF: "red",
				VNI: 100,
				HostSession: &v1alpha1.HostSession{
					ASN:       64514,
					HostASN:   new(int64(64515)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.10.0/24")},
				},
			},
			{
				VRF: "blue",
				VNI: 200,
				HostSession: &v1alpha1.HostSession{
					ASN:       64514,
					HostASN:   new(int64(64516)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.11.0/24")},
				},
			},
		},
		L2VNIs: []v1alpha1.L2VNISpec{
			{VNI: 300},
		},
		BGPPassthrough: v1alpha1.L3PassthroughSpec{
			HostSession: v1alpha1.HostSession{
				ASN:       64514,
				HostASN:   new(int64(64517)),
				LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.100.0/24")},
			},
		},
		RawFRRConfigs: []v1alpha1.RawFRRConfigSpec{
			{RawConfig: "test config"},
		},
	}

	result, err := staticConfigToAPIConfig(cfg, "worker-1", "openperouter-system")
	if err != nil {
		t.Fatalf("staticConfigToAPIConfig() unexpected error: %v", err)
	}

	// Verify naming convention
	tests := []struct {
		desc     string
		got      string
		expected string
	}{
		{"underlay 0", result.Underlays[0].Name, "static-worker-1-underlay-0"},
		{"underlay 1", result.Underlays[1].Name, "static-worker-1-underlay-1"},
		{"l3vni 0", result.L3VNIs[0].Name, "static-worker-1-l3vni-0"},
		{"l3vni 1", result.L3VNIs[1].Name, "static-worker-1-l3vni-1"},
		{"l2vni 0", result.L2VNIs[0].Name, "static-worker-1-l2vni-0"},
		{"l3passthrough", result.L3Passthrough[0].Name, "static-worker-1-l3passthrough"},
		{"rawfrrconfig 0", result.RawFRRConfigs[0].Name, "static-worker-1-rawfrrconfig-0"},
	}
	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("%s: expected name %s, got %s", tt.desc, tt.expected, tt.got)
		}
	}

	// Verify labels on all resource types
	allResources := []metav1.ObjectMeta{
		result.Underlays[0].ObjectMeta,
		result.Underlays[1].ObjectMeta,
		result.L3VNIs[0].ObjectMeta,
		result.L3VNIs[1].ObjectMeta,
		result.L2VNIs[0].ObjectMeta,
		result.L3Passthrough[0].ObjectMeta,
		result.RawFRRConfigs[0].ObjectMeta,
	}
	for _, meta := range allResources {
		if meta.Labels[StaticSourceLabel] != StaticSourceValue {
			t.Errorf("resource %s: expected label %s=%s, got %v", meta.Name, StaticSourceLabel, StaticSourceValue, meta.Labels)
		}
	}

	// Verify namespace on all resource types
	for _, meta := range allResources {
		if meta.Namespace != "openperouter-system" {
			t.Errorf("resource %s: expected namespace openperouter-system, got %s", meta.Name, meta.Namespace)
		}
	}

	// Verify NodeSelector on all resource types
	checkNodeSelector := func(name string, got *metav1.LabelSelector) {
		t.Helper()
		if got == nil {
			t.Errorf("resource %s: expected non-nil NodeSelector", name)
			return
		}
		if got.MatchLabels["kubernetes.io/hostname"] != "worker-1" {
			t.Errorf("resource %s: expected NodeSelector hostname=worker-1, got %v", name, got.MatchLabels)
		}
	}

	checkNodeSelector("underlay-0", result.Underlays[0].Spec.NodeSelector)
	checkNodeSelector("underlay-1", result.Underlays[1].Spec.NodeSelector)
	checkNodeSelector("l3vni-0", result.L3VNIs[0].Spec.NodeSelector)
	checkNodeSelector("l3vni-1", result.L3VNIs[1].Spec.NodeSelector)
	checkNodeSelector("l2vni-0", result.L2VNIs[0].Spec.NodeSelector)
	checkNodeSelector("l3passthrough", result.L3Passthrough[0].Spec.NodeSelector)
	checkNodeSelector("rawfrrconfig-0", result.RawFRRConfigs[0].Spec.NodeSelector)

	if result.L2VNIs[0].Spec.VRF != nil {
		t.Errorf("expected L2VNI VRF to be nil when not set, got %s", *result.L2VNIs[0].Spec.VRF)
	}
}

func TestStaticConfigToAPIConfig_L3PassthroughSkippedWhenZeroASN(t *testing.T) {
	cfg := &static.PERouterConfig{
		BGPPassthrough: v1alpha1.L3PassthroughSpec{
			HostSession: v1alpha1.HostSession{
				ASN: 0,
			},
		},
	}

	result, err := staticConfigToAPIConfig(cfg, "worker-1", "ns")
	if err != nil {
		t.Fatalf("staticConfigToAPIConfig() unexpected error: %v", err)
	}
	if len(result.L3Passthrough) != 0 {
		t.Errorf("expected no l3passthrough when ASN is 0, got %d", len(result.L3Passthrough))
	}
}

func TestStaticConfigToAPIConfig_PreservesSpecFields(t *testing.T) {
	cfg := &static.PERouterConfig{
		Underlays: []v1alpha1.UnderlaySpec{
			{
				ASN:          64514,
				RouterIDCIDR: new("10.0.0.0/24"),
				Interfaces: []v1alpha1.UnderlayInterface{
					{
						Type:          "NetworkDevice",
						NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
					},
				},
				Neighbors: []v1alpha1.Neighbor{
					{ASN: new(int64(64512)), Address: new("192.168.11.2")},
				},
			},
		},
		L3VNIs: []v1alpha1.L3VNISpec{
			{VRF: "red", VNI: 100, VXLanPort: new(int32(4789))},
		},
	}

	result, err := staticConfigToAPIConfig(cfg, "node-a", "test-ns")
	if err != nil {
		t.Fatalf("staticConfigToAPIConfig() unexpected error: %v", err)
	}

	if result.Underlays[0].Spec.ASN != 64514 {
		t.Errorf("expected ASN 64514, got %d", result.Underlays[0].Spec.ASN)
	}
	if ptr.Deref(result.Underlays[0].Spec.RouterIDCIDR, "") != "10.0.0.0/24" {
		t.Errorf("expected RouterIDCIDR 10.0.0.0/24, got %s", ptr.Deref(result.Underlays[0].Spec.RouterIDCIDR, ""))
	}
	if len(result.Underlays[0].Spec.Neighbors) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(result.Underlays[0].Spec.Neighbors))
	}
	if ptr.Deref(result.Underlays[0].Spec.Neighbors[0].Address, "") != "192.168.11.2" {
		t.Errorf("expected neighbor address 192.168.11.2, got %s", ptr.Deref(result.Underlays[0].Spec.Neighbors[0].Address, ""))
	}
	if result.L3VNIs[0].Spec.VRF != "red" {
		t.Errorf("expected VRF red, got %s", result.L3VNIs[0].Spec.VRF)
	}
	if result.L3VNIs[0].Spec.VNI != 100 {
		t.Errorf("expected VNI 100, got %d", result.L3VNIs[0].Spec.VNI)
	}
}

func TestStaticConfigToAPIConfig_L2VNIPreservesExplicitVRF(t *testing.T) {
	explicitVRF := "my-vrf"
	cfg := &static.PERouterConfig{
		L2VNIs: []v1alpha1.L2VNISpec{
			{VNI: 500, VRF: &explicitVRF},
		},
	}

	result, err := staticConfigToAPIConfig(cfg, "worker-1", "ns")
	if err != nil {
		t.Fatalf("staticConfigToAPIConfig() unexpected error: %v", err)
	}

	if result.L2VNIs[0].Spec.VRF == nil {
		t.Fatal("expected VRF to be set, got nil")
	}
	if *result.L2VNIs[0].Spec.VRF != "my-vrf" {
		t.Errorf("expected VRF my-vrf, got %s", *result.L2VNIs[0].Spec.VRF)
	}
}

func TestStaticConfigToAPIConfig_EmptyConfig(t *testing.T) {
	cfg := &static.PERouterConfig{}

	result, err := staticConfigToAPIConfig(cfg, "worker-1", "ns")
	if err != nil {
		t.Fatalf("staticConfigToAPIConfig() unexpected error: %v", err)
	}

	if len(result.Underlays) != 0 {
		t.Errorf("expected 0 underlays, got %d", len(result.Underlays))
	}
	if len(result.L3VNIs) != 0 {
		t.Errorf("expected 0 l3vnis, got %d", len(result.L3VNIs))
	}
	if len(result.L2VNIs) != 0 {
		t.Errorf("expected 0 l2vnis, got %d", len(result.L2VNIs))
	}
	if len(result.L3Passthrough) != 0 {
		t.Errorf("expected 0 l3passthrough, got %d", len(result.L3Passthrough))
	}
	if len(result.RawFRRConfigs) != 0 {
		t.Errorf("expected 0 rawfrrconfigs, got %d", len(result.RawFRRConfigs))
	}
}
