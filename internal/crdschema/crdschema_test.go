// SPDX-License-Identifier:Apache-2.0

package crdschema

import (
	"context"
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	underlayGVK      = schema.GroupVersionKind{Group: group, Version: version, Kind: "Underlay"}
	l2vniGVK         = schema.GroupVersionKind{Group: group, Version: version, Kind: "L2VNI"}
	l3vniGVK         = schema.GroupVersionKind{Group: group, Version: version, Kind: "L3VNI"}
	l3passthroughGVK = schema.GroupVersionKind{Group: group, Version: version, Kind: "L3Passthrough"}
	rawFRRConfigGVK  = schema.GroupVersionKind{Group: group, Version: version, Kind: "RawFRRConfig"}
)

func TestSchemaInitialization(t *testing.T) {
	gvks := KnownGVKs()

	expectedKinds := []string{"Underlay", "L2VNI", "L3VNI", "L3VPN", "L3Passthrough", "RawFRRConfig", "RouterNodeConfigurationStatus"}
	sort.Strings(expectedKinds)

	foundKinds := make([]string, 0, len(gvks))
	for _, gvk := range gvks {
		if gvk.Group != group || gvk.Version != version {
			t.Errorf("unexpected GVK group/version: %s", gvk)
			continue
		}
		foundKinds = append(foundKinds, gvk.Kind)
	}
	sort.Strings(foundKinds)

	if len(foundKinds) != len(expectedKinds) {
		t.Fatalf("expected %d kinds, got %d: %v", len(expectedKinds), len(foundKinds), foundKinds)
	}

	for i, kind := range expectedKinds {
		if foundKinds[i] != kind {
			t.Errorf("expected kind %q at index %d, got %q", kind, i, foundKinds[i])
		}
	}
}

func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		gvk      schema.GroupVersionKind
		obj      *unstructured.Unstructured
		field    string
		expected any
	}{
		{
			name:     "Underlay routerIDCIDR default",
			gvk:      underlayGVK,
			obj:      newUnstructured("Underlay", map[string]any{"asn": int64(65000)}),
			field:    "spec.routerIDCIDR",
			expected: "10.0.0.0/24",
		},
		{
			name:     "L2VNI vxlanport default",
			gvk:      l2vniGVK,
			obj:      newUnstructured("L2VNI", map[string]any{}),
			field:    "spec.vxlanPort",
			expected: int64(4789),
		},
		{
			name:     "L3VNI vxlanport default",
			gvk:      l3vniGVK,
			obj:      newUnstructured("L3VNI", map[string]any{"vrf": "testvrf"}),
			field:    "spec.vxlanPort",
			expected: int64(4789),
		},
		{
			name:     "RawFRRConfig priority default",
			gvk:      rawFRRConfigGVK,
			obj:      newUnstructured("RawFRRConfig", map[string]any{"rawConfig": "some config"}),
			field:    "spec.priority",
			expected: int64(0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ApplyDefaults(tc.obj, tc.gvk); err != nil {
				t.Fatalf("ApplyDefaults() returned error: %v", err)
			}

			val, ok := getNestedField(tc.obj, tc.field)
			if !ok {
				t.Fatalf("field %q not found after applying defaults", tc.field)
			}

			if val != tc.expected {
				t.Errorf("field %q = %v (%T), want %v (%T)", tc.field, val, val, tc.expected, tc.expected)
			}
		})
	}
}

func TestApplyDefaultsCNIInterfaceName(t *testing.T) {
	obj := newUnstructured("Underlay", map[string]any{
		"asn": int64(65000),
		"interfaces": []any{
			map[string]any{
				"type": "CNIDevice",
				"cniDevice": map[string]any{
					"type":      "RawConfig",
					"rawConfig": map[string]any{"cniVersion": "1.0.0"},
				},
			},
		},
	})

	if err := ApplyDefaults(obj, underlayGVK); err != nil {
		t.Fatalf("ApplyDefaults() returned error: %v", err)
	}

	interfaces, _, err := unstructured.NestedSlice(obj.Object, "spec", "interfaces")
	if err != nil || len(interfaces) != 1 {
		t.Fatalf("failed to get interfaces after applying defaults: %v", err)
	}
	iface, ok := interfaces[0].(map[string]any)
	if !ok {
		t.Fatalf("interface entry is not a map: %T", interfaces[0])
	}
	ifName, _, err := unstructured.NestedString(iface, "cniDevice", "interfaceName")
	if err != nil {
		t.Fatalf("failed to get cniDevice.interfaceName: %v", err)
	}
	if ifName != "net1" {
		t.Errorf("cniDevice.interfaceName = %q, want %q", ifName, "net1")
	}
}

