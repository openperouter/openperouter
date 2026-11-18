// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
)

func testTrunk(name string, hostMaster *v1alpha1.HostMaster) v1alpha1.L2Trunk {
	return v1alpha1.L2Trunk{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.L2TrunkSpec{
			Type:       v1alpha1.L2TrunkTypeHost,
			HostMaster: hostMaster,
		},
	}
}

func externalHostMaster(name string) *v1alpha1.HostMaster {
	return &v1alpha1.HostMaster{
		Type: v1alpha1.LinuxBridge,
		LinuxBridge: &v1alpha1.LinuxBridgeConfig{
			Lifecycle: v1alpha1.BridgeLifecycleExternal,
			Name:      ptr.To(name),
		},
	}
}

func testTrunkedL2VNI(name, trunk string, tag, vni int32, port *int32, family *string) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.L2VNISpec{
			VNI:                   vni,
			VXLanPort:             port,
			UnderlayAddressFamily: family,
			Trunk: &v1alpha1.L2TrunkReference{
				Name: trunk,
				VLAN: v1alpha1.VLANConfig{Tag: tag},
			},
		},
	}
}

func TestFilterUniqueL2VNITrunkTags(t *testing.T) {
	first := testTrunkedL2VNI("first", "workloads", 100, 10100, nil, nil)
	second := testTrunkedL2VNI("second", "workloads", 100, 10200, nil, nil)
	otherTag := testTrunkedL2VNI("other", "workloads", 200, 10300, nil, nil)

	valid, err := FilterUniqueL2VNITrunkTags([]v1alpha1.L2VNI{first, second, otherTag})
	require.Equal(t, []v1alpha1.L2VNI{otherTag}, valid)

	expected := []v1alpha1.FailedResource{
		{
			Kind:    openpeerrors.KindL2VNI,
			Name:    "first",
			Reason:  v1alpha1.FailedResourceReasonValidationFailed,
			Message: `duplicate vlan tag 100 on L2Trunk "workloads": first, second`,
		},
		{
			Kind:    openpeerrors.KindL2VNI,
			Name:    "second",
			Reason:  v1alpha1.FailedResourceReasonValidationFailed,
			Message: `duplicate vlan tag 100 on L2Trunk "workloads": first, second`,
		},
	}
	require.Equal(t, expected, openpeerrors.CollectFailures(err))
}

func TestFilterUniqueTrunkTagsAcrossHostMasters(t *testing.T) {
	firstVNI := testTrunkedL2VNI("first", "trunk-a", 100, 10100, nil, nil)
	secondVNI := testTrunkedL2VNI("second", "trunk-b", 100, 10200, nil, nil)

	tests := []struct {
		name         string
		trunks       []v1alpha1.L2Trunk
		wantAccepted bool
	}{
		{
			name: "trunks sharing one external host master may not reuse a tag",
			trunks: []v1alpha1.L2Trunk{
				testTrunk("trunk-a", externalHostMaster("br-ex")),
				testTrunk("trunk-b", externalHostMaster("br-ex")),
			},
			wantAccepted: false,
		},
		{
			name: "trunks on different host masters may reuse a tag",
			trunks: []v1alpha1.L2Trunk{
				testTrunk("trunk-a", externalHostMaster("br-ex-a")),
				testTrunk("trunk-b", externalHostMaster("br-ex-b")),
			},
			wantAccepted: true,
		},
		{
			name: "trunks with no host master are exempt",
			trunks: []v1alpha1.L2Trunk{
				testTrunk("trunk-a", nil),
				testTrunk("trunk-b", nil),
			},
			wantAccepted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := FilterUniqueTrunkTagsAcrossHostMasters(
				[]v1alpha1.L2VNI{firstVNI, secondVNI}, tt.trunks)
			if tt.wantAccepted {
				require.NoError(t, err)
				require.Equal(t, []v1alpha1.L2VNI{firstVNI, secondVNI}, valid)
				return
			}
			require.Empty(t, valid)
			require.Len(t, openpeerrors.CollectFailures(err), 2)
		})
	}
}

