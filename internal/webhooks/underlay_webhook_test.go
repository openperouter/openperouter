// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/logging"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestValidateUnderlay tests the logic of the Underlay webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateUnderlay(t *testing.T) {
	tcs := []struct {
		name        string
		underlays   []*v1alpha1.Underlay
		l3vpns      []*v1alpha1.L3VPN
		nodes       []*v1.Node
		newUnderlay *v1alpha1.Underlay
		errorString string
	}{
		{
			name: "webhook passes",
			nodes: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			newUnderlay: &v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newUnderlay",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN: 65000,
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"10.0.0.0/24"},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
					Neighbors: []v1alpha1.Neighbor{{}},
				},
			},
		},
		{
			name: "testing conversion.ValidateUnderlaysForNodes is hit - more than one underlay per node",
			nodes: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			underlays: []*v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "existingUnderlay",
					},
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"10.0.0.0/24"},
						},
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
						Neighbors: []v1alpha1.Neighbor{{}},
					},
				},
			},
			newUnderlay: &v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newUnderlay",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN: 65001,
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"10.0.1.0/24"},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "can't have more than one underlay per node",
		},
		// We do not want to block underlays with invalid configuration, only overlay resources.
		{
			name: "underlay validation passes even when l3vpns present, but the underlay's SRv6 configuration was removed",
			nodes: []*v1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			underlays: []*v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "existingUnderlay",
					},
					Spec: v1alpha1.UnderlaySpec{
						ASN: 65000,
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"10.0.0.0/24"},
						},
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
						Neighbors: []v1alpha1.Neighbor{{}},
						SRV6:      &v1alpha1.SRV6Config{},
					},
				},
			},
			l3vpns: []*v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "newL3VPN",
					},
					Spec: v1alpha1.L3VPNSpec{
						VRF: "vrfa",
						HostSession: &v1alpha1.HostSession{
							LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.3.0/24")},
						},
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
					},
				},
			},
			newUnderlay: &v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "existingUnderlay",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN: 65001,
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"10.0.1.0/24"},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
					Neighbors: []v1alpha1.Neighbor{{}},
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			underlays := objectsFromResources(tc.underlays)
			nodes := objectsFromResources(tc.nodes)
			l3vpns := objectsFromResources(tc.l3vpns)
			objects := append(underlays, nodes...)
			objects = append(objects, l3vpns...)
			client, err := setupFakeWebhookClient(objects)
			if err != nil {
				t.Fatal(err)
			}
			origWebhookClient := WebhookClient
			origLogger := Logger
			defer func() {
				WebhookClient = origWebhookClient
				Logger = origLogger
			}()
			WebhookClient = client
			Logger, _ = logging.New("debug")

			err = validateUnderlay(tc.newUnderlay)
			if tc.errorString == "" {
				if err != nil {
					t.Fatalf("expected no error, but got %q", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error to contain %q but got no error", tc.errorString)
			}
			if !strings.Contains(err.Error(), tc.errorString) {
				t.Fatalf("expected error message %q to contain substring %q", err.Error(), tc.errorString)
			}
		})
	}
}

func TestValidateCNIDeviceImmutable(t *testing.T) {
	underlayWithCNIDevice := func(ifName, rawConfig, runtimeConfig string) *v1alpha1.Underlay {
		var runtime *apiextensionsv1.JSON
		if runtimeConfig != "" {
			runtime = &apiextensionsv1.JSON{Raw: []byte(runtimeConfig)}
		}
		return &v1alpha1.Underlay{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "underlay"},
			Spec: v1alpha1.UnderlaySpec{
				Interfaces: []v1alpha1.UnderlayInterface{
					{
						Type: v1alpha1.UnderlayInterfaceTypeCNIDevice,
						CNIDevice: &v1alpha1.CNIDevice{
							Type:          v1alpha1.CNIConfigTypeRawConfig,
							RawConfig:     &apiextensionsv1.JSON{Raw: []byte(rawConfig)},
							RuntimeConfig: runtime,
							InterfaceName: new(ifName),
						},
					},
				},
			},
		}
	}
	underlayWithCNI := func(ifName, rawConfig string) *v1alpha1.Underlay {
		return underlayWithCNIDevice(ifName, rawConfig, "")
	}
	underlayWithNetworkDevice := func(ifName string) *v1alpha1.Underlay {
		return &v1alpha1.Underlay{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "underlay"},
			Spec: v1alpha1.UnderlaySpec{
				Interfaces: []v1alpha1.UnderlayInterface{
					{
						Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
						NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: ifName},
					},
				},
			},
		}
	}

	tcs := []struct {
		name        string
		oldUnderlay *v1alpha1.Underlay
		newUnderlay *v1alpha1.Underlay
		errorString string
	}{
		{
			name:        "unchanged rawConfig passes",
			oldUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
			newUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
		},
		{
			name:        "changed rawConfig is rejected",
			oldUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
			newUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"ipvlan"}`),
			errorString: "rawConfig for interface \"net1\" is immutable",
		},
		{
			name:        "new cni interface name passes",
			oldUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
			newUnderlay: underlayWithCNI("net2", `{"cniVersion":"1.0.0","type":"ipvlan"}`),
		},
		{
			name:        "switching from network device to cni passes",
			oldUnderlay: underlayWithNetworkDevice("eth0"),
			newUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
		},
		{
			name:        "switching from cni to network device passes",
			oldUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
			newUnderlay: underlayWithNetworkDevice("eth0"),
		},
		{
			name:        "unchanged runtimeConfig passes",
			oldUnderlay: underlayWithCNIDevice("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`, `{"mac":"02:00:00:00:00:01"}`),
			newUnderlay: underlayWithCNIDevice("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`, `{"mac":"02:00:00:00:00:01"}`),
		},
		{
			name:        "changed runtimeConfig is rejected",
			oldUnderlay: underlayWithCNIDevice("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`, `{"mac":"02:00:00:00:00:01"}`),
			newUnderlay: underlayWithCNIDevice("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`, `{"mac":"02:00:00:00:00:02"}`),
			errorString: "runtimeConfig for interface \"net1\" is immutable",
		},
		{
			name:        "added runtimeConfig is rejected",
			oldUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
			newUnderlay: underlayWithCNIDevice("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`, `{"mac":"02:00:00:00:00:01"}`),
			errorString: "runtimeConfig for interface \"net1\" is immutable",
		},
		{
			name:        "removed runtimeConfig is rejected",
			oldUnderlay: underlayWithCNIDevice("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`, `{"mac":"02:00:00:00:00:01"}`),
			newUnderlay: underlayWithCNI("net1", `{"cniVersion":"1.0.0","type":"macvlan"}`),
			errorString: "runtimeConfig for interface \"net1\" is immutable",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCNIDeviceImmutable(tc.oldUnderlay, tc.newUnderlay)
			if tc.errorString == "" {
				if err != nil {
					t.Fatalf("expected no error, but got %q", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error to contain %q but got no error", tc.errorString)
			}
			if !strings.Contains(err.Error(), tc.errorString) {
				t.Fatalf("expected error message %q to contain substring %q", err.Error(), tc.errorString)
			}
		})
	}
}
