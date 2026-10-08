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

// TestValidateL2VNICreate tests the create logic of the L2VNI webhook. The goal
// is not to test each called function (functions themselves should have unit tests for that),
// but to make sure that the webhook's logic overall is sound.
func TestValidateL2VNICreate(t *testing.T) {
	tcs := []struct {
		name        string
		l2vnis      []*v1alpha1.L2VNI
		l3vnis      []*v1alpha1.L3VNI
		l3vpns      []*v1alpha1.L3VPN
		nodes       []*v1.Node
		newL2VNI    *v1alpha1.L2VNI
		errorString string
	}{
		{
			name: "webhook passes (pre-existing l3vni in same VRF)",
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
							LocalCIDRs: []string{"192.0.2.0/24"},
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
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VNI,
						L3VNI: &v1alpha1.L3VNIReference{Name: "existingL3VNI"},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
					GatewayIPs: []string{"192.0.3.0/24"},
				},
			},
		},
		// Even though this is technically not a correct configuration, the webhook should let this pass to avoid
		// order of operations issues.
		{
			name: "webhook passes (pre-existing l3vni in different VRF)",
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
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "otherL3VNI",
					},
					Spec: v1alpha1.L3VNISpec{
						VRF: "vrfb",
						VNI: 300,
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"nodeName": "node1",
							},
						},
						HostSession: &v1alpha1.HostSession{
							LocalCIDRs: []string{"192.0.2.0/24"},
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
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VNI,
						L3VNI: &v1alpha1.L3VNIReference{Name: "otherL3VNI"},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
					GatewayIPs: []string{"192.0.3.0/24"},
				},
			},
		},
		{
			name: "webhook passes (no prior resources)",
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
			name: "testing conversion.ValidateL2VNIsForNodes is hit - duplicate VNI due to L3VPN",
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
						VRF:              "existing",
						RDAssignedNumber: 100,
						ImportRTs:        []v1alpha1.RouteTarget{"65000:100"},
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
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VPN,
						L3VPN: &v1alpha1.L3VPNReference{Name: "existingL3VPN"},
					},
					VNI: 100,
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
				},
			},
			errorString: "validation failed: duplicate VNIs found in L2VNIs for node \"node1\": L2VNI/newL2VNI: duplicate vni 100:L3VPN/existingL3VPN",
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
							LocalCIDRs: []string{"192.0.2.0/24"},
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
					RoutingDomain: &v1alpha1.RoutingDomain{
						Type:  v1alpha1.RoutingDomainTypeL3VNI,
						L3VNI: &v1alpha1.L3VNIReference{Name: "existingL3VNI"},
					},
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodeName": "node1",
						},
					},
					GatewayIPs: []string{"192.0.2.0/24"},
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
			l3vpns := objectsFromResources(tc.l3vpns)
			nodes := objectsFromResources(tc.nodes)
			objects := append(l2vnis, l3vnis...)
			objects = append(objects, l3vpns...)
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
					VNI:        100,
					GatewayIPs: []string{"192.0.2.1/24"},
				},
			},
			oldL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:        100,
					GatewayIPs: []string{"192.0.2.1/24"},
				},
			},
		},
		{
			name: "GatewayIPs changed",
			newL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:        100,
					GatewayIPs: []string{"192.0.3.1/24"},
				},
			},
			oldL2VNI: &v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "newL2VNI",
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:        100,
					GatewayIPs: []string{"192.0.2.1/24"},
				},
			},
			errorString: "GatewayIPs cannot be changed",
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

func TestValidateL2VNICreateRejectsInvalidRouteTarget(t *testing.T) {
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
	client, err := setupFakeWebhookClient(objectsFromResources([]*v1.Node{node}))
	if err != nil {
		t.Fatal(err)
	}

	originalWebhookClient := WebhookClient
	originalLogger := Logger
	defer func() {
		WebhookClient = originalWebhookClient
		Logger = originalLogger
	}()
	WebhookClient = client
	Logger, _ = logging.New("debug")

	err = validateL2VNICreate(&v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "invalid-route-target", Namespace: "default"},
		Spec: v1alpha1.L2VNISpec{
			VNI:       100,
			ExportRTs: []v1alpha1.RouteTarget{"invalid"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `invalid route targets for vni "invalid-route-target"`) {
		t.Fatalf("validateL2VNICreate() error = %v, want invalid route target error", err)
	}
}

func TestL2VNIRDAssignedNumberBounds(t *testing.T) {
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
			resource := &v1alpha1.L2VNI{ObjectMeta: metav1.ObjectMeta{Name: "bounded"},
				Spec: v1alpha1.L2VNISpec{VNI: 100, RDAssignedNumber: new(number)}}
			err := validateL2VNICreate(resource)
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
			if err := validateL2VNICreate(resource); err != nil {
				t.Fatalf("omitted RD: %v", err)
			}
		})
	}
}

func TestL2VNIConfiguredRDCollisionCreateUpdate(t *testing.T) {
	number := int32(700)
	resource := &v1alpha1.L2VNI{ObjectMeta: metav1.ObjectMeta{Name: "candidate"}, Spec: v1alpha1.L2VNISpec{VNI: 100, RDAssignedNumber: new(number)}}
	other := &v1alpha1.L3VNI{ObjectMeta: metav1.ObjectMeta{Name: "existing"}, Spec: v1alpha1.L3VNISpec{VNI: 200, VRF: "other", RDAssignedNumber: new(int32(700))}}
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

	for _, err := range []error{validateL2VNICreate(resource), validateL2VNIUpdate(oldResource, resource)} {
		if err == nil {
			t.Fatal("expected configured RD collision")
		}
		for _, identity := range []string{"L2VNI/candidate", "L3VNI/existing", "700"} {
			if !strings.Contains(err.Error(), identity) {
				t.Errorf("error %v missing %s", err, identity)
			}
		}
	}
}
