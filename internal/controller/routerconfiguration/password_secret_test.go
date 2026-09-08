// SPDX-License-Identifier: Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
		name            string
		neighbors       []v1alpha1.Neighbor
		secrets         []corev1.Secret
		prePasswords    map[string]string
		wantNeighbors   []v1alpha1.Neighbor
		wantPasswords   map[string]string
		wantErrContains string
	}{
		{
			name: "resolves password from secret",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "bgp-auth"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "bgp-auth", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("secret-password")},
				},
			},
			wantNeighbors: []v1alpha1.Neighbor{{
				Address:        new("192.168.1.2"),
				ASN:            new(int64(64513)),
				PasswordSecret: &v1alpha1.SecretKeyRef{Name: "bgp-auth"},
			}},
			wantPasswords: map[string]string{"192.168.1.2": "secret-password"},
		},
		{
			name: "any secret type is accepted, not just basic-auth",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "opaque-secret"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "opaque-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeOpaque,
					Data:       map[string][]byte{"password": []byte("secret-password")},
				},
			},
			wantNeighbors: []v1alpha1.Neighbor{{
				Address:        new("192.168.1.2"),
				ASN:            new(int64(64513)),
				PasswordSecret: &v1alpha1.SecretKeyRef{Name: "opaque-secret"},
			}},
			wantPasswords: map[string]string{"192.168.1.2": "secret-password"},
		},
		{
			name: "resolves password from custom key",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "bgp-auth-custom-key", Key: new("bgp-password")},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "bgp-auth-custom-key", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeOpaque,
					Data:       map[string][]byte{"bgp-password": []byte("secret-password")},
				},
			},
			wantNeighbors: []v1alpha1.Neighbor{{
				Address:        new("192.168.1.2"),
				ASN:            new(int64(64513)),
				PasswordSecret: &v1alpha1.SecretKeyRef{Name: "bgp-auth-custom-key", Key: new("bgp-password")},
			}},
			wantPasswords: map[string]string{"192.168.1.2": "secret-password"},
		},
		{
			name: "secret missing the configured custom key",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "bad-custom-key-secret", Key: new("bgp-password")},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-custom-key-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeOpaque,
					Data:       map[string][]byte{"password": []byte("secret-password")},
				},
			},
			wantErrContains: "missing key \"bgp-password\"",
		},
		{
			name: "pre-resolved password preserved (systemd mode)",
			neighbors: []v1alpha1.Neighbor{
				{
					Address: new("192.168.1.2"),
					ASN:     new(int64(64513)),
				},
			},
			prePasswords: map[string]string{"192.168.1.2": "inline-password"},
			wantNeighbors: []v1alpha1.Neighbor{{
				Address: new("192.168.1.2"),
				ASN:     new(int64(64513)),
			}},
			wantPasswords: map[string]string{"192.168.1.2": "inline-password"},
		},
		{
			name: "no password fields set",
			neighbors: []v1alpha1.Neighbor{
				{
					Address: new("192.168.1.2"),
					ASN:     new(int64(64513)),
				},
			},
			wantNeighbors: []v1alpha1.Neighbor{{
				Address: new("192.168.1.2"),
				ASN:     new(int64(64513)),
			}},
		},
		{
			name: "secret not found removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "missing-secret"},
				},
			},
			wantErrContains: "secrets \"missing-secret\" not found",
		},
		{
			name: "secret missing password key removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "bad-secret"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "bad-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"wrong-key": []byte("value")},
				},
			},
			wantErrContains: "missing key \"password\"",
		},
		{
			name: "secret password with newline removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "inject-secret"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inject-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("x\n  redistribute connected")},
				},
			},
			wantErrContains: "contains whitespace or is empty",
		},
		{
			name: "secret password with carriage return removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "cr-secret"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "cr-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("pass\rword")},
				},
			},
			wantErrContains: "contains whitespace or is empty",
		},
		{
			name: "secret password with space removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "space-secret"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "space-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte("pass word")},
				},
			},
			wantErrContains: "contains whitespace or is empty",
		},
		{
			name: "secret password exceeding max length removes neighbor",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "long-secret"},
				},
			},
			secrets: []corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "long-secret", Namespace: "openperouter-system"},
					Type:       corev1.SecretTypeBasicAuth,
					Data:       map[string][]byte{"password": []byte(strings.Repeat("a", 81))},
				},
			},
			wantErrContains: "exceeds maximum length 80",
		},
		{
			name: "mix of valid and invalid neighbors keeps valid ones",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "good-secret"},
				},
				{
					Address:        new("192.168.1.3"),
					ASN:            new(int64(64514)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "missing-secret"},
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
			wantNeighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: &v1alpha1.SecretKeyRef{Name: "good-secret"},
				},
				{Address: new("192.168.1.4"), ASN: new(int64(64515))},
			},
			wantPasswords:   map[string]string{"192.168.1.2": "valid-password"},
			wantErrContains: "secrets \"missing-secret\" not found",
		},
		{
			name: "secret password empty removes neighbor",
			neighbors: []v1alpha1.Neighbor{{
				Address:        new("192.168.1.2"),
				ASN:            new(int64(64513)),
				PasswordSecret: &v1alpha1.SecretKeyRef{Name: "empty-secret"},
			}},
			secrets: []corev1.Secret{{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-secret", Namespace: "openperouter-system"},
				Data:       map[string][]byte{"password": []byte("")},
			}},
			wantErrContains: "contains whitespace or is empty",
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
				MyNamespace: "operator-namespace",
			}

			config := conversion.APIConfigData{
				Underlays: []v1alpha1.Underlay{
					{
						ObjectMeta: metav1.ObjectMeta{Namespace: "openperouter-system"},
						Spec: v1alpha1.UnderlaySpec{
							Neighbors: tt.neighbors,
						},
					},
				},
				Passwords: tt.prePasswords,
			}

			err := r.resolvePasswordSecrets(context.Background(), &config)

			checkResolveError(t, err, tt.wantErrContains)

			gotNeighbors := config.Underlays[0].Spec.Neighbors
			if diff := cmp.Diff(tt.wantNeighbors, gotNeighbors, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("neighbors mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantPasswords, config.Passwords, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("passwords mismatch (-want +got):\n%s", diff)
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
