// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/logging"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestValidateL3VPNCreate tests the create logic of the L3VPN webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateL3VPNCreate(t *testing.T) {
	tcs := []struct {
		name        string
		l3vpns      []*v1alpha1.L3VPN
		l3vnis      []*v1alpha1.L3VNI
		l2vnis      []*v1alpha1.L2VNI
		underlays   []*v1alpha1.Underlay
		nodes       []*v1.Node
		newL3VPN    *v1alpha1.L3VPN
		errorString string
	}{
		{
			name: "webhook passes",
			underlays: []*v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "underlay1",
					},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
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
			newL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					VRF:       "vrfa",
					ImportRTs: []v1alpha1.RouteTarget{"65000:100"},
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
		{
			name: "webhook fails due to VNI overlap with existing L2VNI",
			underlays: []*v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "underlay1",
					},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
					},
				},
			},
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
			l2vnis: []*v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "l2vni",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI: 100,
					},
				},
			},
			newL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					VRF:              "vrfa",
					ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
					RDAssignedNumber: 100,
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
			errorString: "validation failed: duplicate VNIs found in L2VNIs for node \"node1\": L2VNI/l2vni: " +
				"duplicate vni 100:L3VPN/newL3VPN",
		},
		{
			name: "webhook fails due to no underlays",
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
			newL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					VRF:       "vrfa",
					ImportRTs: []v1alpha1.RouteTarget{"65000:100"},
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
			errorString: "validation failed: L3VPN/newL3VPN: cannot specify L3VPN configuration without an " +
				"underlay with SRV6 configuration",
		},
		{
			name: "webhook fails due to no underlays targeting same node",
			underlays: []*v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "underlay1",
					},
					Spec: v1alpha1.UnderlaySpec{
						SRV6: &v1alpha1.SRV6Config{},
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node2",
							},
						},
					},
				},
			},
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
			newL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					VRF:       "vrfa",
					ImportRTs: []v1alpha1.RouteTarget{"65000:100"},
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
			errorString: "validation failed: L3VPN/newL3VPN: cannot specify L3VPN configuration without an " +
				"underlay with SRV6 configuration",
		},
		{
			name: "testing L3VNIs and L3VPNs are mutually exclusive",
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
			l3vnis: []*v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "existingL3VNI",
					},
					Spec: v1alpha1.L3VNISpec{
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
			newL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					VRF: "0123456789abcdefghijkl",
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.4.0/24")},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "cannot create L3VPN default/newL3VPN when L3VNIs already exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			l3vpns := objectsFromResources(tc.l3vpns)
			l3vnis := objectsFromResources(tc.l3vnis)
			l2vnis := objectsFromResources(tc.l2vnis)
			underlays := objectsFromResources(tc.underlays)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l3vpns, l3vnis...)
			objects = append(objects, l2vnis...)
			objects = append(objects, nodes...)
			objects = append(objects, underlays...)
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

			err = validateL3VPNCreate(tc.newL3VPN)
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

// TestValidateL3VPNUpdate tests the update logic of the L3VPN webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound. The Update webhook has a lot
// in common with the Create webhook, so only test validation that's different, here.
func TestValidateL3VPNUpdate(t *testing.T) {
	tcs := []struct {
		name        string
		l3vpns      []*v1alpha1.L3VPN
		nodes       []*v1.Node
		newL3VPN    *v1alpha1.L3VPN
		oldL3VPN    *v1alpha1.L3VPN
		errorString string
	}{
		{
			name: "objects are the same",
			newL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
					},
				},
			},
			oldL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
					},
				},
			},
		},
		{
			name: "objects have different LocalCIDRs",
			newL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.3.0/24")},
					},
				},
			},
			oldL3VPN: &v1alpha1.L3VPN{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VPN",
				},
				Spec: v1alpha1.L3VPNSpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
					},
				},
			},
			errorString: "LocalCIDR cannot be changed",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			l3vpns := objectsFromResources(tc.l3vpns)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l3vpns, nodes...)
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

			err = validateL3VPNUpdate(tc.newL3VPN, tc.oldL3VPN)
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
