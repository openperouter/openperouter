// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	testNodeName  = "worker-1"
	testNamespace = "openperouter-system"
	testNodeUID   = "test-uid-12345"
)

func TestMirrorController(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		expectedCRs expectedResources
		updateFiles map[string]string  // optional: files to write after initial reconcile
		deleteFiles []string           // optional: files to delete after initial reconcile
		updatedCRs  *expectedResources // optional: expected state after update
		preExisting []client.Object    // optional: resources that already exist
	}{
		{
			name: "creates underlay from static file",
			files: map[string]string{
				"openpe_underlay.yaml": `underlays:
  - asn: 64514
    routeridcidr: "10.0.0.0/24"
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`,
			},
			expectedCRs: expectedResources{
				underlays:        1,
				validateUnderlay: validateUnderlayLabelsAndSelector,
			},
		},
		{
			name: "creates multiple resource types",
			files: map[string]string{
				"openpe_underlay.yaml": `underlays:
  - asn: 64514
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`,
				"openpe_l3vni.yaml": `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
  - name: l3vni-blue
    vrf: blue
    vni: 200
`,
				"openpe_l2vni.yaml": `l2vnis:
  - name: l2vni-300
    vni: 300
    vxlanport: 4789
`,
			},
			expectedCRs: expectedResources{
				underlays:     1,
				l3vnis:        2,
				l2vnis:        1,
				validateL3VNI: validateL3VNI(map[string]int32{"red": 100, "blue": 200}),
				validateL2VNI: validateL2VNI(300),
			},
		},
		{
			name: "updates resource when static file changes",
			files: map[string]string{
				"openpe_underlay.yaml": `underlays:
  - asn: 64514
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`,
			},
			expectedCRs: expectedResources{
				underlays:        1,
				validateUnderlay: validateUnderlayASN(64514),
			},
			updateFiles: map[string]string{
				"openpe_underlay.yaml": `underlays:
  - asn: 64515
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`,
			},
			updatedCRs: &expectedResources{
				underlays:        1,
				validateUnderlay: validateUnderlayASNAndLabel(64515),
			},
		},
		{
			name: "deletes stale resources when removed from config",
			files: map[string]string{
				"openpe_l3vni.yaml": `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
  - name: l3vni-blue
    vrf: blue
    vni: 200
`,
			},
			expectedCRs: expectedResources{
				l3vnis: 2,
			},
			updateFiles: map[string]string{
				"openpe_l3vni.yaml": `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`,
			},
			updatedCRs: &expectedResources{
				l3vnis:        1,
				validateL3VNI: validateL3VNI(map[string]int32{"red": 100}),
			},
		},
		{
			name: "deletes all when static files removed",
			files: map[string]string{
				"openpe_underlay.yaml": `underlays:
  - asn: 64514
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`,
				"openpe_l3vni.yaml": `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`,
			},
			expectedCRs: expectedResources{
				underlays: 1,
				l3vnis:    1,
			},
			deleteFiles: []string{"openpe_underlay.yaml", "openpe_l3vni.yaml"},
			updatedCRs: &expectedResources{
				underlays: 0,
				l3vnis:    0,
			},
		},
		{
			name: "does not delete resources from other nodes",
			preExisting: []client.Object{
				&v1alpha1.L3VNI{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "static-node-b-l3vni-0",
						Namespace: testNamespace,
						Labels: map[string]string{
							StaticSourceLabel: StaticSourceValue,
							StaticNodeLabel:   "node-b",
						},
					},
					Spec: v1alpha1.L3VNISpec{VRF: "green", VNI: 500},
				},
			},
			files: map[string]string{
				"openpe_l3vni.yaml": `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`,
			},
			expectedCRs: expectedResources{
				l3vnis:        2, // worker-1 + node-b
				validateL3VNI: validateNodeBL3VNIUnchanged,
			},
			deleteFiles: []string{"openpe_l3vni.yaml"},
			updatedCRs: &expectedResources{
				l3vnis: 1, // only node-b remains
			},
		},
		{
			name: "adds new file while preserving existing",
			files: map[string]string{
				"openpe_underlay.yaml": `underlays:
  - asn: 64514
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`,
			},
			expectedCRs: expectedResources{
				underlays: 1,
			},
			updateFiles: map[string]string{
				"openpe_l3vni.yaml": `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`,
			},
			updatedCRs: &expectedResources{
				underlays: 1,
				l3vnis:    1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Write initial files
			for name, content := range tt.files {
				writeStaticFile(t, dir, name, content)
			}

			mc := newMirrorController(t, dir, append([]client.Object{testNode()}, tt.preExisting...)...)
			reconcile(t, mc)

			// Check initial state
			assertExpectedResources(t, mc, tt.expectedCRs)

			// Apply updates if specified
			if len(tt.updateFiles) > 0 || len(tt.deleteFiles) > 0 {
				for name, content := range tt.updateFiles {
					writeStaticFile(t, dir, name, content)
				}
				for _, name := range tt.deleteFiles {
					removeStaticFile(t, dir, name)
				}
				reconcile(t, mc)

				if tt.updatedCRs != nil {
					assertExpectedResources(t, mc, *tt.updatedCRs)
				}
			}
		})
	}
}

