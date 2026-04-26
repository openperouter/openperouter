/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package routerconfiguration

import (
	"context"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
)

var noopUpdater = frr.ConfigUpdater(func(_ context.Context, _ string) error {
	return nil
})

var noopHostConfigurator = HostConfigurator(func(_ context.Context, _ interfacesConfiguration) (ReconcileResult, error) {
	return ReconcileResult{}, nil
})

func TestReconcileQuarantine(t *testing.T) {
	tests := []struct {
		name             string
		l3VNIs           []v1alpha1.L3VNI
		l2VNIs           []v1alpha1.L2VNI
		expectedFailures []v1alpha1.FailedResource
	}{
		{
			name: "all valid L3VNIs pass through",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("vni-a", "vrfA", 100),
				l3VNI("vni-b", "vrfB", 200),
			},
		},
		{
			name: "duplicate VRF quarantines second L3VNI",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("first", "same-vrf", 100),
				l3VNI("second", "same-vrf", 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: KindL3VNI, Name: "second", Reason: v1alpha1.ValidationFailed,
					Message: ptr.To("duplicate VRF same-vrf, already used by L3VNI first")},
			},
		},
		{
			name: "duplicate VNI within L3VNIs quarantines second",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("first", "vrfA", 100),
				l3VNI("second", "vrfB", 100),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: KindL3VNI, Name: "second", Reason: v1alpha1.ValidationFailed,
					Message: ptr.To("duplicate VNI 100, already used by L3VNI/first")},
			},
		},
		{
			name: "invalid VRF name quarantines L3VNI",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("bad-name", "this-is-way-too-long-for-interface", 100),
				l3VNI("good", "vrfA", 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: KindL3VNI, Name: "bad-name", Reason: v1alpha1.ValidationFailed,
					Message: ptr.To("invalid vrf name for vni \"bad-name\", vrf \"this-is-way-too-long-for-interface\": interface name this-is-way-too-long-for-interface can't be longer than 15 characters")},
			},
		},
		{
			name: "valid connected and disconnected L2VNIs",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("l3-a", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("connected", ptr.To("vrfA"), 200),
				l2VNI("disconnected", nil, 201),
			},
		},
		{
			name: "orphan L2VNI referencing nonexistent VRF",
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("orphan", ptr.To("nonexistent"), 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: KindL2VNI, Name: "orphan", Reason: v1alpha1.DependencyFailed,
					Message: ptr.To("no valid L3VNI for VRF nonexistent")},
			},
		},
		{
			name: "duplicate VNI within L2VNIs quarantines second",
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("first", nil, 200),
				l2VNI("second", nil, 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: KindL2VNI, Name: "second", Reason: v1alpha1.ValidationFailed,
					Message: ptr.To("duplicate VNI 200, already used by L2VNI/first")},
			},
		},
		{
			name: "cross-type VNI conflict quarantines L2VNI",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3", "vrfA", 100),
				l3VNI("conflict-l3", "vrfB", 300),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", nil, 200),
				l2VNI("conflict-l2", nil, 300),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: KindL2VNI, Name: "conflict-l2", Reason: v1alpha1.ValidationFailed,
					Message: ptr.To("duplicate VNI 300, already used by L3VNI/conflict-l3")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := conversion.APIConfigData{
				L3VNIs: tt.l3VNIs,
				L2VNIs: tt.l2VNIs,
			}
			result, err := Reconcile(context.Background(), config, false, 0, "",
				"", "", noopUpdater, noopHostConfigurator)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(result.FailedResources, tt.expectedFailures) {
				t.Errorf("FailedResources mismatch:\n  got:  %s\n  want: %s",
					formatFailures(result.FailedResources), formatFailures(tt.expectedFailures))
			}
		})
	}
}

func l3VNI(name, vrf string, vni int32) v1alpha1.L3VNI {
	return v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.L3VNISpec{VRF: vrf, VNI: vni},
	}
}

func l2VNI(name string, vrf *string, vni int32) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.L2VNISpec{VRF: vrf, VNI: vni},
	}
}

func formatFailures(failures []v1alpha1.FailedResource) string {
	if len(failures) == 0 {
		return "<none>"
	}
	s := ""
	for i, f := range failures {
		msg := "<nil>"
		if f.Message != nil {
			msg = *f.Message
		}
		if i > 0 {
			s += "\n         "
		}
		s += f.Kind + "/" + f.Name + " (" + string(f.Reason) + "): " + msg
	}
	return s
}
