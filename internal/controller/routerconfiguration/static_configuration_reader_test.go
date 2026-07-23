// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openperouter/openperouter/api/static"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{
							"100.65.0.0/24",
							"2001:db8:1234:5678::/64",
						},
					},
					ISIS: &v1alpha1.ISISConfig{
						BaseNet: "49.0001.0002.0003.0004.00",
						Level:   new(int32(1)),
						Interfaces: []v1alpha1.ISISInterface{
							{
								Name:     "toswitch1",
								IPFamily: new(v1alpha1.IPFamilyIPv6),
							},
						},
					},
					SRV6: &v1alpha1.SRV6Config{
						EncapBehavior: new(v1alpha1.HEncapsRed),
						Locator: v1alpha1.SRV6Locator{
							BasePrefix: "fd00:0:32::/48",
							Format:     "usid-f3216",
						},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/hostname": "test-node",
						},
					},
				},
			},
		},
		L3VNIs: []v1alpha1.L3VNI{
			{
				TypeMeta: metav1.TypeMeta{Kind: "L3VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-l3vni-red",
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
					Name:      "static-test-node-l3vni-blue",
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
					Name:      "static-test-node-l2vni-storage",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L2VNISpec{
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VNI,
						L3VNI: &v1alpha1.L3VNIReference{Name: "static-test-node-l3vni-red"},
					},
					VNI:       300,
					VXLanPort: new(int32(4789)),
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
					Name:      "static-test-node-l2vni-management",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L2VNISpec{
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VNI,
						L3VNI: &v1alpha1.L3VNIReference{Name: "static-test-node-l3vni-blue"},
					},
					VNI:       400,
					VXLanPort: new(int32(4789)),
					HostMaster: &v1alpha1.HostMaster{
						Type:      v1alpha1.OVSBridge,
						OVSBridge: &v1alpha1.OVSBridgeConfig{Name: new("ovsbr0"), AutoCreate: new(false)},
					},
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
			{
				TypeMeta: metav1.TypeMeta{Kind: "L2VNI", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "static-test-node-l2-over-vpn",
					Namespace: "test-namespace",
					Labels: map[string]string{
						StaticSourceLabel: StaticSourceValue,
						StaticNodeLabel:   "test-node",
					},
				},
				Spec: v1alpha1.L2VNISpec{
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VPN,
						L3VPN: &v1alpha1.L3VPNReference{Name: "static-test-node-red"},
					},
					VNI:       210,
					VXLanPort: new(int32(4789)),
					HostMaster: &v1alpha1.HostMaster{
						Type:        v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{AutoCreate: new(true)},
					},
					GatewayIPs:   []string{"192.170.1.1/24"},
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "test-node"}},
				},
			},
		},
		L3VPNs: []v1alpha1.L3VPN{
			{
				TypeMeta:   metav1.TypeMeta{Kind: "L3VPN", APIVersion: "network.openperouter.io/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{Name: "static-test-node-red", Namespace: "test-namespace", Labels: map[string]string{StaticSourceLabel: StaticSourceValue, StaticNodeLabel: "test-node"}},
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

func TestReadStaticConfigsCELValidationSRv6(t *testing.T) {
	validSRv6Underlay := `
underlays:
  - asn: 64514
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: toswitch1
    neighbors:
      - asn: 64520
        address: "2001:db8:1234::1"
        ebgpMultiHop: true
    tunnelEndpoint:
      cidrs:
      - "2001:db8:1234:5678::/64"
    isis:
      baseNet: "49.0001.0002.0003.0004.00"
      level: 1
      interfaces:
        - name: toswitch1
          ipFamily: ipv6
    srv6:
      locator:
        basePrefix: "fd00:0:32::/48"
        format: "usid-f3216"
`
	tests := []struct {
		name       string
		yaml       string
		wantErrMsg string
	}{
		{
			name: "valid H.Encaps",
			yaml: fmt.Sprintf("%s      encapBehavior: \"%s\"\n", validSRv6Underlay, v1alpha1.HEncaps),
		},
		{
			name: "valid H.Encaps.Red",
			yaml: fmt.Sprintf("%s      encapBehavior: \"%s\"\n", validSRv6Underlay, v1alpha1.HEncapsRed),
		},
		{
			name: "valid no encapBehavior",
			yaml: validSRv6Underlay,
		},
		{
			name:       "invalid encapBehavior value",
			yaml:       fmt.Sprintf("%s      encapBehavior: \"%s\"\n", validSRv6Underlay, "H.Encaps.Invalid"),
			wantErrMsg: `Unsupported value: "H.Encaps.Invalid": supported values: "H.Encaps", "H.Encaps.Red"`,
		},
		{
			name: "srv6 without isis",
			yaml: `
underlays:
  - asn: 64514
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: toswitch1
    neighbors:
      - asn: 64520
        address: "2001:db8:1234::1"
        ebgpMultiHop: true
    tunnelEndpoint:
      cidrs:
      - "2001:db8:1234:5678::/64"
    srv6:
      locator:
        basePrefix: "fd00:0:32::/48"
        format: "usid-f3216"
`,
			wantErrMsg: "SRv6 can only be configured if isis is set",
		},
		{
			name: "srv6 without ipv6 tunnel endpoint",
			yaml: `
underlays:
  - asn: 64514
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: toswitch1
    neighbors:
      - asn: 64520
        address: "2001:db8:1234::1"
        ebgpMultiHop: true
    tunnelEndpoint:
      cidrs:
      - "100.65.0.0/24"
    isis:
      baseNet: "49.0001.0002.0003.0004.00"
      level: 1
      interfaces:
        - name: toswitch1
          ipFamily: ipv6
    srv6:
      locator:
        basePrefix: "fd00:0:32::/48"
        format: "usid-f3216"
`,
			wantErrMsg: "SRv6 requires at least one IPv6 CIDR in tunnelEndpoint.cidrs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeYAMLFile(t, dir, "openpe_srv6.yaml", tc.yaml)

			_, err := readStaticConfigs(dir, "test-node", "test-namespace")
			if tc.wantErrMsg != "" {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Errorf("expected error containing %q, got: %v", tc.wantErrMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
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
		Underlays: []static.StaticUnderlaySpec{
			{
				UnderlaySpec: v1alpha1.UnderlaySpec{
					ASN: 64514,
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type:          "NetworkDevice",
							NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
						},
					},
				},
				Neighbors: []static.StaticNeighbor{
					{Neighbor: v1alpha1.Neighbor{ASN: new(int64(64512)), Address: new("192.168.11.2")}},
				},
			},
			{
				UnderlaySpec: v1alpha1.UnderlaySpec{
					ASN: 64515,
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type:          "NetworkDevice",
							NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth1"},
						},
					},
				},
				Neighbors: []static.StaticNeighbor{
					{Neighbor: v1alpha1.Neighbor{ASN: new(int64(64513)), Address: new("192.168.11.3")}},
				},
			},
		},
		L3VNIs: []static.StaticL3VNI{
			{
				Name: "l3vni-red",
				L3VNISpec: v1alpha1.L3VNISpec{
					VRF: "red",
					VNI: 100,
					HostSession: &v1alpha1.HostSession{
						ASN:       64514,
						HostASN:   new(int64(64515)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.10.0/24")},
					},
				},
			},
			{
				Name: "l3vni-blue",
				L3VNISpec: v1alpha1.L3VNISpec{
					VRF: "blue",
					VNI: 200,
					HostSession: &v1alpha1.HostSession{
						ASN:       64514,
						HostASN:   new(int64(64516)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.169.11.0/24")},
					},
				},
			},
		},
		L2VNIs: []static.StaticL2VNI{
			{Name: "l2vni-plain", L2VNISpec: v1alpha1.L2VNISpec{VNI: 300}},
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
		{"l3vni 0", result.L3VNIs[0].Name, "static-worker-1-l3vni-red"},
		{"l3vni 1", result.L3VNIs[1].Name, "static-worker-1-l3vni-blue"},
		{"l2vni 0", result.L2VNIs[0].Name, "static-worker-1-l2vni-plain"},
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

	if result.L2VNIs[0].Spec.RoutingDomain != nil {
		t.Errorf("expected L2VNI RoutingDomain to be nil when not set, got %v", result.L2VNIs[0].Spec.RoutingDomain)
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
		Underlays: []static.StaticUnderlaySpec{
			{
				UnderlaySpec: v1alpha1.UnderlaySpec{
					ASN:          64514,
					RouterIDCIDR: new("10.0.0.0/24"),
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type:          "NetworkDevice",
							NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
						},
					},
				},
				Neighbors: []static.StaticNeighbor{
					{Neighbor: v1alpha1.Neighbor{ASN: new(int64(64512)), Address: new("192.168.11.2")}},
				},
			},
		},
		L3VNIs: []static.StaticL3VNI{
			{Name: "l3vni-red", L3VNISpec: v1alpha1.L3VNISpec{VRF: "red", VNI: 100, VXLanPort: new(int32(4789))}},
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

func TestStaticConfigToAPIConfig_L2VNIPreservesExplicitRoutingDomain(t *testing.T) {
	cfg := &static.PERouterConfig{
		L2VNIs: []static.StaticL2VNI{
			{
				Name: "l2vni-with-rd",
				L2VNISpec: v1alpha1.L2VNISpec{
					VNI: 500,
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VNI,
						L3VNI: &v1alpha1.L3VNIReference{Name: "my-l3vni"},
					},
				},
			},
		},
	}

	result, err := staticConfigToAPIConfig(cfg, "worker-1", "ns")
	if err != nil {
		t.Fatalf("staticConfigToAPIConfig() unexpected error: %v", err)
	}

	if result.L2VNIs[0].Spec.RoutingDomain == nil {
		t.Fatal("expected RoutingDomain to be set, got nil")
	}
	if result.L2VNIs[0].Spec.RoutingDomain.L3VNI == nil {
		t.Fatal("expected RoutingDomain.L3VNI to be set, got nil")
	}
	if result.L2VNIs[0].Spec.RoutingDomain.L3VNI.Name != "static-worker-1-my-l3vni" {
		t.Errorf("expected RoutingDomain L3VNI name static-worker-1-my-l3vni, got %s", result.L2VNIs[0].Spec.RoutingDomain.L3VNI.Name)
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

func TestNeighborKey(t *testing.T) {
	tests := []struct {
		name string
		n    v1alpha1.Neighbor
		want string
	}{
		{name: "address only", n: v1alpha1.Neighbor{Address: new("10.0.0.1")}, want: "addr:10.0.0.1:179"},
		{name: "address with default port", n: v1alpha1.Neighbor{Address: new("10.0.0.1"), Port: new(int32(179))}, want: "addr:10.0.0.1:179"},
		{name: "address with custom port", n: v1alpha1.Neighbor{Address: new("10.0.0.1"), Port: new(int32(1179))}, want: "addr:10.0.0.1:1179"},
		{name: "interface only", n: v1alpha1.Neighbor{Interface: new("eth0")}, want: "iface:eth0:179"},
		{name: "interface with port", n: v1alpha1.Neighbor{Interface: new("eth0"), Port: new(int32(200))}, want: "iface:eth0:200"},
		{name: "neither", n: v1alpha1.Neighbor{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := neighborKey(&tt.n); got != tt.want {
				t.Errorf("neighborKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateStaticNeighbors(t *testing.T) {
	basePath := field.NewPath("underlays").Index(0).Child("neighbors")

	tests := []struct {
		name      string
		neighbors []static.StaticNeighbor
		wantErr   bool
		errMsg    string
	}{
		{
			name: "valid neighbor without password",
			neighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64512))}},
			},
		},
		{
			name: "valid neighbor with password",
			neighbors: []static.StaticNeighbor{
				{
					Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64512))},
					Password: new("validpass"),
				},
			},
		},
		{
			name: "password too long",
			neighbors: []static.StaticNeighbor{
				{
					Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64512))},
					Password: new(strings.Repeat("a", 81)),
				},
			},
			wantErr: true,
			errMsg:  "maximum length",
		},
		{
			name: "password with newline",
			neighbors: []static.StaticNeighbor{
				{
					Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64512))},
					Password: new("x\n  redistribute connected"),
				},
			},
			wantErr: true,
			errMsg:  "whitespace",
		},
		{
			name: "password with space",
			neighbors: []static.StaticNeighbor{
				{
					Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64512))},
					Password: new("pass word"),
				},
			},
			wantErr: true,
			errMsg:  "whitespace",
		},
		{
			name: "duplicate address",
			neighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64512))}},
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64513))}},
			},
			wantErr: true,
			errMsg:  "duplicate",
		},
		{
			name: "same address different ports is not duplicate",
			neighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64512)), Port: new(int32(179))}},
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1"), ASN: new(int64(64513)), Port: new(int32(1179))}},
			},
		},
		{
			name: "nil address and interface",
			neighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{ASN: new(int64(64512))}},
			},
			wantErr: true,
			errMsg:  "neither address nor interface",
		},
		{
			name: "password clears passwordSecret",
			neighbors: []static.StaticNeighbor{
				{
					Neighbor: v1alpha1.Neighbor{
						Address:        new("10.0.0.1"),
						ASN:            new(int64(64512)),
						PasswordSecret: new("my-secret"),
					},
					Password: new("staticpass"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateStaticNeighbors(tt.neighbors, basePath)
			if tt.wantErr {
				if len(errs) == 0 {
					t.Fatal("expected validation error, got none")
				}
				if !strings.Contains(errs.ToAggregate().Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, errs.ToAggregate().Error())
				}
				return
			}
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs.ToAggregate())
			}
		})
	}

	// Verify passwordSecret was cleared when password takes priority.
	t.Run("verify passwordSecret cleared", func(t *testing.T) {
		neighbors := []static.StaticNeighbor{
			{
				Neighbor: v1alpha1.Neighbor{
					Address:        new("10.0.0.1"),
					ASN:            new(int64(64512)),
					PasswordSecret: new("my-secret"),
				},
				Password: new("staticpass"),
			},
		}
		errs := validateStaticNeighbors(neighbors, basePath)
		if len(errs) > 0 {
			t.Fatalf("unexpected errors: %v", errs.ToAggregate())
		}
		if neighbors[0].PasswordSecret != nil {
			t.Error("expected PasswordSecret to be cleared, got non-nil")
		}
	})
}

