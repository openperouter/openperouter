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

// TestValidateUnderlay tests the logic of the Underlay webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateUnderlay(t *testing.T) {
	tcs := []struct {
		name        string
		underlays   []*v1alpha1.Underlay
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
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: new("10.0.0.0/24"),
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
						EVPN: &v1alpha1.EVPNConfig{
							VTEPCIDR: new("10.0.0.0/24"),
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
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: new("10.0.1.0/24"),
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
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			underlays := objectsFromResources(tc.underlays)
			nodes := objectsFromResources(tc.nodes)
			objects := append(underlays, nodes...)
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
