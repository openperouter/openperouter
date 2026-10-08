// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"fmt"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

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
						LocalCIDRs: []string{"192.0.3.0/24"},
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
						LocalCIDRs: []string{"192.0.3.0/24"},
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
						LocalCIDRs: nil,
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
						LocalCIDRs: []string{"192.0.2.0/24"},
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
			name: "testing L3VNIs and L3VPNs are mutually exclusive per VRF",
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
						VRF:              "vrfa",
						RDAssignedNumber: 200,
						ImportRTs:        []v1alpha1.RouteTarget{"65000:200"},
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"192.0.4.0/24"},
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
						LocalCIDRs: []string{"192.0.3.0/24"},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: `L3VPN/existingL3VPN: conflict with L3VNI "default/newL3VNI" detected in VRF "vrfa"`,
		},
		{
			name: "L3VNI allowed when L3VPN exists in a different VRF",
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
						VRF:              "vrfa",
						RDAssignedNumber: 200,
						ImportRTs:        []v1alpha1.RouteTarget{"65000:200"},
					},
				},
			},
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "vrfb",
					VNI: 100,
				},
			},
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
						LocalCIDRs: []string{"192.0.2.0/24"},
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
						LocalCIDRs: []string{"192.0.2.0/24"},
					},
				},
			},
		},
		{
			name: "objects have different localCIDRs",
			newL3VNI: &v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL3VNI",
				},
				Spec: v1alpha1.L3VNISpec{
					HostSession: &v1alpha1.HostSession{
						LocalCIDRs: []string{"192.0.3.0/24"},
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
						LocalCIDRs: []string{"192.0.2.0/24"},
					},
				},
			},
			errorString: "localCIDRs cannot be changed",
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
						LocalCIDRs: []string{"192.0.3.0/24"},
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
						LocalCIDRs: []string{"192.0.3.0/24"},
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

func TestL3VNIRDAssignedNumberBounds(t *testing.T) {
	reader, err := setupFakeWebhookClient(objectsFromResources([]*v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node"}}}))
	if err != nil {
		t.Fatal(err)
	}
	oldClient, oldLogger := WebhookClient, Logger
	t.Cleanup(func() { WebhookClient, Logger = oldClient, oldLogger })
	WebhookClient = reader
	Logger, _ = logging.New("debug")
	for _, number := range []int32{-1, 0, 1, 65535, 65536} {
		t.Run(fmt.Sprint(number), func(t *testing.T) {
			resource := &v1alpha1.L3VNI{ObjectMeta: metav1.ObjectMeta{Name: "bounded"},
				Spec: v1alpha1.L3VNISpec{VNI: 100, VRF: "vni", RDAssignedNumber: new(number)}}
			err := validateL3VNICreate(resource)
			invalid := number < 1 || number > 65535
			if invalid {
				if err == nil || !strings.Contains(err.Error(), "bounded") || !strings.Contains(err.Error(), "rdAssignedNumber") {
					t.Fatalf("expected resource-identifying bounds error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			resource.Spec.RDAssignedNumber = nil
			if err := validateL3VNICreate(resource); err != nil {
				t.Fatalf("omitted RD: %v", err)
			}
		})
	}
}

func TestL3VNIConfiguredRDCollisionCreateUpdate(t *testing.T) {
	number := int32(700)
	resource := &v1alpha1.L3VNI{ObjectMeta: metav1.ObjectMeta{Name: "candidate"}, Spec: v1alpha1.L3VNISpec{VNI: 100, VRF: "vni", RDAssignedNumber: new(number)}}
	other := &v1alpha1.L2VNI{ObjectMeta: metav1.ObjectMeta{Name: "existing"}, Spec: v1alpha1.L2VNISpec{VNI: 200, RDAssignedNumber: new(int32(700))}}
	objects := []client.Object{&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}, other}

	reader, err := setupFakeWebhookClient(objects)
	if err != nil {
		t.Fatal(err)
	}
	oldClient, oldLogger := WebhookClient, Logger
	t.Cleanup(func() { WebhookClient, Logger = oldClient, oldLogger })
	WebhookClient = reader
	Logger, _ = logging.New("debug")
	oldResource := resource.DeepCopy()
	oldResource.Spec.RDAssignedNumber = nil

	for _, err := range []error{validateL3VNICreate(resource), validateL3VNIUpdate(resource, oldResource)} {
		if err == nil {
			t.Fatal("expected configured RD collision")
		}
		for _, identity := range []string{"L3VNI/candidate", "L2VNI/existing", "700"} {
			if !strings.Contains(err.Error(), identity) {
				t.Errorf("error %v missing %s", err, identity)
			}
		}
	}
}
