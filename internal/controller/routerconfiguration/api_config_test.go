// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"log/slog"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetConfigFromAPICollectsSelectorErrors(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	invalid := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "rack", Operator: "invalid"}}}

	type resource struct {
		name     string
		selector *metav1.LabelSelector
	}
	tests := []struct {
		name             string
		resources        []resource
		wantNames        []string
		wantFailureNames []string
	}{
		{
			name:      "valid selectors",
			resources: []resource{{name: "good"}},
			wantNames: []string{"good"},
		},
		{
			name: "invalid selectors preserve valid resources",
			resources: []resource{
				{name: "bad-a", selector: invalid}, {name: "good"}, {name: "bad-b", selector: invalid},
			},
			wantNames:        []string{"good"},
			wantFailureNames: []string{"bad-a", "bad-b"},
		},
		{
			name: "all selectors invalid",
			resources: []resource{
				{name: "bad-a", selector: invalid}, {name: "bad-b", selector: invalid},
			},
			wantFailureNames: []string{"bad-a", "bad-b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := []client.Object{&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}}}
			for _, input := range tt.resources {
				name, selector := input.name, input.selector
				meta := metav1.ObjectMeta{Name: name, Namespace: "test", ResourceVersion: "1"}
				objects = append(objects,
					&v1alpha1.Underlay{ObjectMeta: meta, Spec: v1alpha1.UnderlaySpec{NodeSelector: selector}},
					&v1alpha1.L3VNI{ObjectMeta: meta, Spec: v1alpha1.L3VNISpec{NodeSelector: selector}},
					&v1alpha1.L2VNI{ObjectMeta: meta, Spec: v1alpha1.L2VNISpec{NodeSelector: selector}},
					&v1alpha1.L3VPN{ObjectMeta: meta, Spec: v1alpha1.L3VPNSpec{NodeSelector: selector}},
					&v1alpha1.L3Passthrough{ObjectMeta: meta, Spec: v1alpha1.L3PassthroughSpec{NodeSelector: selector}},
					&v1alpha1.RawFRRConfig{ObjectMeta: meta, Spec: v1alpha1.RawFRRConfigSpec{NodeSelector: selector}},
				)
			}
			r := &PERouterReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
				MyNode: "node", MyNamespace: "test", notStaticConfigsListOpts: &client.ListOptions{},
			}
			config, err := r.getConfigFromAPI(context.Background(), slog.Default())
			if len(tt.wantFailureNames) > 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.False(t, openpeerrors.IsNonResourceError(err))
			var wantFailures []v1alpha1.FailedResource
			for _, kind := range []v1alpha1.FailedResourceKind{
				"Underlay", "L3VNI", "L2VNI", "L3VPN", "L3Passthrough", "RawFRRConfig",
			} {
				for _, name := range tt.wantFailureNames {
					wantFailures = append(wantFailures, v1alpha1.FailedResource{
						Kind: kind, Name: name, Reason: v1alpha1.FailedResourceReasonValidationFailed,
						Message: "invalid node selector: \"invalid\" is not a valid label selector operator",
					})
				}
			}
			require.Equal(t, wantFailures, openpeerrors.CollectFailures(err))
			var wantConfig conversion.APIConfigData
			for _, name := range tt.wantNames {
				meta := metav1.ObjectMeta{Name: name, Namespace: "test", ResourceVersion: "1"}
				wantConfig.Underlays = append(wantConfig.Underlays, v1alpha1.Underlay{ObjectMeta: meta})
				wantConfig.L3VNIs = append(wantConfig.L3VNIs, v1alpha1.L3VNI{ObjectMeta: meta})
				wantConfig.L2VNIs = append(wantConfig.L2VNIs, v1alpha1.L2VNI{ObjectMeta: meta})
				wantConfig.L3VPNs = append(wantConfig.L3VPNs, v1alpha1.L3VPN{ObjectMeta: meta})
				wantConfig.L3Passthrough = append(wantConfig.L3Passthrough, v1alpha1.L3Passthrough{ObjectMeta: meta})
				wantConfig.RawFRRConfigs = append(wantConfig.RawFRRConfigs, v1alpha1.RawFRRConfig{ObjectMeta: meta})
			}
			require.Equal(t, wantConfig, config)
		})
	}
}