func TestRestoreStaticPasswords(t *testing.T) {
	tests := []struct {
		name            string
		neighbors       []v1alpha1.Neighbor
		staticNeighbors []static.StaticNeighbor
		wantPasswords   map[string]*string
	}{
		{
			name: "restores password by address",
			neighbors: []v1alpha1.Neighbor{
				{Address: new("10.0.0.1")},
			},
			staticNeighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1")}, Password: new("secret")},
			},
			wantPasswords: map[string]*string{"10.0.0.1": new("secret")},
		},
		{
			name: "no password stays nil",
			neighbors: []v1alpha1.Neighbor{
				{Address: new("10.0.0.1")},
			},
			staticNeighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1")}},
			},
			wantPasswords: map[string]*string{"10.0.0.1": nil},
		},
		{
			name: "restores password by interface",
			neighbors: []v1alpha1.Neighbor{
				{Interface: new("eth0")},
			},
			staticNeighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{Interface: new("eth0")}, Password: new("ifacepw")},
			},
			wantPasswords: map[string]*string{"eth0": new("ifacepw")},
		},
		{
			name: "multiple neighbors get correct passwords",
			neighbors: []v1alpha1.Neighbor{
				{Address: new("10.0.0.1")},
				{Address: new("10.0.0.2")},
			},
			staticNeighbors: []static.StaticNeighbor{
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.1")}, Password: new("pw1")},
				{Neighbor: v1alpha1.Neighbor{Address: new("10.0.0.2")}, Password: new("pw2")},
			},
			wantPasswords: map[string]*string{
				"10.0.0.1": new("pw1"),
				"10.0.0.2": new("pw2"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restoreStaticPasswords(tt.neighbors, tt.staticNeighbors)
			for _, n := range tt.neighbors {
				addr := neighborAddr(&n)
				want := tt.wantPasswords[addr]
				switch {
				case want == nil && n.Password == nil:
				case want == nil || n.Password == nil:
					t.Errorf("neighbor %s: password = %v, want %v", addr, n.Password, want)
				case *want != *n.Password:
					t.Errorf("neighbor %s: password = %q, want %q", addr, *n.Password, *want)
				}
			}
		})
	}
}