type expectedResources struct {
	underlays         int
	l3vnis            int
	l2vnis            int
	l3passthroughs    int
	rawfrrconfigs     int
	validateUnderlay  func(*testing.T, *v1alpha1.Underlay)
	validateL3VNI     func(*testing.T, *v1alpha1.L3VNI)
	validateL2VNI     func(*testing.T, *v1alpha1.L2VNI)
	validateL3PT      func(*testing.T, *v1alpha1.L3Passthrough)
	validateRawConfig func(*testing.T, *v1alpha1.RawFRRConfig)
}

func assertExpectedResources(t *testing.T, mc *MirrorController, expected expectedResources) {
	t.Helper()
	ctx := context.Background()
	matchLabels := client.MatchingLabels{StaticSourceLabel: StaticSourceValue}
	inNs := client.InNamespace(testNamespace)

	var underlays v1alpha1.UnderlayList
	if err := mc.List(ctx, &underlays, matchLabels, inNs); err != nil {
		t.Fatalf("failed to list underlays: %v", err)
	}
	if len(underlays.Items) != expected.underlays {
		t.Errorf("expected %d underlays, got %d", expected.underlays, len(underlays.Items))
	}
	if expected.validateUnderlay != nil {
		for i := range underlays.Items {
			expected.validateUnderlay(t, &underlays.Items[i])
		}
	}

	var l3vnis v1alpha1.L3VNIList
	if err := mc.List(ctx, &l3vnis, matchLabels, inNs); err != nil {
		t.Fatalf("failed to list l3vnis: %v", err)
	}
	if len(l3vnis.Items) != expected.l3vnis {
		t.Errorf("expected %d l3vnis, got %d", expected.l3vnis, len(l3vnis.Items))
	}
	if expected.validateL3VNI != nil {
		for i := range l3vnis.Items {
			expected.validateL3VNI(t, &l3vnis.Items[i])
		}
	}

	var l2vnis v1alpha1.L2VNIList
	if err := mc.List(ctx, &l2vnis, matchLabels, inNs); err != nil {
		t.Fatalf("failed to list l2vnis: %v", err)
	}
	if len(l2vnis.Items) != expected.l2vnis {
		t.Errorf("expected %d l2vnis, got %d", expected.l2vnis, len(l2vnis.Items))
	}
	if expected.validateL2VNI != nil {
		for i := range l2vnis.Items {
			expected.validateL2VNI(t, &l2vnis.Items[i])
		}
	}

	var l3pts v1alpha1.L3PassthroughList
	if err := mc.List(ctx, &l3pts, matchLabels, inNs); err != nil {
		t.Fatalf("failed to list l3passthroughs: %v", err)
	}
	if len(l3pts.Items) != expected.l3passthroughs {
		t.Errorf("expected %d l3passthroughs, got %d", expected.l3passthroughs, len(l3pts.Items))
	}
	if expected.validateL3PT != nil {
		for i := range l3pts.Items {
			expected.validateL3PT(t, &l3pts.Items[i])
		}
	}

	var rawCfgs v1alpha1.RawFRRConfigList
	if err := mc.List(ctx, &rawCfgs, matchLabels, inNs); err != nil {
		t.Fatalf("failed to list rawfrrconfigs: %v", err)
	}
	if len(rawCfgs.Items) != expected.rawfrrconfigs {
		t.Errorf("expected %d rawfrrconfigs, got %d", expected.rawfrrconfigs, len(rawCfgs.Items))
	}
	if expected.validateRawConfig != nil {
		for i := range rawCfgs.Items {
			expected.validateRawConfig(t, &rawCfgs.Items[i])
		}
	}
}