func TestApplyDefaultsPreservation(t *testing.T) {
	tests := []struct {
		name     string
		gvk      schema.GroupVersionKind
		obj      *unstructured.Unstructured
		field    string
		expected any
	}{
		{
			name:     "Underlay explicit routerIDCIDR preserved",
			gvk:      underlayGVK,
			obj:      newUnstructured("Underlay", map[string]any{"asn": int64(65000), "routerIDCIDR": "192.168.0.0/16"}),
			field:    "spec.routerIDCIDR",
			expected: "192.168.0.0/16",
		},
		{
			name:     "L2VNI explicit vxlanport preserved",
			gvk:      l2vniGVK,
			obj:      newUnstructured("L2VNI", map[string]any{"vxlanPort": int64(5000)}),
			field:    "spec.vxlanPort",
			expected: int64(5000),
		},
		{
			name:     "L3VNI explicit vxlanport preserved",
			gvk:      l3vniGVK,
			obj:      newUnstructured("L3VNI", map[string]any{"vrf": "testvrf", "vxlanPort": int64(9999)}),
			field:    "spec.vxlanPort",
			expected: int64(9999),
		},
		{
			name:     "RawFRRConfig explicit priority preserved",
			gvk:      rawFRRConfigGVK,
			obj:      newUnstructured("RawFRRConfig", map[string]any{"rawConfig": "config", "priority": int64(50)}),
			field:    "spec.priority",
			expected: int64(50),
		},
		{
			name: "L2VNI linuxBridge lifecycle preserved",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"hostMaster": map[string]any{
					"type":        "LinuxBridge",
					"linuxBridge": map[string]any{"lifecycle": "Managed"},
				},
			}),
			field:    "spec.hostMaster.linuxBridge.lifecycle",
			expected: "Managed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ApplyDefaults(tc.obj, tc.gvk); err != nil {
				t.Fatalf("ApplyDefaults() returned error: %v", err)
			}

			val, ok := getNestedField(tc.obj, tc.field)
			if !ok {
				t.Fatalf("field %q not found after applying defaults", tc.field)
			}

			if val != tc.expected {
				t.Errorf("field %q = %v (%T), want %v (%T)", tc.field, val, val, tc.expected, tc.expected)
			}
		})
	}
}

func TestApplyDefaultsCombinedAndEdgeCases(t *testing.T) {
	t.Run("multiple defaults applied at once for L2VNI", func(t *testing.T) {
		obj := newUnstructured("L2VNI", map[string]any{
			"hostMaster": map[string]any{
				"type":        "LinuxBridge",
				"linuxBridge": map[string]any{"lifecycle": "External", "name": "br0"},
			},
		})
		if err := ApplyDefaults(obj, l2vniGVK); err != nil {
			t.Fatalf("ApplyDefaults() returned error: %v", err)
		}

		val, ok := getNestedField(obj, "spec.vxlanPort")
		if !ok {
			t.Fatal("spec.vxlanPort not found")
		}
		if val != int64(4789) {
			t.Errorf("spec.vxlanPort = %v, want %v", val, int64(4789))
		}

		val, ok = getNestedField(obj, "spec.hostMaster.linuxBridge.lifecycle")
		if !ok {
			t.Fatal("spec.hostMaster.linuxBridge.lifecycle not found")
		}
		if val != "External" {
			t.Errorf("spec.hostMaster.linuxBridge.lifecycle = %v, want External", val)
		}
	})

	t.Run("empty spec gets defaults", func(t *testing.T) {
		obj := newUnstructured("Underlay", map[string]any{})
		if err := ApplyDefaults(obj, underlayGVK); err != nil {
			t.Fatalf("ApplyDefaults() returned error: %v", err)
		}

		val, ok := getNestedField(obj, "spec.routerIDCIDR")
		if !ok {
			t.Fatal("spec.routerIDCIDR not found")
		}
		if val != "10.0.0.0/24" {
			t.Errorf("spec.routerIDCIDR = %v, want 10.0.0.0/24", val)
		}
	})

	t.Run("idempotency", func(t *testing.T) {
		obj := newUnstructured("L2VNI", map[string]any{})

		if err := ApplyDefaults(obj, l2vniGVK); err != nil {
			t.Fatalf("first ApplyDefaults() returned error: %v", err)
		}

		firstVal, _ := getNestedField(obj, "spec.vxlanPort")

		if err := ApplyDefaults(obj, l2vniGVK); err != nil {
			t.Fatalf("second ApplyDefaults() returned error: %v", err)
		}

		secondVal, _ := getNestedField(obj, "spec.vxlanPort")
		if firstVal != secondVal {
			t.Errorf("idempotency broken: first=%v, second=%v", firstVal, secondVal)
		}
	})

	t.Run("unknown GVK returns error", func(t *testing.T) {
		unknownGVK := schema.GroupVersionKind{Group: "unknown.io", Version: "v1", Kind: "Unknown"}
		obj := newUnstructured("Unknown", map[string]any{})
		err := ApplyDefaults(obj, unknownGVK)
		if err == nil {
			t.Fatal("expected error for unknown GVK, got nil")
		}
		if !strings.Contains(err.Error(), "no CRD schema found") {
			t.Errorf("expected error to contain 'no CRD schema found', got: %v", err)
		}
	})
}

