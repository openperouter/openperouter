// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/logging"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func externalHostMasterForTest(name string) *v1alpha1.HostMaster {
	return &v1alpha1.HostMaster{
		Type: v1alpha1.LinuxBridge,
		LinuxBridge: &v1alpha1.LinuxBridgeConfig{
			Lifecycle: v1alpha1.BridgeLifecycleExternal,
			Name:      ptr.To(name),
		},
	}
}

func trunkForTest(name string, hostMaster *v1alpha1.HostMaster) *v1alpha1.L2Trunk {
	return &v1alpha1.L2Trunk{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.L2TrunkSpec{
			Type:       v1alpha1.L2TrunkTypeHost,
			HostMaster: hostMaster,
		},
	}
}

func trunkedL2VNIForTest(name, trunk string, tag, vni int32, port *int32,
	selector *metav1.LabelSelector) *v1alpha1.L2VNI {
	return &v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.L2VNISpec{
			VNI:          vni,
			VXLanPort:    port,
			NodeSelector: selector,
			Trunk: &v1alpha1.L2TrunkReference{
				Name: trunk,
				VLAN: v1alpha1.VLANConfig{Tag: tag},
			},
		},
	}
}

func TestValidateL2Trunk(t *testing.T) {
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1"}}
	otherNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2"}}
	selectorFor := func(nodeName string) *metav1.LabelSelector {
		return &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": nodeName}}
	}

	tests := []struct {
		name        string
		l2trunks    []*v1alpha1.L2Trunk
		l2vnis      []*v1alpha1.L2VNI
		nodes       []*v1.Node
		newTrunk    *v1alpha1.L2Trunk
		errorString string
	}{
		{
			name:     "a trunk with no L2VNIs is admitted",
			nodes:    []*v1.Node{node},
			newTrunk: trunkForTest("workloads", nil),
		},
		{
			name:  "an L2VNI reusing a tag on the same trunk is rejected",
			nodes: []*v1.Node{node},
			l2vnis: []*v1alpha1.L2VNI{
				trunkedL2VNIForTest("first", "workloads", 100, 10100, nil, nil),
				trunkedL2VNIForTest("second", "workloads", 100, 10200, nil, nil),
			},
			newTrunk:    trunkForTest("workloads", nil),
			errorString: `duplicate vlan tag 100 on L2Trunk "workloads": first, second`,
		},
		{
			name:  "a second trunk on the same host master may not reuse a tag",
			nodes: []*v1.Node{node},
			l2trunks: []*v1alpha1.L2Trunk{
				trunkForTest("existing", externalHostMasterForTest("br-ex")),
			},
			l2vnis: []*v1alpha1.L2VNI{
				trunkedL2VNIForTest("first", "existing", 100, 10100, nil, nil),
				trunkedL2VNIForTest("second", "workloads", 100, 10200, nil, nil),
			},
			newTrunk:    trunkForTest("workloads", externalHostMasterForTest("br-ex")),
			errorString: "duplicate vlan tag 100 across L2Trunks sharing host master",
		},
		{
			name:  "trunks on different host masters may reuse a tag",
			nodes: []*v1.Node{node},
			l2trunks: []*v1alpha1.L2Trunk{
				trunkForTest("existing", externalHostMasterForTest("br-ex-a")),
			},
			l2vnis: []*v1alpha1.L2VNI{
				trunkedL2VNIForTest("first", "existing", 100, 10100, nil, nil),
				trunkedL2VNIForTest("second", "workloads", 100, 10200, nil, nil),
			},
			newTrunk: trunkForTest("workloads", externalHostMasterForTest("br-ex-b")),
		},
		{
			name:  "L2VNIs on one trunk may not disagree on vxlanPort",
			nodes: []*v1.Node{node},
			l2vnis: []*v1alpha1.L2VNI{
				trunkedL2VNIForTest("first", "workloads", 100, 10100, ptr.To(int32(4789)), nil),
				trunkedL2VNIForTest("second", "workloads", 200, 10200, ptr.To(int32(4790)), nil),
			},
			newTrunk:    trunkForTest("workloads", nil),
			errorString: "disagree on vxlanPort or underlayAddressFamily",
		},
		{
			name:  "disjoint node selectors allow a tag collision",
			nodes: []*v1.Node{node, otherNode},
			l2vnis: []*v1alpha1.L2VNI{
				trunkedL2VNIForTest("first", "workloads", 100, 10100, nil, selectorFor("node1")),
				trunkedL2VNIForTest("second", "workloads", 100, 10200, nil, selectorFor("node2")),
			},
			newTrunk: trunkForTest("workloads", nil),
		},
		{
			name:  "a managed host master is rejected",
			nodes: []*v1.Node{node},
			newTrunk: trunkForTest("workloads", &v1alpha1.HostMaster{
				Type: v1alpha1.LinuxBridge,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			}),
			errorString: "requires an External host master",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := objectsFromResources(tt.l2trunks)
			objects = append(objects, objectsFromResources(tt.l2vnis)...)
			objects = append(objects, objectsFromResources(tt.nodes)...)
			webhookClient, err := setupFakeWebhookClient(objects)
			if err != nil {
				t.Fatal(err)
			}
			origWebhookClient := WebhookClient
			origLogger := Logger
			defer func() {
				WebhookClient = origWebhookClient
				Logger = origLogger
			}()
			WebhookClient = webhookClient
			Logger, _ = logging.New("debug")

			err = validateL2Trunk(tt.newTrunk)
			if tt.errorString == "" {
				if err != nil {
					t.Fatalf("expected no error, but got %q", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error to contain %q but got no error", tt.errorString)
			}
			if !strings.Contains(err.Error(), tt.errorString) {
				t.Fatalf("expected error message %q to contain substring %q", err.Error(), tt.errorString)
			}
		})
	}
}

func TestValidateL2TrunkSRIOV(t *testing.T) {
	nodes := []*v1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"name": "node1"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"name": "node2"}}},
	}

	tests := []struct {
		name     string
		existing []*v1alpha1.L2Trunk
		newTrunk *v1alpha1.L2Trunk
		wantErr  string
	}{
		{
			name:     "first trunk",
			newTrunk: webhookSRIOVTrunk("a", 1, nil),
		},
		{
			name: "host trunk is accepted",
			newTrunk: &v1alpha1.L2Trunk{
				ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "default"},
				Spec:       v1alpha1.L2TrunkSpec{Type: v1alpha1.L2TrunkTypeHost},
			},
		},
		{
			name: "unsupported trunk type",
			newTrunk: &v1alpha1.L2Trunk{
				ObjectMeta: metav1.ObjectMeta{Name: "unsupported", Namespace: "default"},
				Spec:       v1alpha1.L2TrunkSpec{Type: "Foo"},
			},
			wantErr: `L2Trunk/unsupported: unsupported type "Foo" for L2Trunk "unsupported"`,
		},
		{
			name:     "different VF of the same PF",
			existing: []*v1alpha1.L2Trunk{webhookSRIOVTrunk("a", 1, nil)},
			newTrunk: webhookSRIOVTrunk("b", 2, nil),
			wantErr:  `L2Trunk/b: PF enp1s0 is already used by L2Trunk a`,
		},
		{
			name:     "update keeps its own VF",
			existing: []*v1alpha1.L2Trunk{webhookSRIOVTrunk("a", 1, nil)},
			newTrunk: webhookSRIOVTrunk("a", 1, nil),
		},
		{
			name: "same VF on different nodes",
			existing: []*v1alpha1.L2Trunk{
				webhookSRIOVTrunk("a", 1, map[string]string{"name": "node1"}),
			},
			newTrunk: webhookSRIOVTrunk("b", 1, map[string]string{"name": "node2"}),
		},
		{
			name:     "same VF on the same node",
			existing: []*v1alpha1.L2Trunk{webhookSRIOVTrunk("a", 1, nil)},
			newTrunk: webhookSRIOVTrunk("b", 1, map[string]string{"name": "node2"}),
			wantErr:  `invalid overlay resources for node "node2": L2Trunk/b: PF enp1s0 is already used by L2Trunk a`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := append(objectsFromResources(tt.existing), objectsFromResources(nodes)...)
			client, err := setupFakeWebhookClient(objects)
			require.NoError(t, err)

			origWebhookClient := WebhookClient
			origLogger := Logger
			t.Cleanup(func() {
				WebhookClient = origWebhookClient
				Logger = origLogger
			})
			WebhookClient = client
			Logger, _ = logging.New("debug")

			err = validateL2Trunk(tt.newTrunk)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestOrphanedL2VNIWarnings(t *testing.T) {
	l2vnis := []*v1alpha1.L2VNI{
		trunkedL2VNIForTest("first", "workloads", 100, 10100, nil, nil),
		trunkedL2VNIForTest("second", "workloads", 200, 10200, nil, nil),
		trunkedL2VNIForTest("unrelated", "other", 300, 10300, nil, nil),
	}
	webhookClient, err := setupFakeWebhookClient(objectsFromResources(l2vnis))
	if err != nil {
		t.Fatal(err)
	}
	origWebhookClient := WebhookClient
	defer func() { WebhookClient = origWebhookClient }()
	WebhookClient = webhookClient

	warnings, err := orphanedL2VNIWarnings(*trunkForTest("workloads", nil))
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"L2VNI default/first references this L2Trunk and will lose its dataplane configuration",
		"L2VNI default/second references this L2Trunk and will lose its dataplane configuration",
	}
	if len(warnings) != len(expected) {
		t.Fatalf("expected %d warnings, got %v", len(expected), warnings)
	}
	for i := range expected {
		if warnings[i] != expected[i] {
			t.Errorf("warning %d = %q, want %q", i, warnings[i], expected[i])
		}
	}
}

func webhookSRIOVTrunk(name string, vfIndex int32, nodeLabels map[string]string) *v1alpha1.L2Trunk {
	trunk := &v1alpha1.L2Trunk{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: v1alpha1.L2TrunkSpec{
			Type:  v1alpha1.L2TrunkTypeSRIOV,
			SRIOV: &v1alpha1.SRIOVTrunkConfig{PFName: "enp1s0", VFIndex: new(vfIndex)},
		},
	}
	if nodeLabels != nil {
		trunk.Spec.NodeSelector = &metav1.LabelSelector{MatchLabels: nodeLabels}
	}
	return trunk
}
