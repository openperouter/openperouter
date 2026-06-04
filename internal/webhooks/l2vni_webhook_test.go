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

// TestValidateL2VNICreate tests the create logic of the L2VNI webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateL2VNICreate(t *testing.T) {
	tcs := []struct {
		name        string
		l2vnis      []*v1alpha1.L2VNI
		l3vnis      []*v1alpha1.L3VNI
		nodes       []*v1.Node
		newL2VNI    *v1alpha1.L2VNI
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
			l3vnis: []*v1alpha1.L3VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "existingL3VNI",
					},
					Spec: v1alpha1.L3VNISpec{
						VRF: "vrfa",
						VNI: 200,
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
						HostSession: &v1alpha1.HostSession{
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.0.2.0/24"),
							},
						},
					},
				},
			},
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 100,
					VRF: new("vrfb"),
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
					L2GatewayIPs: []string{"192.0.2.0/24"},
				},
			},
		},
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
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 100,
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
		},
		{
			name: "testing conversion.ValidateL2VNIsForNodes is hit - duplicate VNI",
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
						Name:      "existingL2VNI",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI: 100,
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
					},
				},
			},
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 100,
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "duplicate vni",
		},
		{
			name: "testing conversion.ValidateVRFsForNodes is hit - subnet overlap in VRF",
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
						VNI: 200,
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
						HostSession: &v1alpha1.HostSession{
							LocalCIDR: v1alpha1.LocalCIDRConfig{
								IPv4: new("192.0.2.0/24"),
							},
						},
					},
				},
			},
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 100,
					VRF: new("vrfa"),
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
					L2GatewayIPs: []string{"192.0.2.0/24"},
				},
			},
			errorString: "subnet overlap in VRF \"vrfa\": " +
				"IPNet 192.0.2.0/24 (L3VNI default/existingL3VNI) overlaps with IPNet " +
				"192.0.2.0/24 (L2VNI default/newL2VNI)",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			l2vnis := objectsFromResources(tc.l2vnis)
			l3vnis := objectsFromResources(tc.l3vnis)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l2vnis, l3vnis...)
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

			err = validateL2VNICreate(tc.newL2VNI)
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

// TestValidateL2VNIUpdate tests the update logic of the L2VNI webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound. The Update webhook has a lot
// in common with the Create webhook, so only test validation that's different, here.
func TestValidateL2VNIUpdate(t *testing.T) {
	tcs := []struct {
		name        string
		l2vnis      []*v1alpha1.L2VNI
		nodes       []*v1.Node
		oldL2VNI    *v1alpha1.L2VNI
		newL2VNI    *v1alpha1.L2VNI
		errorString string
	}{
		{
			name: "objects are the same",
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:          100,
					L2GatewayIPs: []string{"192.0.2.1/24"},
				},
			},
			oldL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:          100,
					L2GatewayIPs: []string{"192.0.2.1/24"},
				},
			},
		},
		{
			name: "L2GatewayIPs changed",
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:          100,
					L2GatewayIPs: []string{"192.0.3.1/24"},
				},
			},
			oldL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:          100,
					L2GatewayIPs: []string{"192.0.2.1/24"},
				},
			},
			errorString: "L2GatewayIPs cannot be changed",
		},
		{
			name: "testing validateL2VNI is hit - duplicate VNI",
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
						Name:      "existingL2VNI",
					},
					Spec: v1alpha1.L2VNISpec{
						VNI: 100,
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
					},
				},
			},
			oldL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "updatedL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 100,
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "updatedL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI: 100,
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "duplicate vni",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			l2vnis := objectsFromResources(tc.l2vnis)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l2vnis, nodes...)
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

			err = validateL2VNIUpdate(tc.oldL2VNI, tc.newL2VNI)
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
