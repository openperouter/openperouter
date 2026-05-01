// SPDX-License-Identifier:Apache-2.0

package crdschema

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

const (
	group   = "openpe.openperouter.github.io"
	version = "v1alpha1"
)

func TestParityDefaults(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		input    any
		validate func(t *testing.T, u *unstructured.Unstructured)
	}{
		{
			name: "Underlay minimal gets RouterIDCIDR default",
			kind: "Underlay",
			input: &v1alpha1.Underlay{
				TypeMeta: metav1.TypeMeta{Kind: "Underlay", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-underlay",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN: 65000,
					Neighbors: []v1alpha1.Neighbor{
						{ASN: 65001, Address: "192.168.1.1"},
					},
				},
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				result := unstructuredToTyped[v1alpha1.Underlay](t, u)
				if result.Spec.RouterIDCIDR != "10.0.0.0/24" {
					t.Errorf("expected RouterIDCIDR=10.0.0.0/24, got %q", result.Spec.RouterIDCIDR)
				}
			},
		},
		{
			name: "L2VNI minimal with linuxBridge gets VXLanPort and AutoCreate defaults",
			kind: "L2VNI",
			input: &v1alpha1.L2VNI{
				TypeMeta: metav1.TypeMeta{Kind: "L2VNI", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-l2vni",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 100,
					HostMaster: &v1alpha1.HostMaster{
						Type: v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{
							Name: "br0",
						},
					},
				},
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				result := unstructuredToTyped[v1alpha1.L2VNI](t, u)
				if result.Spec.VXLanPort != 4789 {
					t.Errorf("expected VXLanPort=4789, got %d", result.Spec.VXLanPort)
				}
				if result.Spec.HostMaster == nil || result.Spec.HostMaster.LinuxBridge == nil {
					t.Fatal("expected HostMaster.LinuxBridge to be non-nil")
				}
				if result.Spec.HostMaster.LinuxBridge.AutoCreate != false {
					t.Errorf("expected LinuxBridge.AutoCreate=false, got %v", result.Spec.HostMaster.LinuxBridge.AutoCreate)
				}
			},
		},
		{
			name: "L3VNI minimal gets VXLanPort default",
			kind: "L3VNI",
			input: &v1alpha1.L3VNI{
				TypeMeta: metav1.TypeMeta{Kind: "L3VNI", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-l3vni",
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "testvrf",
					VNI: 200,
				},
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				result := unstructuredToTyped[v1alpha1.L3VNI](t, u)
				if result.Spec.VXLanPort != 4789 {
					t.Errorf("expected VXLanPort=4789, got %d", result.Spec.VXLanPort)
				}
			},
		},
		{
			name: "RawFRRConfig minimal gets Priority default",
			kind: "RawFRRConfig",
			input: &v1alpha1.RawFRRConfig{
				TypeMeta: metav1.TypeMeta{Kind: "RawFRRConfig", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-rawfrrconfig",
				},
				Spec: v1alpha1.RawFRRConfigSpec{
					RawConfig: "router bgp 65000",
				},
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				result := unstructuredToTyped[v1alpha1.RawFRRConfig](t, u)
				if result.Spec.Priority != 0 {
					t.Errorf("expected Priority=0, got %d", result.Spec.Priority)
				}
			},
		},
		{
			name: "L2VNI full - all fields set - no fields changed",
			kind: "L2VNI",
			input: &v1alpha1.L2VNI{
				TypeMeta: metav1.TypeMeta{Kind: "L2VNI", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "full-l2vni",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:       500,
					VXLanPort: 5000,
					VRF:       ptr.To("myvrf"),
					HostMaster: &v1alpha1.HostMaster{
						Type: v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{
							Name: "mybridge",
						},
					},
					L2GatewayIPs: []string{"10.10.10.1/24"},
				},
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				result := unstructuredToTyped[v1alpha1.L2VNI](t, u)
				if result.Spec.VXLanPort != 5000 {
					t.Errorf("expected VXLanPort=5000 (unchanged), got %d", result.Spec.VXLanPort)
				}
				if result.Spec.HostMaster.LinuxBridge.Name != "mybridge" {
					t.Errorf("expected LinuxBridge.Name=mybridge, got %q", result.Spec.HostMaster.LinuxBridge.Name)
				}
			},
		},
		{
			name: "Underlay full - all fields set - no fields changed",
			kind: "Underlay",
			input: &v1alpha1.Underlay{
				TypeMeta: metav1.TypeMeta{Kind: "Underlay", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "full-underlay",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:          65100,
					RouterIDCIDR: "172.16.0.0/16",
					Neighbors: []v1alpha1.Neighbor{
						{ASN: 65200, Address: "10.0.0.1"},
					},
					Nics: []string{"eth0"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "10.100.0.0/24",
					},
				},
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				result := unstructuredToTyped[v1alpha1.Underlay](t, u)
				if result.Spec.RouterIDCIDR != "172.16.0.0/16" {
					t.Errorf("expected RouterIDCIDR=172.16.0.0/16 (unchanged), got %q", result.Spec.RouterIDCIDR)
				}
				if result.Spec.ASN != 65100 {
					t.Errorf("expected ASN=65100 (unchanged), got %d", result.Spec.ASN)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := typedToUnstructured(t, tc.input)
			if err := ApplyDefaults(u, parityGVK(tc.kind)); err != nil {
				t.Fatalf("ApplyDefaults failed: %v", err)
			}
			tc.validate(t, u)
		})
	}
}

