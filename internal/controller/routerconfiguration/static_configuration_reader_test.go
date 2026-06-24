// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
    nics:
      - eth0
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

	apiConfig, err := readStaticConfigs(dir)
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
    nics:
      - eth0
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

	apiConfig, err := readStaticConfigs(dir)
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
    nics:
      - eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
`)

	apiConfig, err := readStaticConfigs(dir)
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
    nics:
      - eth0
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

	apiConfig, err := readStaticConfigs(dir)
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
    nics:
      - eth0
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

	apiConfig, err := readStaticConfigs(dir)
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
    nics:
      - eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
`)

	apiConfig, err := readStaticConfigs(dir)
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
    nics:
      - eth0
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

	apiConfig, err := readStaticConfigs(dir)
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

	apiConfig, err := readStaticConfigs(testdataDir)
	if err != nil {
		t.Fatalf("readStaticConfigs() with existing testdata unexpected error: %v", err)
	}

	expected := conversion.APIConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				TypeMeta:   metav1.TypeMeta{Kind: "Underlay", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-underlay-0"},
				Spec: v1alpha1.UnderlaySpec{
					ASN:          64514,
					RouterIDCIDR: new(defaultRouterIDCIDR),
					Nics:         []string{"toswitch1", "eth0"},
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
				},
			},
		},
		L3VNIs: []v1alpha1.L3VNI{
			{
				TypeMeta:   metav1.TypeMeta{Kind: "L3VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-l3vni-0"},
				Spec: v1alpha1.L3VNISpec{
					VRF: "red", VNI: 100, VXLanPort: new(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN: 64514, HostASN: new(int64(64515)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.10.0/24"), IPv6: new("2001:db8:1::/64")},
					},
				},
			},
			{
				TypeMeta:   metav1.TypeMeta{Kind: "L3VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-l3vni-1"},
				Spec: v1alpha1.L3VNISpec{
					VRF: "blue", VNI: 200, VXLanPort: new(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN: 64514, HostASN: new(int64(64516)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.11.0/24"), IPv6: new("2001:db8:2::/64")},
					},
				},
			},
		},
		L2VNIs: []v1alpha1.L2VNI{
			{
				TypeMeta:   metav1.TypeMeta{Kind: "L2VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-l2vni-0"},
				Spec: v1alpha1.L2VNISpec{
					VRF: new("storage"), VNI: 300, VXLanPort: new(int32(4789)),
					HostMaster: &v1alpha1.HostMaster{
						Type:        v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{Name: new("br-storage"), AutoCreate: new(false)},
					},
				},
			},
			{
				TypeMeta:   metav1.TypeMeta{Kind: "L2VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-l2vni-1"},
				Spec: v1alpha1.L2VNISpec{
					VRF: new("management"), VNI: 400, VXLanPort: new(int32(4789)),
					HostMaster: &v1alpha1.HostMaster{
						Type:      v1alpha1.OVSBridge,
						OVSBridge: &v1alpha1.OVSBridgeConfig{Name: new("ovsbr0"), AutoCreate: new(false)},
					},
				},
			},
		},
		L3Passthrough: []v1alpha1.L3Passthrough{
			{
				TypeMeta:   metav1.TypeMeta{Kind: "L3Passthrough", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-l3passthrough"},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 64514, HostASN: new(int64(64517)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.100.0/24"), IPv6: new("2001:db8:100::/64")},
					},
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
    nics:
      - eth0
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

	_, err := readStaticConfigs(dir)
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
    nics:
      - eth0
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

			_, err := readStaticConfigs(dir)
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
    nics:
      - eth0
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

	_, err := readStaticConfigs(dir)
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
    nics:
      - eth0
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

	_, err := readStaticConfigs(dir)
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