func TestFilterTrunkSettingsAgreement(t *testing.T) {
	ipv4 := ptr.To("IPv4")
	ipv6 := ptr.To("IPv6")

	tests := []struct {
		name         string
		l2vnis       []v1alpha1.L2VNI
		wantAccepted bool
	}{
		{
			name: "same effective port and family agree",
			l2vnis: []v1alpha1.L2VNI{
				testTrunkedL2VNI("first", "workloads", 100, 10100, nil, nil),
				testTrunkedL2VNI("second", "workloads", 200, 10200, ptr.To(int32(4789)), nil),
			},
			wantAccepted: true,
		},
		{
			name: "different ports disagree",
			l2vnis: []v1alpha1.L2VNI{
				testTrunkedL2VNI("first", "workloads", 100, 10100, ptr.To(int32(4789)), nil),
				testTrunkedL2VNI("second", "workloads", 200, 10200, ptr.To(int32(4790)), nil),
			},
			wantAccepted: false,
		},
		{
			name: "different families disagree",
			l2vnis: []v1alpha1.L2VNI{
				testTrunkedL2VNI("first", "workloads", 100, 10100, nil, ipv4),
				testTrunkedL2VNI("second", "workloads", 200, 10200, nil, ipv6),
			},
			wantAccepted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, err := FilterTrunkSettingsAgreement(tt.l2vnis)
			if tt.wantAccepted {
				require.NoError(t, err)
				require.Equal(t, tt.l2vnis, valid)
				return
			}
			require.Empty(t, valid)
			failures := openpeerrors.CollectFailures(err)
			require.Len(t, failures, 2)
			require.Contains(t, failures[0].Message, "disagree on vxlanPort or underlayAddressFamily")
		})
	}
}

func TestFilterValidL2Trunks(t *testing.T) {
	managed := testTrunk("managed", &v1alpha1.HostMaster{
		Type: v1alpha1.LinuxBridge,
		LinuxBridge: &v1alpha1.LinuxBridgeConfig{
			Lifecycle: v1alpha1.BridgeLifecycleManaged,
		},
	})
	external := testTrunk("external", externalHostMaster("br-ex"))
	noL2VNIs := testTrunk("no-l2vnis", nil)

	valid, err := FilterValidL2Trunks([]v1alpha1.L2Trunk{managed, external, noL2VNIs})
	require.Equal(t, []v1alpha1.L2Trunk{external, noL2VNIs}, valid)

	failures := openpeerrors.CollectFailures(err)
	require.Len(t, failures, 1)
	require.Equal(t, openpeerrors.KindL2Trunk, failures[0].Kind)
	require.Equal(t, "managed", failures[0].Name)
	require.Contains(t, failures[0].Message, "requires an External host master")
}

func TestFilterOverlayResourcesKeepsValidResources(t *testing.T) {
	trunk := testTrunk("workloads", externalHostMaster("br-ex"))
	duplicateTag := []v1alpha1.L2VNI{
		testTrunkedL2VNI("first", "workloads", 100, 10100, nil, nil),
		testTrunkedL2VNI("second", "workloads", 100, 10200, nil, nil),
	}

	valid, err := FilterOverlayResources(OverlayResources{
		L2Trunks: []v1alpha1.L2Trunk{trunk},
		L2VNIs:   duplicateTag,
	})
	require.Equal(t, []v1alpha1.L2Trunk{trunk}, valid.L2Trunks)
	require.Empty(t, valid.L2VNIs)
	require.Len(t, openpeerrors.CollectFailures(err), 2)
}

func TestGroutDatapathAcceptsTrunklessConfig(t *testing.T) {
	err := (&GroutDatapathConfigValidator{}).Validate(APIConfigData{
		L2VNIs: []v1alpha1.L2VNI{{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "standalone"},
			Spec:       v1alpha1.L2VNISpec{VNI: 10100},
		}},
	})
	require.NoError(t, err)
}
