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

// TestValidateL3VNICreate tests the create logic of the L3VNI webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateL3VNICreate(t *testing.T) {
	tcs := []struct {
		name        string
		l3vnis      []*v1alpha1.L3VNI
		l3vpns      []*v1alpha1.L3VPN
		nodes       []*v1.Node
		newL3VNI    *v1alpha1.L3VNI
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
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
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
		{
			name: "testing conversion.ValidateL3VNIsForNodes is hit - long VRF name",
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
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "0123456789abcdefghijkl",
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
			errorString: "can't be longer than 15 characters",
		},
		{
			name: "testing conversion.ValidateHostSessionsForNodes is hit",
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
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "vrfa",
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "at least one local CIDR (IPv4 or IPv6) must be provided for vni l3vni newL3VNI",
		},
		{
			name: "testing conversion.ValidateVRFs is hit",
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
						VNI: 100,
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
					},
				},
			},
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "vrfa",
					VNI: 101,
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "more than one L3VNI detected in VRF",
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
			l3vpns: []*v1alpha1.L3VPN{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "existingL3VPN",
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
			},
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
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
			errorString: "cannot create L3VNI default/newL3VNI when L3VPNs already exist",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			l3vnis := objectsFromResources(tc.l3vnis)
			l3vpns := objectsFromResources(tc.l3vpns)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l3vnis, l3vpns...)
			objects = append(objects, nodes...)
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

			err = validateL3VNICreate(tc.newL3VNI)
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

// TestValidateL3VNIUpdate tests the update logic of the L3VNI webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound. The Update webhook has a lot
// in common with the Create webhook, so only test validation that's different, here.
func TestValidateL3VNIUpdate(t *testing.T) {
	tcs := []struct {
		name        string
		l3vnis      []*v1alpha1.L3VNI
		nodes       []*v1.Node
		newL3VNI    *v1alpha1.L3VNI
		oldL3VNI    *v1alpha1.L3VNI
		errorString string
	}{
		{
			name: "objects are the same",
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
					},
				},
			},
			oldL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
					},
				},
			},
		},
		{
			name: "objects have different LocalCIDRs",
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.3.0/24")},
					},
				},
			},
			oldL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
					},
				},
			},
			errorString: "LocalCIDR cannot be changed",
		},
		{
			name: "testing validateL3VNI is hit - long VRF name",
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
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "0123456789abcdefghijkl",
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
			oldL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
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
			errorString: "can't be longer than 15 characters",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			l3vnis := objectsFromResources(tc.l3vnis)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l3vnis, nodes...)
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

			err = validateL3VNIUpdate(tc.newL3VNI, tc.oldL3VNI)
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
