// SPDX-License-Identifier: Apache-2.0

package routerconfiguration

import (
	"context"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolvePasswordSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name         string
		neighbors    []v1alpha1.Neighbor
		secrets      []corev1.Secret
		wantPassword string
		wantErr      bool
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
			wantPassword: "secret-password",
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
			wantPassword: "inline-password",
		},
		{
			name: "no password fields set",
			neighbors: []v1alpha1.Neighbor{
				{
					Address: new("192.168.1.2"),
					ASN:     new(int64(64513)),
				},
			},
			wantPassword: "",
		},
		{
			name: "secret not found",
			neighbors: []v1alpha1.Neighbor{
				{
					Address:        new("192.168.1.2"),
					ASN:            new(int64(64513)),
					PasswordSecret: new("missing-secret"),
				},
			},
			wantErr: true,
		},
		{
			name: "secret with wrong type rejected",
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
			wantErr: true,
		},
		{
			name: "secret missing password key",
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
			wantErr: true,
		},
		{
			name: "secret password with newline rejected",
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
			wantErr: true,
		},
		{
			name: "secret password with carriage return rejected",
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
			wantErr: true,
		},
		{
			name: "secret password with space rejected",
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
			wantErr: true,
		},
		{
			name: "secret password exceeding max length rejected",
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
			wantErr: true,
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
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := ""
			if pw := config.Underlays[0].Spec.Neighbors[0].Password; pw != nil {
				got = *pw
			}
			if got != tt.wantPassword {
				t.Errorf("password = %q, want %q", got, tt.wantPassword)
			}
		})
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
