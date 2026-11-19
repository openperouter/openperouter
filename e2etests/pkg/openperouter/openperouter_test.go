// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGet(t *testing.T) {
	originalLabel := InstrumentedNodesLabel
	t.Cleanup(func() { InstrumentedNodesLabel = originalLabel })

	tests := []struct {
		name     string
		selector string
		hostMode bool
		want     []string
		wantErr  string
	}{
		{name: "all routers", want: []string{"node1", "node2"}},
		{name: "selected routers", selector: "role=instrumented", want: []string{"node1"}},
		{
			name: "missing router", selector: "role=missing",
			wantErr: "expected 1 router pods on selected nodes, got 0",
		},
		{
			name: "no matching nodes", selector: "role=absent",
			wantErr: `no nodes match instrumented node label selector "role=absent"`,
		},
		{
			name: "no matching host nodes", selector: "role=absent", hostMode: true,
			wantErr: `no nodes match instrumented node label selector "role=absent"`,
		},
		{name: "invalid selector", selector: "=", wantErr: "invalid node label selector"},
		{name: "invalid host selector", selector: "=", hostMode: true, wantErr: "invalid node label selector"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			InstrumentedNodesLabel = tt.selector
			cs := fake.NewClientset(
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"role": "instrumented"}}},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"role": "other"}}},
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3", Labels: map[string]string{"role": "missing"}}},
				routerTestPod("router1", "node1"),
				routerTestPod("router2", "node2"),
			)
			routers, err := Get(cs, tt.hostMode)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Get() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for router := range routers.GetExecutors() {
				got = append(got, router.NodeName())
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Get() nodes = %v, want %v", got, tt.want)
			}
		})
	}
}

func routerTestPod(name, node string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: Namespace, Labels: map[string]string{"app": "router"}},
		Spec:       corev1.PodSpec{NodeName: node},
	}
}