func TestValidateSuccessful(t *testing.T) {
	tests := []struct {
		name string
		gvk  schema.GroupVersionKind
		obj  *unstructured.Unstructured
	}{
		{
			name: "Underlay with tunnel endpoint",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type":          "NetworkDevice",
						"networkDevice": map[string]any{"interfaceName": "eth0"},
					},
				},
				"tunnelEndpoint": map[string]any{"cidrs": []any{"10.10.0.0/24"}},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
		},
		{
			name: "Underlay Neighbor without hostasn",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type":          "NetworkDevice",
						"networkDevice": map[string]any{"interfaceName": "eth0"},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
		},
		{
			name: "Underlay Neighbor with different hostasn and asn",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type":          "NetworkDevice",
						"networkDevice": map[string]any{"interfaceName": "eth0"},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
						"hostASN": int64(65002),
					},
				},
			}),
		},
		{
			name: "Underlay with CNI interface",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type": "CNIDevice",
						"cniDevice": map[string]any{
							"type": "RawConfig",
							"rawConfig": map[string]any{
								"cniVersion": "1.0.0",
								"name":       "macvlan-underlay",
								"plugins": []any{
									map[string]any{
										"type":   "macvlan",
										"master": "eth1",
										"mode":   "bridge",
									},
								},
							},
							"runtimeConfig": map[string]any{
								"mac": "02:42:c0:a8:01:0a",
							},
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
		},
		{
			name: "Underlay NetworkDevice with empty acceleratedConfig",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type": "NetworkDevice",
						"networkDevice": map[string]any{
							"interfaceName":     "enp3s0f0v0",
							"acceleratedConfig": map[string]any{},
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
		},
		{
			name: "Underlay NetworkDevice with acceleratedConfig port options",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type": "NetworkDevice",
						"networkDevice": map[string]any{
							"interfaceName": "enp3s0f0v0",
							"acceleratedConfig": map[string]any{
								"rxQueues":    int64(4),
								"qSize":       int64(1024),
								"promiscuous": false,
							},
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
		},
		{
			name: "L2VNI with LinuxBridge External lifecycle and name set",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"vni": int64(100),
				"hostMaster": map[string]any{
					"type": "LinuxBridge",
					"linuxBridge": map[string]any{
						"name":      "br0",
						"lifecycle": "External",
					},
				},
			}),
		},
		{
			name: "L2VNI with LinuxBridge Managed lifecycle and no name",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"vni": int64(100),
				"hostMaster": map[string]any{
					"type": "LinuxBridge",
					"linuxBridge": map[string]any{
						"lifecycle": "Managed",
					},
				},
			}),
		},
		{
			name: "L2VNI HostMaster type linux-bridge with linuxBridge field",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"vni": int64(100),
				"hostMaster": map[string]any{
					"type": "LinuxBridge",
					"linuxBridge": map[string]any{
						"lifecycle": "Managed",
					},
				},
			}),
		},
		{
			name: "L3VNI without hostsession",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"vni": int64(200),
			}),
		},
		{
			name: "L3VNI with different hostsession ASNs",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"vni": int64(200),
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"10.0.0.0/30"},
				},
			}),
		},
		{
			name: "valid L3Passthrough",
			gvk:  l3passthroughGVK,
			obj: newUnstructured("L3Passthrough", map[string]any{
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"10.0.0.0/30"},
				},
			}),
		},
		{
			name: "valid RawFRRConfig",
			gvk:  rawFRRConfigGVK,
			obj: newUnstructured("RawFRRConfig", map[string]any{
				"rawConfig": "router bgp 65000",
				"priority":  int64(10),
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ApplyDefaults(tc.obj, tc.gvk); err != nil {
				t.Fatalf("ApplyDefaults() returned error: %v", err)
			}

			errs := Validate(context.Background(), tc.obj, tc.gvk)
			if len(errs) > 0 {
				t.Errorf("expected no validation errors, got %d:", len(errs))
				for _, e := range errs {
					t.Errorf("  - %s", e)
				}
			}
		})
	}
}

