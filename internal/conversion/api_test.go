// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/google/go-cmp/cmp"
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
		L3VNIs:        []v1alpha1.L3VNI{},
		L2VNIs:        []v1alpha1.L2VNI{},
		L3Passthrough: []v1alpha1.L3Passthrough{},
	}

	merged, err := MergeAPIConfigs(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cmp.Equal(config, merged) {
		t.Errorf("merged config mismatch (-want +got):\n%s", cmp.Diff(config, merged))
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

	want := ApiConfigData{
		Underlays: []v1alpha1.Underlay{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "underlay1"},
				Spec:       v1alpha1.UnderlaySpec{ASN: 64515},
			},
		},
		L3VNIs: []v1alpha1.L3VNI{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "l3vni1"},
				Spec:       v1alpha1.L3VNISpec{VNI: 1000},
			},
		},
		L2VNIs: []v1alpha1.L2VNI{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "l2vni1"},
				Spec:       v1alpha1.L2VNISpec{VNI: 2000},
			},
		},
		L3Passthrough: []v1alpha1.L3Passthrough{},
	}

	if !cmp.Equal(want, merged) {
		t.Errorf("merged config mismatch (-want +got):\n%s", cmp.Diff(want, merged))
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

	if !cmp.Equal(config, merged) {
		t.Errorf("merged config mismatch (-want +got):\n%s", cmp.Diff(config, merged))
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

	want := ApiConfigData{
		Underlays: []v1alpha1.Underlay{
			{ObjectMeta: metav1.ObjectMeta{Name: "underlay1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "underlay2"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "underlay3"}},
		},
		L3VNIs:        []v1alpha1.L3VNI{},
		L2VNIs:        []v1alpha1.L2VNI{},
		L3Passthrough: []v1alpha1.L3Passthrough{},
	}

	if !cmp.Equal(want, merged) {
		t.Errorf("merged config mismatch (-want +got):\n%s", cmp.Diff(want, merged))
	}
}
