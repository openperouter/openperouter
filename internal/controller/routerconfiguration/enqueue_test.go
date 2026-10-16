// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestEnqueueForNodeCoalescesEvents(t *testing.T) {
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
	defer q.ShutDown()
	r := &PERouterReconciler{MyNode: "node-a"}
	h := handler.EnqueueRequestsFromMapFunc(r.nodeRequestFor)
	ctx := context.Background()

	for i, name := range []string{"l2-1", "l2-2", "l2-3"} {
		obj := &v1alpha1.L2VNI{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}}
		h.Create(ctx, event.CreateEvent{Object: obj}, q)
		if i == 0 {
			h.Update(ctx, event.UpdateEvent{ObjectOld: obj, ObjectNew: obj}, q)
		}
	}
	h.Create(ctx, event.CreateEvent{Object: &v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"}}}, q)
	h.Delete(ctx, event.DeleteEvent{Object: &v1alpha1.L3VNI{ObjectMeta: metav1.ObjectMeta{Name: "l3", Namespace: "ns"}}}, q)

	if got := q.Len(); got != 1 {
		t.Fatalf("expected the events to coalesce into one request, got %d", got)
	}
	item, _ := q.Get()
	want := ctrl.Request{NamespacedName: types.NamespacedName{Name: "node-a"}}
	if item != want {
		t.Fatalf("expected %v, got %v", want, item)
	}
	q.Done(item)
}

func TestEnqueueForNodeKeepsOnePendingBehindARunningReconcile(t *testing.T) {
	q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
	defer q.ShutDown()
	r := &PERouterReconciler{MyNode: "node-a"}
	h := handler.EnqueueRequestsFromMapFunc(r.nodeRequestFor)
	ctx := context.Background()
	obj := func(name string) *v1alpha1.L2VNI {
		return &v1alpha1.L2VNI{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"}}
	}

	h.Create(ctx, event.CreateEvent{Object: obj("first")}, q)
	running, _ := q.Get()

	for _, name := range []string{"a", "b", "c", "d"} {
		h.Create(ctx, event.CreateEvent{Object: obj(name)}, q)
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("a key being processed must not be queued twice, got %d", got)
	}
	q.Done(running)
	if got := q.Len(); got != 1 {
		t.Fatalf("expected exactly one pass pending after the running one, got %d", got)
	}
}
