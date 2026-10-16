// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"slices"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestFilterCurrentNodeOnly(t *testing.T) {
	nodeA := &v1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-a", Labels: map[string]string{"kubernetes.io/hostname": "node-a"},
	}}
	localNodeRequest := []ctrl.Request{{NamespacedName: types.NamespacedName{Name: "node-a"}}}
	onA := func() (*v1.Node, error) { return nodeA, nil }

	tests := []struct {
		name         string
		getNode      func() (*v1.Node, error)
		obj          client.Object
		wantRequests []ctrl.Request
	}{
		{name: "L2VNI selecting this node", getNode: onA, obj: l2vniFor(selectHost("node-a")), wantRequests: localNodeRequest},
		{name: "L2VNI selecting another node", getNode: onA, obj: l2vniFor(selectHost("node-b"))},
		{name: "L2VNI without a selector", getNode: onA, obj: l2vniFor(nil), wantRequests: localNodeRequest},
		{
			name:    "Underlay for another node",
			getNode: onA,
			obj: &v1alpha1.Underlay{
				Spec: v1alpha1.UnderlaySpec{
					NodeSelector: selectHost("node-b"),
				},
			},
		},
		{
			name:    "L3VNI for another node",
			getNode: onA,
			obj: &v1alpha1.L3VNI{
				Spec: v1alpha1.L3VNISpec{
					NodeSelector: selectHost("node-b"),
				},
			},
		},
		{
			name:    "L3VPN for another node",
			getNode: onA,
			obj: &v1alpha1.L3VPN{
				Spec: v1alpha1.L3VPNSpec{
					NodeSelector: selectHost("node-b"),
				},
			},
		},
		{
			name:    "L3Passthrough for another node",
			getNode: onA,
			obj: &v1alpha1.L3Passthrough{
				Spec: v1alpha1.L3PassthroughSpec{
					NodeSelector: selectHost("node-b"),
				},
			},
		},
		{
			name:    "RawFRRConfig for another node",
			getNode: onA,
			obj: &v1alpha1.RawFRRConfig{
				Spec: v1alpha1.RawFRRConfigSpec{
					NodeSelector: selectHost("node-b"),
				},
			},
		},
		{name: "objects without a node selector pass", getNode: onA, obj: &v1.Secret{}, wantRequests: localNodeRequest},
		{name: "an unreadable node lets the event through",
			getNode: func() (*v1.Node, error) { return nil, errors.New("not in cache") },
			obj:     l2vniFor(selectHost("node-b")), wantRequests: localNodeRequest},
		{
			name:    "an unparsable selector lets the event through",
			getNode: onA,
			obj: l2vniFor(&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "kubernetes.io/hostname",
						Operator: metav1.LabelSelectorOpIn,
					},
				},
			}),
			wantRequests: localNodeRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &PERouterReconciler{MyNode: "node-a"}
			f := r.filterCurrentNodeOnly(tt.getNode)
			h := handler.EnqueueRequestsFromMapFunc(r.nodeRequestFor)
			ctx := context.Background()
			for kind, dispatch := range map[string]func(workqueue.TypedRateLimitingInterface[ctrl.Request]){
				"create": func(q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
					e := event.CreateEvent{Object: tt.obj}
					if f.Create(e) {
						h.Create(ctx, e, q)
					}
				},
				"delete": func(q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
					e := event.DeleteEvent{Object: tt.obj}
					if f.Delete(e) {
						h.Delete(ctx, e, q)
					}
				},
			} {
				t.Run(kind, func(t *testing.T) {
					q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
					defer q.ShutDown()
					dispatch(q)
					var requests []ctrl.Request
					for q.Len() > 0 {
						req, _ := q.Get()
						requests = append(requests, req)
						q.Done(req)
					}
					if !slices.Equal(requests, tt.wantRequests) {
						t.Fatalf("queued requests = %v, want %v", requests, tt.wantRequests)
					}
				})
			}
		})
	}
}

func TestFilterCurrentNodeOnlyUpdateMatchesEitherVersion(t *testing.T) {
	nodeA := &v1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-a", Labels: map[string]string{"kubernetes.io/hostname": "node-a"},
	}}
	f := (&PERouterReconciler{MyNode: "node-a"}).filterCurrentNodeOnly(func() (*v1.Node, error) { return nodeA, nil })

	tests := []struct {
		name     string
		old, new *metav1.LabelSelector
		want     bool
	}{
		{name: "moves onto this node", old: selectHost("node-b"), new: selectHost("node-a", "node-b"), want: true},
		{name: "moves away from this node", old: selectHost("node-a"), new: selectHost("node-b"), want: true},
		{name: "never on this node", old: selectHost("node-b"), new: selectHost("node-b", "node-c"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := event.UpdateEvent{ObjectOld: l2vniFor(tt.old), ObjectNew: l2vniFor(tt.new)}
			if got := f.Update(e); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterCurrentNodeOnlyLocalEvents(t *testing.T) {
	r := &PERouterReconciler{MyNode: "node-a", MyNamespace: "router"}
	p := r.filterCurrentNodeOnly(func() (*v1.Node, error) {
		t.Fatal("Node events and nil selectors must not look up the cached Node")
		return nil, nil
	})
	tests := []struct {
		name string
		obj  client.Object
		want bool
	}{
		{name: "local node", obj: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}, want: true},
		{name: "other node", obj: &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}}},
		{name: "secret in other namespace", obj: &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "other"}}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated := tt.obj.DeepCopyObject().(client.Object)
			updated.SetLabels(map[string]string{"changed": "true"})
			events := map[string]bool{
				"create":  p.Create(event.CreateEvent{Object: tt.obj}),
				"update":  p.Update(event.UpdateEvent{ObjectOld: tt.obj, ObjectNew: updated}),
				"delete":  p.Delete(event.DeleteEvent{Object: tt.obj}),
				"generic": p.Generic(event.GenericEvent{Object: tt.obj}),
			}
			for kind, got := range events {
				if got != tt.want {
					t.Errorf("%s: got %v, want %v", kind, got, tt.want)
				}
			}
		})
	}
}

func l2vniFor(sel *metav1.LabelSelector) *v1alpha1.L2VNI {
	return &v1alpha1.L2VNI{Spec: v1alpha1.L2VNISpec{NodeSelector: sel}}
}

func selectHost(hosts ...string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
		{Key: "kubernetes.io/hostname", Operator: metav1.LabelSelectorOpIn, Values: hosts},
	}}
}
