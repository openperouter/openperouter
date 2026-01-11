// SPDX-License-Identifier:Apache-2.0

package filter_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
)

func TestFilterUnderlaysForNode(t *testing.T) {
	tests := []struct {
		name          string
		nodeLabels    map[string]string
		underlays     []v1alpha1.Underlay
		expectedCount int
		expectedNames []string
	}{
		{
			name:       "no selector matches all",
			nodeLabels: map[string]string{"rack": "rack-1"},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-1"},
					Spec:       v1alpha1.UnderlaySpec{NodeSelector: nil},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"underlay-1"},
		},
		{
			name:       "matching selector",
			nodeLabels: map[string]string{"rack": "rack-1"},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
					},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"underlay-rack-1"},
		},
		{
			name:       "non-matching selector",
			nodeLabels: map[string]string{"rack": "rack-1"},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-2"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-2"},
						},
					},
				},
			},
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name:       "multiple underlays with different selectors",
			nodeLabels: map[string]string{"rack": "rack-1", "zone": "us-east-1a"},
			underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-1"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-rack-2"},
					Spec: v1alpha1.UnderlaySpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-2"},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "underlay-all"},
					Spec:       v1alpha1.UnderlaySpec{NodeSelector: nil},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"underlay-rack-1", "underlay-all"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.nodeLabels,
				},
			}

			filtered, err := filter.UnderlaysForNode(node, tt.underlays)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(filtered) != tt.expectedCount {
				t.Errorf("expected %d underlays, got %d", tt.expectedCount, len(filtered))
			}

			for i, expectedName := range tt.expectedNames {
				if i >= len(filtered) {
					t.Errorf("missing expected underlay: %s", expectedName)
					continue
				}
				if filtered[i].Name != expectedName {
					t.Errorf("expected underlay name %s, got %s", expectedName, filtered[i].Name)
				}
			}
		})
	}
}
