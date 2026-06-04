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

// TestValidateL3Passthrough tests the create and update logic of the L3Passthrough webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateL3Passthrough(t *testing.T) {
	tcs := []struct {
		name             string
		l3passthroughs   []*v1alpha1.L3Passthrough
		l3vnis           []*v1alpha1.L3VNI
		nodes            []*v1.Node
		newL3Passthrough *v1alpha1.L3Passthrough
		errorString      string
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
			newL3Passthrough: &v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3Passthrough",
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
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
			name: "testing conversion.ValidatePassthroughsForNodes is hit - more than one passthrough per node",
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
			l3passthroughs: []*v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "existingL3Passthrough",
					},
					Spec: v1alpha1.L3PassthroughSpec{
						HostSession: v1alpha1.HostSession{
							LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.2.0/24")},
						},
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
					},
				},
			},
			newL3Passthrough: &v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3Passthrough",
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{IPv4: new("192.0.3.0/24")},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "can't have more than one l3passthrough per node",
		},
		{
			name: "testing conversion.ValidateHostSessionsForNodes is hit - missing local CIDR",
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
			newL3Passthrough: &v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3Passthrough",
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						LocalCIDR: v1alpha1.LocalCIDRConfig{},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "at least one local CIDR (IPv4 or IPv6) must be provided",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			l3passthroughs := objectsFromResources(tc.l3passthroughs)
			l3vnis := objectsFromResources(tc.l3vnis)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l3passthroughs, l3vnis...)
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

			err = validateL3Passthrough(tc.newL3Passthrough)
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