func TestMirrorController_RecreatesExternallyDeletedResource(t *testing.T) {
	dir := t.TempDir()
	writeStaticFile(t, dir, "openpe_l3vni.yaml", `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`)
	mc := newMirrorController(t, dir, testNode())
	reconcile(t, mc)

	ctx := context.Background()
	key := client.ObjectKey{Name: "static-worker-1-l3vni-red", Namespace: testNamespace}
	var v v1alpha1.L3VNI
	if err := mc.Get(ctx, key, &v); err != nil {
		t.Fatalf("expected L3VNI to exist: %v", err)
	}
	if err := mc.Delete(ctx, &v); err != nil {
		t.Fatalf("failed to delete L3VNI: %v", err)
	}
	reconcile(t, mc)
	if err := mc.Get(ctx, key, &v); err != nil {
		t.Fatalf("expected L3VNI to be recreated: %v", err)
	}
	if v.Spec.VNI != 100 {
		t.Errorf("expected VNI 100, got %d", v.Spec.VNI)
	}
}

func TestMirrorController_IdempotentMultipleReconciles(t *testing.T) {
	dir := t.TempDir()
	writeStaticFile(t, dir, "openpe_underlay.yaml", `underlays:
  - asn: 64514
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`)
	writeStaticFile(t, dir, "openpe_l3vni.yaml", `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`)
	mc := newMirrorController(t, dir, testNode())

	for range 3 {
		reconcile(t, mc)
	}

	assertExpectedResources(t, mc, expectedResources{
		underlays: 1,
		l3vnis:    1,
	})
}

func TestMirrorController_OverwritesExternallyModifiedResource(t *testing.T) {
	dir := t.TempDir()
	writeStaticFile(t, dir, "openpe_l3vni.yaml", `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`)
	mc := newMirrorController(t, dir, testNode())
	reconcile(t, mc)

	ctx := context.Background()
	key := client.ObjectKey{Name: "static-worker-1-l3vni-red", Namespace: testNamespace}

	// External actor changes VNI
	var v v1alpha1.L3VNI
	if err := mc.Get(ctx, key, &v); err != nil {
		t.Fatalf("expected L3VNI to exist: %v", err)
	}
	v.Spec.VNI = 999
	if err := mc.Update(ctx, &v); err != nil {
		t.Fatalf("failed to update L3VNI externally: %v", err)
	}

	// Reconcile should revert
	reconcile(t, mc)

	var reverted v1alpha1.L3VNI
	if err := mc.Get(ctx, key, &reverted); err != nil {
		t.Fatalf("expected L3VNI to exist after revert: %v", err)
	}
	if reverted.Spec.VNI != 100 {
		t.Errorf("expected VNI reverted to 100, got %d", reverted.Spec.VNI)
	}
}

func TestMirrorController_RestoresLabelAfterExternalRemoval(t *testing.T) {
	dir := t.TempDir()
	writeStaticFile(t, dir, "openpe_l3vni.yaml", `l3vnis:
  - name: l3vni-red
    vrf: red
    vni: 100
`)
	mc := newMirrorController(t, dir, testNode())
	reconcile(t, mc)

	ctx := context.Background()
	key := client.ObjectKey{Name: "static-worker-1-l3vni-red", Namespace: testNamespace}

	// External actor removes label
	var v v1alpha1.L3VNI
	if err := mc.Get(ctx, key, &v); err != nil {
		t.Fatalf("expected L3VNI to exist: %v", err)
	}
	v.Labels = map[string]string{}
	if err := mc.Update(ctx, &v); err != nil {
		t.Fatalf("failed to remove label externally: %v", err)
	}

	// Reconcile should restore
	reconcile(t, mc)

	var restored v1alpha1.L3VNI
	if err := mc.Get(ctx, key, &restored); err != nil {
		t.Fatalf("expected L3VNI to exist after restore: %v", err)
	}
	if restored.Labels[StaticSourceLabel] != StaticSourceValue {
		t.Errorf("expected label restored, got %v", restored.Labels)
	}
}