func TestParityValidation(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		input       any
		wantMessage string
	}{
		{
			name: "EVPN with both VTEPs",
			kind: "Underlay",
			input: &v1alpha1.Underlay{
				TypeMeta: metav1.TypeMeta{Kind: "Underlay", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-underlay",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:          65000,
					RouterIDCIDR: "10.0.0.0/24",
					Neighbors: []v1alpha1.Neighbor{
						{ASN: 65001, Address: "192.168.1.1"},
					},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR:      "10.200.0.0/24",
						VTEPInterface: "eth1",
					},
				},
			},
			wantMessage: "exactly one of vtepcidr or vtepInterface must be specified",
		},
		{
			name: "L2VNI linuxBridge with name and autoCreate=true",
			kind: "L2VNI",
			input: &v1alpha1.L2VNI{
				TypeMeta: metav1.TypeMeta{Kind: "L2VNI", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name: "invalid-l2vni",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:       400,
					VXLanPort: 4789,
					HostMaster: &v1alpha1.HostMaster{
						Type: v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{
							Name:       "mybridge",
							AutoCreate: true,
						},
					},
				},
			},
			wantMessage: "either name must be set or autoCreate must be true, but not both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := typedToUnstructured(t, tc.input)
			if err := ApplyDefaults(u, parityGVK(tc.kind)); err != nil {
				t.Fatalf("ApplyDefaults failed: %v", err)
			}

			errs := Validate(context.Background(), u, parityGVK(tc.kind))
			if len(errs) == 0 {
				t.Fatal("expected validation errors, got none")
			}

			found := false
			var messages []string
			for _, e := range errs {
				messages = append(messages, e.Detail)
				if strings.Contains(e.Detail, tc.wantMessage) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got: %v", tc.wantMessage, messages)
			}
		})
	}
}

func TestParityRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		input any
	}{
		{
			name: "Underlay round-trip with pointer types",
			kind: "Underlay",
			input: &v1alpha1.Underlay{
				TypeMeta: metav1.TypeMeta{Kind: "Underlay", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rt-underlay",
					Namespace: "default",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:          65100,
					RouterIDCIDR: "172.16.0.0/16",
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:          65200,
							Address:      "10.0.0.1",
							Port:         ptr.To[uint16](179),
							Password:     "secret",
							HoldTime:     &metav1.Duration{Duration: 90 * time.Second},
							EBGPMultiHop: true,
							BFD: &v1alpha1.BFDSettings{
								ReceiveInterval:  ptr.To[uint32](300),
								TransmitInterval: ptr.To[uint32](300),
								DetectMultiplier: ptr.To[uint32](3),
								EchoInterval:     ptr.To[uint32](50),
								EchoMode:         ptr.To(false),
								PassiveMode:      ptr.To(true),
								MinimumTTL:       ptr.To[uint32](254),
							},
						},
					},
					Nics: []string{"eth0", "eth1"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "10.100.0.0/24",
					},
				},
			},
		},
		{
			name: "L2VNI round-trip",
			kind: "L2VNI",
			input: &v1alpha1.L2VNI{
				TypeMeta: metav1.TypeMeta{Kind: "L2VNI", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rt-l2vni",
					Namespace: "default",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:       1000,
					VXLanPort: 4789,
					VRF:       ptr.To("myvrf"),
					HostMaster: &v1alpha1.HostMaster{
						Type: v1alpha1.LinuxBridge,
						LinuxBridge: &v1alpha1.LinuxBridgeConfig{
							Name:       "mybridge",
							AutoCreate: false,
						},
					},
					L2GatewayIPs: []string{"10.10.10.1/24", "fd00::1/64"},
				},
			},
		},
		{
			name: "L3VNI round-trip",
			kind: "L3VNI",
			input: &v1alpha1.L3VNI{
				TypeMeta: metav1.TypeMeta{Kind: "L3VNI", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rt-l3vni",
					Namespace: "default",
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       "l3vrf",
					VNI:       2000,
					VXLanPort: 4789,
					HostSession: &v1alpha1.HostSession{
						ASN:     65000,
						HostASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.0.0/24",
							IPv6: "fd00::/64",
						},
					},
				},
			},
		},
		{
			name: "L3Passthrough round-trip",
			kind: "L3Passthrough",
			input: &v1alpha1.L3Passthrough{
				TypeMeta: metav1.TypeMeta{Kind: "L3Passthrough", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rt-l3passthrough",
					Namespace: "default",
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN:     65000,
						HostASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.0.0/24",
						},
					},
				},
			},
		},
		{
			name: "RawFRRConfig round-trip",
			kind: "RawFRRConfig",
			input: &v1alpha1.RawFRRConfig{
				TypeMeta: metav1.TypeMeta{Kind: "RawFRRConfig", APIVersion: group + "/" + version},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rt-rawfrrconfig",
					Namespace: "default",
				},
				Spec: v1alpha1.RawFRRConfigSpec{
					Priority:  5,
					RawConfig: "router bgp 65000\n neighbor 10.0.0.1 remote-as 65001",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := typedToUnstructured(t, tc.input)

			if err := ApplyDefaults(u, parityGVK(tc.kind)); err != nil {
				t.Fatalf("ApplyDefaults failed: %v", err)
			}

			switch tc.kind {
			case "Underlay":
				original := tc.input.(*v1alpha1.Underlay)
				result := unstructuredToTyped[v1alpha1.Underlay](t, u)
				if diff := cmp.Diff(*original, result); diff != "" {
					t.Errorf("round-trip mismatch (-original +after):\n%s", diff)
				}
			case "L2VNI":
				original := tc.input.(*v1alpha1.L2VNI)
				result := unstructuredToTyped[v1alpha1.L2VNI](t, u)
				if diff := cmp.Diff(*original, result); diff != "" {
					t.Errorf("round-trip mismatch (-original +after):\n%s", diff)
				}
			case "L3VNI":
				original := tc.input.(*v1alpha1.L3VNI)
				result := unstructuredToTyped[v1alpha1.L3VNI](t, u)
				if diff := cmp.Diff(*original, result); diff != "" {
					t.Errorf("round-trip mismatch (-original +after):\n%s", diff)
				}
			case "L3Passthrough":
				original := tc.input.(*v1alpha1.L3Passthrough)
				result := unstructuredToTyped[v1alpha1.L3Passthrough](t, u)
				if diff := cmp.Diff(*original, result); diff != "" {
					t.Errorf("round-trip mismatch (-original +after):\n%s", diff)
				}
			case "RawFRRConfig":
				original := tc.input.(*v1alpha1.RawFRRConfig)
				result := unstructuredToTyped[v1alpha1.RawFRRConfig](t, u)
				if diff := cmp.Diff(*original, result); diff != "" {
					t.Errorf("round-trip mismatch (-original +after):\n%s", diff)
				}
			default:
				t.Fatalf("unknown kind %q", tc.kind)
			}
		})
	}
}

func parityGVK(kind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind,
	}
}

func typedToUnstructured(t *testing.T, obj any) *unstructured.Unstructured {
	t.Helper()
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		t.Fatalf("failed to convert to unstructured: %v", err)
	}
	return &unstructured.Unstructured{Object: m}
}

func unstructuredToTyped[T any](t *testing.T, u *unstructured.Unstructured) T {
	t.Helper()
	var result T
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &result); err != nil {
		t.Fatalf("failed to convert from unstructured: %v", err)
	}
	return result
}