func TestValidateFailure(t *testing.T) {
	tests := []struct {
		name      string
		gvk       schema.GroupVersionKind
		obj       *unstructured.Unstructured
		errSubstr string
	}{
		{
			name: "LinuxBridge with both name and Managed lifecycle",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"hostMaster": map[string]any{
					"type": "LinuxBridge",
					"linuxBridge": map[string]any{
						"name":      "br0",
						"lifecycle": "Managed",
					},
				},
			}),
			errSubstr: "name must be set when lifecycle is External, and must not be set when it is Managed.",
		},
		{
			name: "LinuxBridge with External lifecycle and no name",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"hostMaster": map[string]any{
					"type": "LinuxBridge",
					"linuxBridge": map[string]any{
						"lifecycle": "External",
					},
				},
			}),
			errSubstr: "name must be set when lifecycle is External, and must not be set when it is Managed.",
		},
		{
			name: "OVSBridge with both name and Managed lifecycle",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"hostMaster": map[string]any{
					"type": "OVSBridge",
					"ovsBridge": map[string]any{
						"name":      "br0",
						"lifecycle": "Managed",
					},
				},
			}),
			errSubstr: "name must be set when lifecycle is External, and must not be set when it is Managed.",
		},
		{
			name: "HostMaster type linux-bridge with ovsBridge field",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"hostMaster": map[string]any{
					"type": "LinuxBridge",
					"ovsBridge": map[string]any{
						"lifecycle": "Managed",
					},
				},
			}),
			errSubstr: "type/config mismatch",
		},
		{
			name: "HostMaster type ovs-bridge with linuxBridge field",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"hostMaster": map[string]any{
					"type": "OVSBridge",
					"linuxBridge": map[string]any{
						"lifecycle": "Managed",
					},
				},
			}),
			errSubstr: "type/config mismatch",
		},
		{
			name: "Underlay interface type CNI without cniDevice",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type": "CNIDevice",
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
			errSubstr: "cniDevice must be set if and only if type is 'CNIDevice'",
		},
		{
			name: "Underlay interface type NetworkDevice with cniDevice",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type":          "NetworkDevice",
						"networkDevice": map[string]any{"interfaceName": "eth0"},
						"cniDevice": map[string]any{
							"type":      "RawConfig",
							"rawConfig": map[string]any{"cniVersion": "1.0.0"},
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
			errSubstr: "cniDevice must be set if and only if type is 'CNIDevice'",
		},
		{
			name: "Underlay mixing NetworkDevice and CNIDevice interfaces",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type":          "NetworkDevice",
						"networkDevice": map[string]any{"interfaceName": "eth0"},
					},
					map[string]any{
						"type": "CNIDevice",
						"cniDevice": map[string]any{
							"type":      "RawConfig",
							"rawConfig": map[string]any{"cniVersion": "1.0.0"},
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
			errSubstr: "all interfaces must be of the same type",
		},
		{
			name: "Underlay CNI device type RawConfig without rawConfig",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type": "CNIDevice",
						"cniDevice": map[string]any{
							"type": "RawConfig",
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
			errSubstr: "rawConfig must be set if and only if type is 'RawConfig'",
		},
		{
			// The interfaces "same type" CEL rule indexes self[0]: ensure an
			// empty list fails with the MinItems error instead of a CEL
			// runtime error (all() is vacuously true on empty lists).
			name: "Underlay with empty interfaces list",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn":        int64(65000),
				"interfaces": []any{},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
			errSubstr: "should have at least 1 items",
		},
		{
			name: "ConnectTimeSeconds below minimum (0)",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"neighbors": []any{
					map[string]any{
						"address":            "192.168.1.1",
						"asn":                int64(65001),
						"connectTimeSeconds": int64(0),
					},
				},
			}),
			errSubstr: "should be greater than or equal to 1",
		},
		{
			name: "ConnectTimeSeconds above maximum (65536)",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"neighbors": []any{
					map[string]any{
						"address":            "192.168.1.1",
						"asn":                int64(65001),
						"connectTimeSeconds": int64(65536),
					},
				},
			}),
			errSubstr: "should be less than or equal to 65535",
		},
		{
			name: "L3VNI hostSession localCIDRs invalid CIDR",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"vni": int64(200),
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"not-a-cidr"},
				},
			}),
			errSubstr: "all entries must be valid CIDRs",
		},
		{
			name: "L3VNI hostSession localCIDRs two IPv4 CIDRs",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"vni": int64(200),
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"10.0.0.0/24", "10.1.0.0/24"},
				},
			}),
			errSubstr: "at most one IPv4 CIDR is allowed",
		},
		{
			name: "L3VNI hostSession localCIDRs two IPv6 CIDRs",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"vni": int64(200),
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"fd00::/64", "fd01::/64"},
				},
			}),
			errSubstr: "at most one IPv6 CIDR is allowed",
		},
		{
			name: "L3VNI hostSession localCIDRs empty list",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"vni": int64(200),
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{},
				},
			}),
			errSubstr: "should have at least 1 items",
		},
		{
			name: "L3VNI hostSession localCIDRs more than two entries",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"vni": int64(200),
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"10.0.0.0/24", "fd00::/64", "10.1.0.0/24"},
				},
			}),
			errSubstr: "must have at most 2 items",
		},
		{
			name: "Underlay acceleratedConfig rxQueues below minimum",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type": "NetworkDevice",
						"networkDevice": map[string]any{
							"interfaceName": "enp3s0f0v0",
							"acceleratedConfig": map[string]any{
								"rxQueues": int64(0),
							},
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
			errSubstr: "should be greater than or equal to 1",
		},
		{
			name: "Underlay acceleratedConfig qSize below minimum",
			gvk:  underlayGVK,
			obj: newUnstructured("Underlay", map[string]any{
				"asn": int64(65000),
				"interfaces": []any{
					map[string]any{
						"type": "NetworkDevice",
						"networkDevice": map[string]any{
							"interfaceName": "enp3s0f0v0",
							"acceleratedConfig": map[string]any{
								"qSize": int64(32),
							},
						},
					},
				},
				"neighbors": []any{
					map[string]any{
						"address": "192.168.1.1",
						"asn":     int64(65001),
					},
				},
			}),
			errSubstr: "should be greater than or equal to 64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ApplyDefaults(tc.obj, tc.gvk); err != nil {
				t.Fatalf("ApplyDefaults() returned error: %v", err)
			}

			errs := Validate(context.Background(), tc.obj, tc.gvk)
			if len(errs) == 0 {
				t.Fatal("expected validation errors, got none")
			}

			found := false
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.errSubstr) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got:", tc.errSubstr)
				for _, e := range errs {
					t.Errorf("  - %s", e)
				}
			}
		})
	}
}

