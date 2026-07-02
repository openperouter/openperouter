// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var failingHostConfigurator = HostConfigurator(func(_ context.Context, _ interfacesConfiguration) error {
	return fmt.Errorf("failed to setup underlay: link not found")
})

func TestReconcileUpdatesNodeStatus(t *testing.T) {
	t.Run("sets Ready status on successful reconcile", func(t *testing.T) {
		r := newTestPERouterReconciler(t, noopHostConfigurator, testNode())

		if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
			t.Fatalf("Reconcile() returned error: %v", err)
		}

		assertStatusReady(t, r.Client)
	})

	t.Run("sets Degraded status on reconcile error", func(t *testing.T) {
		r := newTestPERouterReconciler(t, failingHostConfigurator, testNode())

		_, _ = r.Reconcile(context.Background(), ctrl.Request{})

		assertStatusDegraded(t, r.Client)
	})

	t.Run("no status patch on repeated successful reconcile", func(t *testing.T) {
		r := newTestPERouterReconciler(t, noopHostConfigurator, testNode())

		if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
			t.Fatalf("first Reconcile() returned error: %v", err)
		}
		first := getNodeStatus(t, r.Client)
		firstRV := first.ResourceVersion

		if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
			t.Fatalf("second Reconcile() returned error: %v", err)
		}
		second := getNodeStatus(t, r.Client)

		if second.ResourceVersion != firstRV {
			t.Errorf("ResourceVersion changed from %s to %s; expected no status patch (reconcile storm)", firstRV, second.ResourceVersion)
		}
	})

	t.Run("no status patch on repeated failed reconcile", func(t *testing.T) {
		r := newTestPERouterReconciler(t, failingHostConfigurator, testNode())

		_, _ = r.Reconcile(context.Background(), ctrl.Request{})
		first := getNodeStatus(t, r.Client)
		firstRV := first.ResourceVersion

		_, _ = r.Reconcile(context.Background(), ctrl.Request{})
		second := getNodeStatus(t, r.Client)

		if second.ResourceVersion != firstRV {
			t.Errorf("ResourceVersion changed from %s to %s; expected no status patch (reconcile storm)", firstRV, second.ResourceVersion)
		}
	})

	t.Run("transitions from Degraded to Ready", func(t *testing.T) {
		r := newTestPERouterReconciler(t, failingHostConfigurator, testNode())

		_, _ = r.Reconcile(context.Background(), ctrl.Request{})
		assertStatusDegraded(t, r.Client)

		r.HostConfigurator = noopHostConfigurator
		if _, err := r.Reconcile(context.Background(), ctrl.Request{}); err != nil {
			t.Fatalf("Reconcile() returned error: %v", err)
		}
		assertStatusReady(t, r.Client)
	})
}

type mockRouterProvider struct{}

func (m *mockRouterProvider) New(_ context.Context) (Router, error) {
	return &mockRouter{}, nil
}

func (m *mockRouterProvider) NodeIndex(_ context.Context) (int, error) {
	return 0, nil
}

type mockRouter struct{}

func (m *mockRouter) TargetNS(_ context.Context) (string, error)        { return "", nil }
func (m *mockRouter) CanReconcile(_ context.Context) (bool, error)      { return true, nil }
func (m *mockRouter) HandleNonRecoverableError(_ context.Context) error { return nil }

func newTestPERouterReconciler(t *testing.T, hostConfigurator HostConfigurator, objects ...client.Object) *PERouterReconciler {
	t.Helper()
	s := testScheme(t)
	cli := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha1.RouterNodeConfigurationStatus{}).
		Build()
	notMirrored, err := labels.NewRequirement(StaticSourceLabel, selection.DoesNotExist, nil)
	if err != nil {
		t.Fatalf("failed to build label requirement: %v", err)
	}
	frrConfigPath := filepath.Join(t.TempDir(), "frr.conf")
	if err := os.WriteFile(frrConfigPath, nil, 0600); err != nil {
		t.Fatalf("failed to create frr config: %v", err)
	}
	return &PERouterReconciler{
		Client:                   cli,
		Scheme:                   s,
		MyNode:                   testNodeName,
		MyNamespace:              testNamespace,
		Logger:                   testLogger(),
		RouterProvider:           &mockRouterProvider{},
		HostConfigurator:         hostConfigurator,
		FRRReloadSocket:          newFRRSocketServer(t),
		FRRConfigPath:            frrConfigPath,
		notStaticConfigsListOpts: &client.ListOptions{LabelSelector: labels.NewSelector().Add(*notMirrored)},
	}
}

func newFRRSocketServer(t *testing.T) string {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "frr.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create unix socket: %v", err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return socketPath
}

func assertStatusReady(t *testing.T, cli client.Client) {
	t.Helper()
	status := getNodeStatus(t, cli)
	if status.Status == nil {
		t.Fatal("Status should not be nil")
	}
	ready := apimeta.FindStatusCondition(status.Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil {
		t.Fatal("Ready condition not set")
	}
	if ready.Status != metav1.ConditionTrue {
		t.Errorf("Ready status = %s, want %s", ready.Status, metav1.ConditionTrue)
	}
	degraded := apimeta.FindStatusCondition(status.Status.Conditions, v1alpha1.ConditionTypeDegraded)
	if degraded == nil {
		t.Fatal("Degraded condition not set")
	}
	if degraded.Status != metav1.ConditionFalse {
		t.Errorf("Degraded status = %s, want %s", degraded.Status, metav1.ConditionFalse)
	}
}

func assertStatusDegraded(t *testing.T, cli client.Client) {
	t.Helper()
	status := getNodeStatus(t, cli)
	if status.Status == nil {
		t.Fatal("Status should not be nil")
	}
	ready := apimeta.FindStatusCondition(status.Status.Conditions, v1alpha1.ConditionTypeReady)
	if ready == nil {
		t.Fatal("Ready condition not set")
	}
	if ready.Status != metav1.ConditionFalse {
		t.Errorf("Ready status = %s, want %s", ready.Status, metav1.ConditionFalse)
	}
	degraded := apimeta.FindStatusCondition(status.Status.Conditions, v1alpha1.ConditionTypeDegraded)
	if degraded == nil {
		t.Fatal("Degraded condition not set")
	}
	if degraded.Status != metav1.ConditionTrue {
		t.Errorf("Degraded status = %s, want %s", degraded.Status, metav1.ConditionTrue)
	}
}

func getNodeStatus(t *testing.T, cli client.Client) *v1alpha1.RouterNodeConfigurationStatus {
	t.Helper()
	status := &v1alpha1.RouterNodeConfigurationStatus{}
	if err := cli.Get(context.Background(), client.ObjectKey{
		Name:      testNodeName,
		Namespace: testNamespace,
	}, status); err != nil {
		t.Fatalf("failed to get node status: %v", err)
	}
	return status
}
