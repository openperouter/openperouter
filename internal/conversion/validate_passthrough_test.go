// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestValidatePassthroughsForNodes(t *testing.T) {
	tests := []struct {
		name           string
		nodes          []corev1.Node
		l3Passthroughs []v1alpha1.L3Passthrough
		expectedErr    error
	}{
		{
			name:           "no nodes no passthroughs",
			nodes:          nil,
			l3Passthroughs: nil,
		},
		{
			name: "single node single passthrough with nil selector",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"rack": "rack-1"}}},
			},
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-1"},
					Spec:       v1alpha1.L3PassthroughSpec{NodeSelector: nil},
				},
			},
		},
		{
			name: "single node two passthroughs matching the same node",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"rack": "rack-1"}}},
			},
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-1"},
					Spec:       v1alpha1.L3PassthroughSpec{NodeSelector: nil},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-2"},
					Spec: v1alpha1.L3PassthroughSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
					},
				},
			},
			expectedErr: fmt.Errorf("can't have more than one l3passthrough per node"),
		},
		{
			name: "two nodes each with one matching passthrough",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"rack": "rack-1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"rack": "rack-2"}}},
			},
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-1"},
					Spec: v1alpha1.L3PassthroughSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-2"},
					Spec: v1alpha1.L3PassthroughSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-2"},
						},
					},
				},
			},
		},
		{
			name: "passthrough does not match any node",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"rack": "rack-1"}}},
			},
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-1"},
					Spec: v1alpha1.L3PassthroughSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-99"},
						},
					},
				},
			},
		},
		{
			name: "nil selector plus targeted passthrough on same node",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"rack": "rack-1"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"rack": "rack-2"}}},
			},
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-global"},
					Spec:       v1alpha1.L3PassthroughSpec{NodeSelector: nil},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-rack-1"},
					Spec: v1alpha1.L3PassthroughSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
					},
				},
			},
			expectedErr: fmt.Errorf("can't have more than one l3passthrough per node"),
		},
		{
			name: "invalid node selector expression",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"rack": "rack-1"}}},
			},
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pt-bad"},
					Spec: v1alpha1.L3PassthroughSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{Key: "rack", Operator: "InvalidOp", Values: []string{"rack-1"}},
							},
						},
					},
				},
			},
			expectedErr: fmt.Errorf("failed to filter underlays for node \"node-1\": \"InvalidOp\" is not a valid label selector operator"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassthroughsForNodes(tt.nodes, tt.l3Passthroughs)
			if tt.expectedErr != nil {
				if err == nil {
					t.Fatalf("expected error %q but got none", tt.expectedErr)
				}
				if !strings.Contains(err.Error(), tt.expectedErr.Error()) {
					t.Fatalf("expected error containing %q, got: %q", tt.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFilterValidPassthroughs(t *testing.T) {
	tests := []struct {
		name           string
		l3Passthroughs []v1alpha1.L3Passthrough
		expectedErr    error
	}{
		{
			name:           "no passthroughs",
			l3Passthroughs: nil,
		},
		{
			name:           "empty slice",
			l3Passthroughs: []v1alpha1.L3Passthrough{},
		},
		{
			name: "single passthrough",
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{ObjectMeta: metav1.ObjectMeta{Name: "pt-1"}},
			},
		},
		{
			name: "two passthroughs",
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{ObjectMeta: metav1.ObjectMeta{Name: "pt-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pt-2"}},
			},
			expectedErr: fmt.Errorf("can't have more than one l3passthrough per node"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FilterValidPassthroughs(tt.l3Passthroughs)
			if tt.expectedErr != nil {
				if err == nil {
					t.Fatalf("expected error %q but got none", tt.expectedErr)
				}
				if !strings.Contains(err.Error(), tt.expectedErr.Error()) {
					t.Fatalf("expected error containing %q, got: %q", tt.expectedErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
