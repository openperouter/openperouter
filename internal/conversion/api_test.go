// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMergeAPIConfigs_SingleConfig(t *testing.T) {
	config := ApiConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
				Spec: v1alpha1.UnderlaySpec{
					ASN: 64515,
				},
			},
		},
	}

	merged, err := MergeAPIConfigs(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(merged.Underlays) != 1 {
		t.Fatalf("expected 1 underlay, got %d", len(merged.Underlays))
	}
	if merged.Underlays[0].Spec.ASN != 64515 {
		t.Errorf("expected ASN 64515, got %d", merged.Underlays[0].Spec.ASN)
	}
}

func TestMergeAPIConfigs_MultipleConfigs(t *testing.T) {
	config1 := ApiConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
				Spec:       v1alpha1.UnderlaySpec{ASN: 64515},
			},
		},
	}

	config2 := ApiConfigData{
		L3VNIs: []v1alpha1.L3VNI{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "l3vni1"},
				Spec:       v1alpha1.L3VNISpec{VNI: 1000},
			},
		},
	}

	config3 := ApiConfigData{
		L2VNIs: []v1alpha1.L2VNI{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
				Spec:       v1alpha1.L2VNISpec{VNI: 2000},
			},
		},
	}

	merged, err := MergeAPIConfigs(config1, config2, config3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(merged.Underlays) != 1 {
		t.Errorf("expected 1 underlay, got %d", len(merged.Underlays))
	}
	if len(merged.L3VNIs) != 1 {
		t.Errorf("expected 1 l3vni, got %d", len(merged.L3VNIs))
	}
	if len(merged.L2VNIs) != 1 {
		t.Errorf("expected 1 l2vni, got %d", len(merged.L2VNIs))
	}
}
func TestMergeAPIConfigs_AllResourceTypes(t *testing.T) {
	config := ApiConfigData{
		Underlays: []v1alpha1.Underlay{
			{ObjectMeta: metav1.ObjectMeta{Name: "underlay1"}},
		},
		L3VNIs: []v1alpha1.L3VNI{
			{ObjectMeta: metav1.ObjectMeta{Name: "l3vni1"}},
		},
		L2VNIs: []v1alpha1.L2VNI{
			{ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"}},
		},
		L3Passthrough: []v1alpha1.L3Passthrough{
			{ObjectMeta: metav1.ObjectMeta{Name: "passthrough1"}},
		},
	}

	merged, err := MergeAPIConfigs(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(merged.Underlays) != 1 {
		t.Errorf("expected 1 underlay, got %d", len(merged.Underlays))
	}
	if len(merged.L3VNIs) != 1 {
		t.Errorf("expected 1 l3vni, got %d", len(merged.L3VNIs))
	}
	if len(merged.L2VNIs) != 1 {
		t.Errorf("expected 1 l2vni, got %d", len(merged.L2VNIs))
	}
	if len(merged.L3Passthrough) != 1 {
		t.Errorf("expected 1 l3passthrough, got %d", len(merged.L3Passthrough))
	}
}

func TestMergeAPIConfigs_ResourcesConcatenated(t *testing.T) {
	config1 := ApiConfigData{
		Underlays: []v1alpha1.Underlay{
			{ObjectMeta: metav1.ObjectMeta{Name: "underlay1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "underlay2"}},
		},
	}

	config2 := ApiConfigData{
		Underlays: []v1alpha1.Underlay{
			{ObjectMeta: metav1.ObjectMeta{Name: "underlay3"}},
		},
	}

	merged, err := MergeAPIConfigs(config1, config2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(merged.Underlays) != 3 {
		t.Errorf("expected 3 underlays, got %d", len(merged.Underlays))
	}

	// Verify order is preserved
	expectedNames := []string{"underlay1", "underlay2", "underlay3"}
	for i, underlay := range merged.Underlays {
		if underlay.Name != expectedNames[i] {
			t.Errorf("expected underlay name %s at index %d, got %s", expectedNames[i], i, underlay.Name)
		}
	}
}
