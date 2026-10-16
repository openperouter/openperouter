// SPDX-License-Identifier:Apache-2.0

package filter_test

import (
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
)

func TestFilterRawFRRConfigsForNode(t *testing.T) {
	tests := []struct {
		name          string
		nodeLabels    map[string]string
		rawConfigs    []v1alpha1.RawFRRConfig
		expectedCount int
		expectedNames []string
	}{
		{
			name:       "nil selector matches all",
			nodeLabels: map[string]string{"rack": "rack-1"},
			rawConfigs: []v1alpha1.RawFRRConfig{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "raw-all"},
					Spec:       v1alpha1.RawFRRConfigSpec{RawConfig: "test config"},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"raw-all"},
		},
		{
			name:       "matching selector",
			nodeLabels: map[string]string{"kubernetes.io/hostname": "node-1"},
			rawConfigs: []v1alpha1.RawFRRConfig{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "raw-node-1"},
					Spec: v1alpha1.RawFRRConfigSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/hostname": "node-1"},
						},
						RawConfig: "test config",
					},
				},
			},
			expectedCount: 1,
			expectedNames: []string{"raw-node-1"},
		},
		{
			name:       "non-matching selector",
			nodeLabels: map[string]string{"kubernetes.io/hostname": "node-1"},
			rawConfigs: []v1alpha1.RawFRRConfig{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "raw-node-2"},
					Spec: v1alpha1.RawFRRConfigSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/hostname": "node-2"},
						},
						RawConfig: "test config",
					},
				},
			},
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name:       "mixed selectors filter correctly",
			nodeLabels: map[string]string{"rack": "rack-1"},
			rawConfigs: []v1alpha1.RawFRRConfig{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "raw-matching"},
					Spec: v1alpha1.RawFRRConfigSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-1"},
						},
						RawConfig: "matching config",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "raw-non-matching"},
					Spec: v1alpha1.RawFRRConfigSpec{
						NodeSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"rack": "rack-2"},
						},
						RawConfig: "non-matching config",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "raw-all-nodes"},
					Spec:       v1alpha1.RawFRRConfigSpec{RawConfig: "all nodes config"},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"raw-matching", "raw-all-nodes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: tt.nodeLabels,
				},
			}

			filtered, err := filter.RawFRRConfigsForNode(node, tt.rawConfigs)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(filtered) != tt.expectedCount {
				t.Errorf("expected %d raw configs, got %d", tt.expectedCount, len(filtered))
			}

			for i, expectedName := range tt.expectedNames {
				if i >= len(filtered) {
					t.Errorf("missing expected raw config: %s", expectedName)
					continue
				}
				if filtered[i].Name != expectedName {
					t.Errorf("expected raw config name %s, got %s", expectedName, filtered[i].Name)
				}
			}
		})
	}
}

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

func TestNodeSelectorMatches(t *testing.T) {
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"kubernetes.io/hostname": "node-1"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-2", Labels: map[string]string{"kubernetes.io/hostname": "node-2"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "unlabeled"}},
	}
	tests := []struct {
		name      string
		selector  *metav1.LabelSelector
		wantNodes []string
		wantError string
	}{
		{name: "nil selector is an error", wantError: "node selector is nil"},
		{name: "empty selects every node", selector: &metav1.LabelSelector{},
			wantNodes: []string{"node-1", "node-2", "unlabeled"}},
		{
			name:      "matching labels",
			selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "node-1"}},
			wantNodes: []string{"node-1"},
		},
		{
			name:      "other node",
			selector:  &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "node-2"}},
			wantNodes: []string{"node-2"},
		},
		{
			name:     "no matching nodes",
			selector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": "node-3"}},
		},
		{
			name: "DoesNotExist selects the unlabeled node",
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "kubernetes.io/hostname", Operator: metav1.LabelSelectorOpDoesNotExist},
			}},
			wantNodes: []string{"unlabeled"},
		},
		{
			name: "In selects multiple nodes",
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "kubernetes.io/hostname", Operator: metav1.LabelSelectorOpIn, Values: []string{"node-1", "node-2"}},
			}},
			wantNodes: []string{"node-1", "node-2"},
		},
		{
			name: "In with no values is an error",
			selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: "kubernetes.io/hostname", Operator: metav1.LabelSelectorOpIn},
			}},
			wantError: "for 'in', 'notin' operators, values set can't be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var matchedNodes []string
			for i := range nodes {
				node := &nodes[i]
				matched, err := filter.NodeSelectorMatches(node, tt.selector)
				if tt.wantError != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantError) {
						t.Fatalf("node %s: error = %v, want error containing %q", node.Name, err, tt.wantError)
					}
					if matched {
						t.Fatalf("node %s matched despite selector error", node.Name)
					}
					continue
				}
				if err != nil {
					t.Fatalf("node %s: unexpected error: %v", node.Name, err)
				}
				if matched {
					matchedNodes = append(matchedNodes, node.Name)
				}
			}
			if !slices.Equal(matchedNodes, tt.wantNodes) {
				t.Fatalf("matched nodes = %v, want %v", matchedNodes, tt.wantNodes)
			}
		})
	}
}
