// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

func TestEnsureTrunkIDLabels(t *testing.T) {
	ctx := context.Background()

	t.Run("labels a trunk missing the trunk id label", func(t *testing.T) {
		trunk := trunkForTest("workloads", "tenant-a", nil)
		r := reconcilerWithObjects(t, trunk)

		r.ensureTrunkIDLabels(ctx, []v1alpha1.L2Trunk{*trunk})

		got := getL2Trunk(t, r.Client, trunk)
		require.Equal(t, hostnetwork.TrunkID(trunk.Namespace, trunk.Name), got.Labels[v1alpha1.TrunkIDLabel])
	})

	t.Run("relabels a trunk with a stale trunk id label", func(t *testing.T) {
		trunk := trunkForTest("workloads", "tenant-a", map[string]string{v1alpha1.TrunkIDLabel: "stalestale"})
		r := reconcilerWithObjects(t, trunk)

		r.ensureTrunkIDLabels(ctx, []v1alpha1.L2Trunk{*trunk})

		got := getL2Trunk(t, r.Client, trunk)
		require.Equal(t, hostnetwork.TrunkID(trunk.Namespace, trunk.Name), got.Labels[v1alpha1.TrunkIDLabel])
	})

	t.Run("leaves an already-correct label untouched", func(t *testing.T) {
		id := hostnetwork.TrunkID("tenant-a", "workloads")
		trunk := trunkForTest("workloads", "tenant-a", map[string]string{v1alpha1.TrunkIDLabel: id})
		r := reconcilerWithObjects(t, trunk)
		before := getL2Trunk(t, r.Client, trunk)

		r.ensureTrunkIDLabels(ctx, []v1alpha1.L2Trunk{*trunk})

		after := getL2Trunk(t, r.Client, trunk)
		require.Equal(t, before.ResourceVersion, after.ResourceVersion,
			"an already-correct label must not trigger a patch")
	})

	t.Run("a failed patch does not stop the remaining trunks from being labeled", func(t *testing.T) {
		deleted := trunkForTest("ghost", "tenant-a", nil)
		present := trunkForTest("workloads", "tenant-b", nil)
		r := reconcilerWithObjects(t, present) // deleted is never created in the fake client

		require.NotPanics(t, func() {
			r.ensureTrunkIDLabels(ctx, []v1alpha1.L2Trunk{*deleted, *present})
		})

		got := getL2Trunk(t, r.Client, present)
		require.Equal(t, hostnetwork.TrunkID(present.Namespace, present.Name), got.Labels[v1alpha1.TrunkIDLabel])
	})
}

func TestEmitTrunkDependencyEvents(t *testing.T) {
	trunked := l2VNIForTestObj("web", "tenant-a")
	config := conversion.APIConfigData{L2VNIs: []v1alpha1.L2VNI{trunked}}
	missingTrunkMessage := `referenced L2Trunk "workloads" not found`
	missingTrunkErr := missingDependencyError(trunked.Name, openpeerrors.KindL2Trunk, "workloads")

	tests := []struct {
		name       string
		err        error
		noRecorder bool
		wantEvents []fakeEvent
	}{
		{
			name: "no error means no event",
		},
		{
			name:       "no recorder means no event, and no panic",
			err:        missingTrunkErr,
			noRecorder: true,
		},
		{
			name: "a missing trunk dependency failure emits a warning event on the L2VNI",
			err:  missingTrunkErr,
			wantEvents: []fakeEvent{
				{object: &trunked, reason: "TrunkUnavailable", note: "dataplane torn down: " + missingTrunkMessage},
			},
		},
		{
			name: "an unrelated resource failure is not reported as a trunk event",
			err: &openpeerrors.ResourceError{Obj: v1alpha1.FailedResource{
				Kind:    openpeerrors.KindL3VNI,
				Name:    "red",
				Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
				Message: "failed to set up VNI",
			}},
		},
		{
			name: "a dependency failure for an L2VNI outside this node's config is not reported",
			err:  missingDependencyError("not-on-this-node", openpeerrors.KindL2Trunk, "workloads"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &fakeEventRecorder{}
			r := &PERouterReconciler{}
			if !tc.noRecorder {
				r.Recorder = recorder
			}

			r.emitTrunkDependencyEvents(config, tc.err)

			require.Equal(t, tc.wantEvents, recorder.events)
		})
	}
}

func trunkForTest(name, namespace string, labels map[string]string) *v1alpha1.L2Trunk {
	return &v1alpha1.L2Trunk{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
	}
}

func l2VNIForTestObj(name, namespace string) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
}

func reconcilerWithObjects(t *testing.T, objects ...client.Object) *PERouterReconciler {
	t.Helper()
	s := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(s).WithObjects(objects...).Build()
	return &PERouterReconciler{Client: cli, Scheme: s}
}

func getL2Trunk(t *testing.T, cli client.Client, trunk *v1alpha1.L2Trunk) *v1alpha1.L2Trunk {
	t.Helper()
	got := &v1alpha1.L2Trunk{}
	require.NoError(t, cli.Get(context.Background(), client.ObjectKeyFromObject(trunk), got))
	return got
}

// fakeEvent is one call fakeEventRecorder observed, with note already
// formatted so test tables can assert on the rendered message.
type fakeEvent struct {
	object runtime.Object
	reason string
	note   string
}

// fakeEventRecorder is a minimal recorder.EventRecorder that records every
// event it is asked to emit instead of sending it anywhere.
type fakeEventRecorder struct {
	events []fakeEvent
}

func (f *fakeEventRecorder) Eventf(regarding, _ runtime.Object, _, reason, _, note string, args ...interface{}) {
	f.events = append(f.events, fakeEvent{object: regarding, reason: reason, note: fmt.Sprintf(note, args...)})
}

func (f *fakeEventRecorder) AnnotatedEventf(
	regarding, related runtime.Object, _ map[string]string, eventtype, reason, action, note string, args ...interface{},
) {
	f.Eventf(regarding, related, eventtype, reason, action, note, args...)
}