func TestValidateOldSelfFiltering(t *testing.T) {
	tests := []struct {
		name     string
		gvk      schema.GroupVersionKind
		obj      *unstructured.Unstructured
		errField string
	}{
		{
			name: "L2VNI with gatewayIPs does not trigger oldSelf error",
			gvk:  l2vniGVK,
			obj: newUnstructured("L2VNI", map[string]any{
				"gatewayIPs": []any{"10.0.0.1/24"},
				"routingDomain": map[string]any{
					"type": "L3VNI",
					"l3vni": map[string]any{
						"name": "test-l3vni",
					},
				},
			}),
			errField: "GatewayIPs cannot be changed",
		},
		{
			name: "L3VNI with hostsession localcidr does not trigger oldSelf error",
			gvk:  l3vniGVK,
			obj: newUnstructured("L3VNI", map[string]any{
				"vrf": "testvrf",
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"10.0.0.0/30"},
				},
			}),
			errField: "localCIDRs can't be changed",
		},
		{
			name: "L3Passthrough with hostsession localcidr does not trigger oldSelf error",
			gvk:  l3passthroughGVK,
			obj: newUnstructured("L3Passthrough", map[string]any{
				"hostSession": map[string]any{
					"asn":        int64(65000),
					"hostASN":    int64(65001),
					"localCIDRs": []any{"10.0.0.0/30"},
				},
			}),
			errField: "localCIDRs can't be changed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ApplyDefaults(tc.obj, tc.gvk); err != nil {
				t.Fatalf("ApplyDefaults() returned error: %v", err)
			}

			errs := Validate(context.Background(), tc.obj, tc.gvk)
			for _, e := range errs {
				if strings.Contains(e.Error(), tc.errField) {
					t.Errorf("unexpected oldSelf error: %s", e)
				}
			}
		})
	}
}

