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
		wantErrContains   string
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
			name: "pre-resolved password preserved (systemd mode)",
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
			wantErrContains:   "missing-secret",
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
			wantErrContains:   "expected \"kubernetes.io/basic-auth\"",
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
			wantErrContains:   "missing key",
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
			wantErrContains:   "whitespace",
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
			wantErrContains:   "whitespace",
		},
		{
			name: "secret password with space removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("space-secret"),
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "space-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("pass word")},
				},
			},
			wantNeighborCount: 0,
			wantErrContains:   "whitespace",
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
					Data:       map[string][]byte{"password": []byte(strings.Repeat("a", 81))},
				},
			},
			wantNeighborCount: 0,
			wantErrContains:   "maximum length",
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
			wantErrContains:   "missing-secret",
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

			checkResolveError(t, err, tt.wantErrContains)

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

func checkResolveError(t *testing.T, err error, wantContains string) {
	t.Helper()
	if wantContains != "" {
		var resourceErr *openpeerrors.ResourceError
		if !errors.As(err, &resourceErr) || !strings.Contains(err.Error(), wantContains) {
			t.Fatalf("expected ResourceError containing %q, got: %v", wantContains, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pass    string
		wantErr bool
	}{
		{name: "valid", pass: "my-secret-123", wantErr: false},
		{name: "single char", pass: "x", wantErr: false},
		{name: "max length", pass: strings.Repeat("a", 80), wantErr: false},
		{name: "too long", pass: strings.Repeat("a", 81), wantErr: true},
		{name: "empty", pass: "", wantErr: true},
		{name: "contains space", pass: "pass word", wantErr: true},
		{name: "contains tab", pass: "pass\tword", wantErr: true},
		{name: "contains newline", pass: "pass\nword", wantErr: true},
		{name: "contains carriage return", pass: "pass\rword", wantErr: true},
		{name: "leading space", pass: " password", wantErr: true},
		{name: "trailing space", pass: "password ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.pass)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePassword(%q) error = %v, wantErr %v", tt.pass, err, tt.wantErr)
			}
		})
	}
}