func validateUnderlayLabelsAndSelector(t *testing.T, u *v1alpha1.Underlay) {
	t.Helper()
	if u.Spec.ASN != 64514 {
		t.Errorf("expected ASN 64514, got %d", u.Spec.ASN)
	}
	if u.Labels[StaticSourceLabel] != StaticSourceValue {
		t.Errorf("missing static source label")
	}
	if u.Labels[StaticNodeLabel] != testNodeName {
		t.Errorf("expected node label %s, got %s", testNodeName, u.Labels[StaticNodeLabel])
	}
	if u.Spec.NodeSelector == nil || u.Spec.NodeSelector.MatchLabels["kubernetes.io/hostname"] != testNodeName {
		t.Errorf("expected node selector for %s", testNodeName)
	}
	if u.Spec.TunnelEndpoint == nil || len(u.Spec.TunnelEndpoint.CIDRs) != 1 || u.Spec.TunnelEndpoint.CIDRs[0] != "100.65.0.0/24" {
		t.Errorf("expected tunnel endpoint with CIDR 100.65.0.0/24, got %+v", u.Spec.TunnelEndpoint)
	}
}

func validateUnderlayASN(expectedASN int64) func(*testing.T, *v1alpha1.Underlay) {
	return func(t *testing.T, u *v1alpha1.Underlay) {
		t.Helper()
		if u.Spec.ASN != expectedASN {
			t.Errorf("expected ASN %d, got %d", expectedASN, u.Spec.ASN)
		}
	}
}

func validateUnderlayASNAndLabel(expectedASN int64) func(*testing.T, *v1alpha1.Underlay) {
	return func(t *testing.T, u *v1alpha1.Underlay) {
		t.Helper()
		if u.Spec.ASN != expectedASN {
			t.Errorf("expected ASN %d, got %d", expectedASN, u.Spec.ASN)
		}
		if u.Labels[StaticSourceLabel] != StaticSourceValue {
			t.Error("label lost after update")
		}
	}
}

func validateL3VNI(expected map[string]int32) func(*testing.T, *v1alpha1.L3VNI) {
	return func(t *testing.T, l *v1alpha1.L3VNI) {
		t.Helper()
		expectedVNI, ok := expected[l.Spec.VRF]
		if !ok {
			t.Errorf("unexpected L3VNI VRF %s", l.Spec.VRF)
			return
		}
		if l.Spec.VNI != expectedVNI {
			t.Errorf("L3VNI VRF %s: expected VNI %d, got %d", l.Spec.VRF, expectedVNI, l.Spec.VNI)
		}
	}
}

func validateL2VNI(expectedVNI int32) func(*testing.T, *v1alpha1.L2VNI) {
	return func(t *testing.T, l *v1alpha1.L2VNI) {
		t.Helper()
		if l.Spec.VNI != expectedVNI {
			t.Errorf("expected L2VNI VNI %d, got %d", expectedVNI, l.Spec.VNI)
		}
	}
}

func validateNodeBL3VNIUnchanged(t *testing.T, l *v1alpha1.L3VNI) {
	t.Helper()
	if l.Name == "static-node-b-l3vni-0" && l.Spec.VNI != 500 {
		t.Errorf("node-b resource was modified")
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add v1alpha1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	return s
}

func testNode() *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNodeName,
			UID:  types.UID(testNodeUID),
		},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newMirrorController(t *testing.T, configDir string, objects ...client.Object) *MirrorController {
	t.Helper()
	s := testScheme(t)
	cli := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha1.Underlay{}, &v1alpha1.L3VNI{}, &v1alpha1.L2VNI{},
			&v1alpha1.L3Passthrough{}, &v1alpha1.RawFRRConfig{}).
		Build()
	return &MirrorController{
		Client:      cli,
		Scheme:      s,
		Logger:      testLogger(),
		MyNode:      testNodeName,
		MyNamespace: testNamespace,
		ConfigDir:   configDir,
		TriggerChan: make(chan event.GenericEvent, 1),
	}
}

func reconcile(t *testing.T, mc *MirrorController) {
	t.Helper()
	_, err := mc.Reconcile(context.Background(), ctrl.Request{})
	if err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}
}

func writeStaticFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write static file %s: %v", filename, err)
	}
}

func removeStaticFile(t *testing.T, dir, filename string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, filename)); err != nil {
		t.Fatalf("failed to remove static file %s: %v", filename, err)
	}
}
