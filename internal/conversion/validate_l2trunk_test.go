// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
)

func TestFilterValidL2Trunks(t *testing.T) {
	tests := []struct {
		name       string
		trunks     []v1alpha1.L2Trunk
		wantValid  []string
		wantFailed []v1alpha1.FailedResource
	}{
		{
			name: "host trunk",
			trunks: []v1alpha1.L2Trunk{
				// A type outside the enum can only come from a CR created
				// before the type was dropped or one that bypassed CRD
				// validation; it must still be rejected here.
				hostTrunk(),
				sriovTrunk("vf1", "enp1s0", 1),
			},
			wantValid: []string{"vf1"},
			wantFailed: []v1alpha1.FailedResource{
				{
					Kind: openpeerrors.KindL2Trunk, Name: "host",
					Reason:  v1alpha1.FailedResourceReasonValidationFailed,
					Message: `unsupported L2Trunk type "Host"`,
				},
			},
		},
		{
			name: "different VFs of the same PF",
			trunks: []v1alpha1.L2Trunk{
				sriovTrunk("vf1", "enp1s0", 1),
				sriovTrunk("vf2", "enp1s0", 2),
			},
			wantValid: []string{"vf1"},
			wantFailed: []v1alpha1.FailedResource{
				{
					Kind: openpeerrors.KindL2Trunk, Name: "vf2",
					Reason:  v1alpha1.FailedResourceReasonValidationFailed,
					Message: "PF enp1s0 is already used by L2Trunk vf1",
				},
			},
		},
		{
			name: "same VF index on different PFs",
			trunks: []v1alpha1.L2Trunk{
				sriovTrunk("a", "enp1s0", 1),
				sriovTrunk("b", "enp2s0", 1),
			},
			wantValid: []string{"a", "b"},
		},
		{
			name: "SR-IOV trunk without sriov",
			trunks: []v1alpha1.L2Trunk{{
				ObjectMeta: metav1.ObjectMeta{Name: "broken"},
				Spec:       v1alpha1.L2TrunkSpec{Type: v1alpha1.L2TrunkTypeSRIOV},
			}},
			wantValid: []string{},
			wantFailed: []v1alpha1.FailedResource{
				{
					Kind: openpeerrors.KindL2Trunk, Name: "broken",
					Reason:  v1alpha1.FailedResourceReasonValidationFailed,
					Message: "sriov must be set when type is SRIOV",
				},
			},
		},
		{
			name:      "invalid PF name",
			trunks:    []v1alpha1.L2Trunk{sriovTrunk("bad", "1enp", 1)},
			wantValid: []string{},
			wantFailed: []v1alpha1.FailedResource{
				{
					Kind: openpeerrors.KindL2Trunk, Name: "bad",
					Reason:  v1alpha1.FailedResourceReasonValidationFailed,
					Message: `invalid pfName "1enp": interface name 1enp contains invalid characters`,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := FilterValidL2Trunks(tt.trunks)
			require.Equal(t, tt.wantValid, trunkNames(valid))
			require.Equal(t, tt.wantFailed, openpeerrors.CollectFailures(err))
		})
	}
}

func TestValidateL2TrunksForNodes(t *testing.T) {
	nodes := []corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"name": "node1"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"name": "node2"}}},
	}

	tests := []struct {
		name    string
		trunks  []v1alpha1.L2Trunk
		wantErr string
	}{
		{
			name: "same VF on different nodes",
			trunks: []v1alpha1.L2Trunk{
				onNode(sriovTrunk("a", "enp1s0", 1), "node1"),
				onNode(sriovTrunk("b", "enp1s0", 1), "node2"),
			},
		},
		{
			name: "same PF on the same node",
			trunks: []v1alpha1.L2Trunk{
				onNode(sriovTrunk("a", "enp1s0", 1), "node1"),
				sriovTrunk("b", "enp1s0", 2),
			},
			wantErr: `invalid l2trunks for node "node1": L2Trunk/b: PF enp1s0 is already used by L2Trunk a`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateL2TrunksForNodes(nodes, tt.trunks)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestFilterUniqueTrunkVLANs(t *testing.T) {
	tests := []struct {
		name       string
		l2vnis     []v1alpha1.L2VNI
		wantValid  []string
		wantFailed []v1alpha1.FailedResource
	}{
		{
			name: "different tags on the same trunk",
			l2vnis: []v1alpha1.L2VNI{
				trunkL2VNI("a", 100, "workloads", 100),
				trunkL2VNI("b", 200, "workloads", 200),
			},
			wantValid: []string{"a", "b"},
		},
		{
			name: "same tag on different trunks",
			l2vnis: []v1alpha1.L2VNI{
				trunkL2VNI("a", 100, "workloads", 100),
				trunkL2VNI("b", 200, "other", 100),
			},
			wantValid: []string{"a", "b"},
		},
		{
			name: "standalone L2VNIs are ignored",
			l2vnis: []v1alpha1.L2VNI{
				{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Spec: v1alpha1.L2VNISpec{VNI: 100}},
				{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Spec: v1alpha1.L2VNISpec{VNI: 200}},
			},
			wantValid: []string{"a", "b"},
		},
		{
			name: "same tag on the same trunk",
			l2vnis: []v1alpha1.L2VNI{
				trunkL2VNI("a", 100, "workloads", 100),
				trunkL2VNI("b", 200, "workloads", 100),
			},
			wantValid: []string{"a"},
			wantFailed: []v1alpha1.FailedResource{
				{
					Kind: openpeerrors.KindL2VNI, Name: "b",
					Reason:  v1alpha1.FailedResourceReasonValidationFailed,
					Message: "VLAN 100 on L2Trunk workloads is already used by L2VNI a",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := FilterUniqueTrunkVLANs(tt.l2vnis)
			names := make([]string, 0, len(valid))
			for _, l2vni := range valid {
				names = append(names, l2vni.Name)
			}
			require.Equal(t, tt.wantValid, names)
			require.Equal(t, tt.wantFailed, openpeerrors.CollectFailures(err))
		})
	}
}

func TestDatapathValidatorsL2Trunks(t *testing.T) {
	tests := []struct {
		name      string
		validator DatapathConfigValidator
		trunk     v1alpha1.L2Trunk
		wantErr   string
	}{
		{
			name:      "grout accepts SR-IOV trunks",
			validator: &GroutDatapathConfigValidator{},
			trunk:     sriovTrunk("vf", "enp1s0", 1),
		},
		{
			name:      "grout rejects host trunks",
			validator: &GroutDatapathConfigValidator{},
			trunk:     hostTrunk(),
			wantErr:   "l2trunk host: type Host is not supported with the grout datapath",
		},
		{
			name:      "kernel rejects SR-IOV trunks",
			validator: &KernelDatapathConfigValidator{},
			trunk:     sriovTrunk("vf", "enp1s0", 1),
			wantErr:   "l2trunk vf: L2Trunks are only supported with the grout datapath",
		},
		{
			name:      "kernel rejects host trunks",
			validator: &KernelDatapathConfigValidator{},
			trunk:     hostTrunk(),
			wantErr:   "l2trunk host: L2Trunks are only supported with the grout datapath",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validator.Validate(APIConfigData{L2Trunks: []v1alpha1.L2Trunk{tt.trunk}})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

// hostTrunk builds a trunk whose type is outside the current enum, as a CR
// created before the Host type was dropped would look like.
func hostTrunk() v1alpha1.L2Trunk {
	return v1alpha1.L2Trunk{
		ObjectMeta: metav1.ObjectMeta{Name: "host"},
		Spec:       v1alpha1.L2TrunkSpec{Type: "Host"},
	}
}

func sriovTrunk(name, pfName string, vfIndex int32) v1alpha1.L2Trunk {
	return v1alpha1.L2Trunk{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.L2TrunkSpec{
			Type:  v1alpha1.L2TrunkTypeSRIOV,
			SRIOV: &v1alpha1.SRIOVTrunkConfig{PFName: pfName, VFIndex: new(vfIndex)},
		},
	}
}

func onNode(trunk v1alpha1.L2Trunk, node string) v1alpha1.L2Trunk {
	trunk.Spec.NodeSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"name": node}}
	return trunk
}

func trunkL2VNI(name string, vni int32, trunk string, tag int32) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.L2VNISpec{
			VNI:   vni,
			Trunk: &v1alpha1.L2TrunkReference{Name: trunk, VLAN: v1alpha1.VLANConfig{Tag: tag}},
		},
	}
}

func trunkNames(trunks []v1alpha1.L2Trunk) []string {
	names := make([]string, 0, len(trunks))
	for _, trunk := range trunks {
		names = append(names, trunk.Name)
	}
	return names
}
