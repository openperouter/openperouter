// SPDX-License-Identifier: Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolvePasswordSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name              string
		neighbors         []v1alpha1.Neighbor
		secrets           []corev1.Secret
		wantPassword      string
		wantNeighborCount int
		wantResourceErr   bool
	}{
		{
			name: "resolves password from secret",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("bgp-auth"),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "bgp-auth", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("secret-password")},
				},
			},
			wantPassword:      "secret-password",
			wantNeighborCount: 1,
		},
		{
			name: "password field takes precedence over passwordSecret",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:  new("192.168.1.2"),
					ASN:      new(int64(64513)),
					Password: new("inline-password"),
				},
			},
			wantPassword:      "inline-password",
			wantNeighborCount: 1,
		},
		{
			name: "no password fields set",
			neighbors: []v1alpha1.Neighbor{
				{
					Address: new("192.168.1.2"),
					ASN:     new(int64(64513)),
				},
			},
			wantPassword:      "",
			wantNeighborCount: 1,
		},
		{
			name: "secret not found removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("missing-secret"),
				},
			},
			wantNeighborCount: 0,
			wantResourceErr:   true,
		},
		{
			name: "secret with wrong type removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("opaque-secret"),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "opaque-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeOpaque,
					Data:       map[string][]byte{"password": []byte("secret-password")},
				},
			},
			wantNeighborCount: 0,
			wantResourceErr:   true,
		},
		{
			name: "secret missing password key removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("bad-secret"),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"wrong-key": []byte("value")},
				},
			},
			wantNeighborCount: 0,
			wantResourceErr:   true,
		},
		{
			name: "secret password with newline removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("inject-secret"),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inject-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("x\n  redistribute connected")},
				},
			},
			wantNeighborCount: 0,
			wantResourceErr:   true,
		},
		{
			name: "secret password with carriage return removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("cr-secret"),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cr-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("pass\rword")},
				},
			},
			wantNeighborCount: 0,
			wantResourceErr:   true,
		},
		{
			name: "secret password exceeding max length removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("long-secret"),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "long-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte(strings.Repeat("a", 129))},
				},
			},
			wantNeighborCount: 0,
			wantResourceErr:   true,
		},
		{
			name: "mix of valid and invalid neighbors keeps valid ones",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("good-secret"),
				},
				{
					Address:        new("192.168.1.3"),
					ASN:            new(int64(64514)),
					PasswordSecret: new("missing-secret"),
				},
				{
					Address: new("192.168.1.4"),
					ASN:     new(int64(64515)),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "good-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("valid-password")},
				},
			},
			wantPassword:      "valid-password",
			wantNeighborCount: 2,
			wantResourceErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objs []runtime.Object
			for i := range tt.secrets {
				objs = append(objs, &tt.secrets[i])
			}
			cli := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			r := &PERouterReconciler{
				Client:      cli,
				MyNamespace: "openperouter-system",
			}

			config := conversion.APIConfigData{
				Underlays: []v1alpha1.Underlay{
					{
						Spec: v1alpha1.UnderlaySpec{
							Neighbors: tt.neighbors,
						},
					},
				},
			}

			err := r.resolvePasswordSecrets(context.Background(), &config)

			var resourceErr *openpeerrors.ResourceError
			hasResourceErr := errors.As(err, &resourceErr)
			if tt.wantResourceErr && !hasResourceErr {
				t.Fatalf("expected ResourceError but got: %v", err)
			}
			if !tt.wantResourceErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			neighbors := config.Underlays[0].Spec.Neighbors
			if len(neighbors) != tt.wantNeighborCount {
				t.Fatalf("neighbor count = %d, want %d", len(neighbors), tt.wantNeighborCount)
			}

			if tt.wantPassword != "" && len(neighbors) > 0 {
				got := ""
				if pw := neighbors[0].Password; pw != nil {
					got = *pw
				}
				if got != tt.wantPassword {
					t.Errorf("password = %q, want %q", got, tt.wantPassword)
				}
			}
		})
	}
}