func TestValidateMultipleErrors(t *testing.T) {
	t.Run("LinuxBridge both name and Managed lifecycle plus type mismatch", func(t *testing.T) {
		obj := newUnstructured("L2VNI", map[string]any{
			"hostMaster": map[string]any{
				"type": "OVSBridge",
				"linuxBridge": map[string]any{
					"name":      "br0",
					"lifecycle": "Managed",
				},
			},
		})

		if err := ApplyDefaults(obj, l2vniGVK); err != nil {
			t.Fatalf("ApplyDefaults() returned error: %v", err)
		}

		errs := Validate(context.Background(), obj, l2vniGVK)
		if len(errs) < 2 {
			t.Errorf("expected at least 2 validation errors, got %d:", len(errs))
			for _, e := range errs {
				t.Errorf("  - %s", e)
			}
		}

		foundNameLifecycle := false
		foundMismatch := false
		for _, e := range errs {
			msg := e.Error()
			if strings.Contains(msg, "name must be set when lifecycle is External, and must not be set when it is Managed.") {
				foundNameLifecycle = true
			}
			if strings.Contains(msg, "type/config mismatch") {
				foundMismatch = true
			}
		}
		if !foundNameLifecycle {
			t.Error("missing name/lifecycle validation error")
		}
		if !foundMismatch {
			t.Error("missing type/config mismatch validation error")
		}
	})
}

func TestValidateErrorCases(t *testing.T) {
	t.Run("Validate() returns no errors for unknown GVK (no validator available)", func(t *testing.T) {
		unknownGVK := schema.GroupVersionKind{Group: "unknown.io", Version: "v1", Kind: "Unknown"}
		obj := newUnstructured("Unknown", map[string]any{})

		errs := Validate(context.Background(), obj, unknownGVK)
		if len(errs) != 0 {
			t.Errorf("expected no errors for unknown GVK, got %d", len(errs))
		}
	})

	t.Run("malformed unstructured with nil spec", func(t *testing.T) {
		obj := newUnstructured("Underlay", nil)

		if err := ApplyDefaults(obj, underlayGVK); err != nil {
			t.Fatalf("ApplyDefaults() returned error: %v", err)
		}

		errs := Validate(context.Background(), obj, underlayGVK)
		if len(errs) == 0 {
			t.Fatal("expected validation errors for nil spec, got none")
		}
		if !strings.Contains(errs[0].Error(), "spec is required") {
			t.Errorf("expected 'spec is required' error, got: %v", errs)
		}
	})

	t.Run("object with only metadata runs without panic", func(t *testing.T) {
		obj := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": group + "/" + version,
				"kind":       "L2VNI",
				"metadata": map[string]any{
					"name":      "test",
					"namespace": "default",
				},
			},
		}

		if err := ApplyDefaults(obj, l2vniGVK); err != nil {
			t.Fatalf("ApplyDefaults() returned error: %v", err)
		}

		errs := Validate(context.Background(), obj, l2vniGVK)
		if len(errs) == 0 {
			t.Fatal("expected validation errors for metadata-only object, got none")
		}
		if !strings.Contains(errs[0].Error(), "spec is required") {
			t.Errorf("expected 'spec is required' error, got: %v", errs)
		}
	})
}

func newUnstructured(kind string, spec map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": group + "/" + version,
			"kind":       kind,
			"metadata": map[string]any{
				"name":      "test",
				"namespace": "default",
			},
		},
	}
	if spec != nil {
		obj.Object["spec"] = spec
	}
	return obj
}

func getNestedField(obj *unstructured.Unstructured, path string) (any, bool) {
	parts := strings.Split(path, ".")
	current := obj.Object
	for i, part := range parts {
		if i == len(parts)-1 {
			val, ok := current[part]
			return val, ok
		}
		next, ok := current[part]
		if !ok {
			return nil, false
		}
		nextMap, ok := next.(map[string]any)
		if !ok {
			return nil, false
		}
		current = nextMap
	}
	return nil, false
}
