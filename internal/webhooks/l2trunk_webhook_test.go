// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/logging"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateL2Trunk(t *testing.T) {
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
			name: "unsupported trunk type",
			newTrunk: &v1alpha1.L2Trunk{
				ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "default"},
				Spec:       v1alpha1.L2TrunkSpec{Type: "Host"},
			},
			wantErr: `L2Trunk/host: unsupported L2Trunk type "Host"`,
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
			wantErr:  `invalid l2trunks for node "node2": L2Trunk/b: PF enp1s0 is already used by L2Trunk a`,
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