func TestStaticConfigPasswordRoundTrip(t *testing.T) {
	cfg := &static.PERouterConfig{
		Underlays: []static.StaticUnderlaySpec{
			{
				UnderlaySpec: v1alpha1.UnderlaySpec{
					ASN: 64514,
					Interfaces: []v1alpha1.UnderlayInterface{
						{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}},
					},
				},
				Neighbors: []static.StaticNeighbor{
					{
						Neighbor: v1alpha1.Neighbor{ASN: new(int64(64512)), Address: new("192.168.11.2")},
						Password: new("my-bgp-pass"),
					},
				},
			},
		},
	}

	result, err := staticConfigToAPIConfig(cfg, "worker-1", "openperouter-system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Underlays) != 1 {
		t.Fatalf("expected 1 underlay, got %d", len(result.Underlays))
	}
	if len(result.Underlays[0].Spec.Neighbors) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(result.Underlays[0].Spec.Neighbors))
	}
	pw := result.Underlays[0].Spec.Neighbors[0].Password
	if pw == nil || *pw != "my-bgp-pass" {
		t.Errorf("password not preserved through round-trip, got %v", pw)
	}
}

func TestStaticConfigInvalidPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{name: "newline injection", password: "x\n  redistribute connected"},
		{name: "too long", password: strings.Repeat("a", 81)},
		{name: "contains space", password: "pass word"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &static.PERouterConfig{
				Underlays: []static.StaticUnderlaySpec{
					{
						UnderlaySpec: v1alpha1.UnderlaySpec{
							ASN: 64514,
							Interfaces: []v1alpha1.UnderlayInterface{
								{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"}},
							},
						},
						Neighbors: []static.StaticNeighbor{
							{
								Neighbor: v1alpha1.Neighbor{ASN: new(int64(64512)), Address: new("192.168.11.2")},
								Password: new(tt.password),
							},
						},
					},
				},
			}
			_, err := staticConfigToAPIConfig(cfg, "worker-1", "ns")
			if err == nil {
				t.Fatal("expected error for invalid password, got nil")
			}
		})
	}
}
