// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/frr"
)

var noopUpdater = frr.ConfigUpdater(func(_ context.Context, _ string) error {
	return nil
})

var failingUpdater = frr.ConfigUpdater(func(_ context.Context, _ string) error {
	return fmt.Errorf("frr-reload failed")
})

type noopDatapathConfigurator struct{}

func (n *noopDatapathConfigurator) Configure(_ context.Context, _ interfacesConfiguration) error {
	return nil
}

func (n *noopDatapathConfigurator) Validate(_ conversion.APIConfigData) error {
	return nil
}

func TestReconcilePerResourceErrors(t *testing.T) {
	tests := []struct {
		name             string
		l3VNIs           []v1alpha1.L3VNI
		l2VNIs           []v1alpha1.L2VNI
		l3Passthroughs   []v1alpha1.L3Passthrough
		expectedFailures []v1alpha1.FailedResource
	}{
		{
			name: "all valid L3VNIs pass through",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("vni-a", "vrfA", 100),
				l3VNI("vni-b", "vrfB", 200),
			},
		},
		{
			name: "duplicate vni within L3VNIs skips second",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("first", "vrfA", 100),
				l3VNI("second", "vrfB", 100),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VNI, Name: "second", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "duplicate vni 100:L3VNI/first"},
			},
		},
		{
			name: "invalid VRF name skips L3VNI",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("bad-name", "this-is-way-too-long-for-interface", 100),
				l3VNI("good", "vrfA", 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VNI, Name: "bad-name", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "invalid vrf name for vni \"bad-name\", vrf \"this-is-way-too-long-for-interface\": interface name this-is-way-too-long-for-interface can't be longer than 15 characters"},
			},
		},
		{
			name: "valid connected and disconnected L2VNIs",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("l3-a", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("connected", new("vrfA"), 200),
				l2VNI("disconnected", nil, 201),
			},
		},
		{
			name: "duplicate vni within L2VNIs skips second",
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("first", nil, 200),
				l2VNI("second", nil, 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "second", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "duplicate vni 200:L2VNI/first"},
			},
		},
		{
			name: "cross-type VNI conflict skips L2VNI",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3", "vrfA", 100),
				l3VNI("conflict-l3", "vrfB", 300),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", nil, 200),
				l2VNI("conflict-l2", nil, 300),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "conflict-l2", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "duplicate vni 300:L3VNI/conflict-l3"},
			},
		},
		{
			name: "more than one passthrough skips all",
			l3Passthroughs: []v1alpha1.L3Passthrough{
				{ObjectMeta: metav1.ObjectMeta{Name: "pt-a"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "pt-b"}},
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3Passthrough, Name: "pt-a", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "can't have more than one l3passthrough per node"},
				{Kind: openpeerrors.KindL3Passthrough, Name: "pt-b", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "can't have more than one l3passthrough per node"},
			},
		},
		{
			name: "duplicate VRF skips second L3VNI",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("first", "same-vrf", 100),
				l3VNI("second", "same-vrf", 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VNI, Name: "second", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `more than one L3VNI detected in VRF "same-vrf": "/first" already exists`},
			},
		},
		{
			name: "L2VNI with no valid L3VNI reports DependencyFailed",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", new("vrfA"), 200),
				l2VNI("orphan-l2", new("vrfMissing"), 300),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "orphan-l2", Reason: v1alpha1.FailedResourceReasonDependencyFailed,
					Message: `no valid L3VNI for L3 domain "vrfMissing"`},
			},
		},
		{
			name: "VRF subnet overlap reports all resources in failed VRF",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3", "vrfGood", 100),
				l3VNI("bad-l3", "vrfBad", 200),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNIWithGateway("good-l2", "vrfGood", 300, []string{"10.0.1.1/24"}),
				l2VNIWithGateway("bad-l2-1", "vrfBad", 400, []string{"10.0.2.1/24"}),
				l2VNIWithGateway("bad-l2-2", "vrfBad", 500, []string{"10.0.2.100/24"}),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VNI, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
				{Kind: openpeerrors.KindL2VNI, Name: "bad-l2-1", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
				{Kind: openpeerrors.KindL2VNI, Name: "bad-l2-2", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := conversion.APIConfigData{
				L3VNIs:        tt.l3VNIs,
				L2VNIs:        tt.l2VNIs,
				L3Passthrough: tt.l3Passthroughs,
			}
			reconcileErr := Reconcile(context.Background(), config, 0, "",
				"", "", noopUpdater, &noopDatapathConfigurator{})

			failures := openpeerrors.CollectFailures(reconcileErr)

			if !reflect.DeepEqual(failures, tt.expectedFailures) {
				t.Errorf("FailedResources mismatch:\n  got:  %+v\n  want: %+v", failures, tt.expectedFailures)
			}
		})
	}
}

func TestReconcileFrrReloadFailure(t *testing.T) {
	reconcileErr := Reconcile(context.Background(), conversion.APIConfigData{}, 0, "",
		"", "", failingUpdater, &noopDatapathConfigurator{})
	if reconcileErr == nil {
		t.Fatal("expected error from FRR reload failure")
	}

	if !strings.Contains(reconcileErr.Error(), "failed to update the frr configuration") {
		t.Errorf("expected FRR error message, got: %s", reconcileErr.Error())
	}

	failures := openpeerrors.CollectFailures(reconcileErr)
	if len(failures) != 0 {
		t.Errorf("expected no resource failures for FRR error, got: %+v", failures)
	}
}

func l3VNI(name, vrf string, vni int32) v1alpha1.L3VNI {
	return v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.L3VNISpec{VRF: vrf, VNI: vni},
	}
}

func l2VNI(name string, vrf *string, vni int32) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.L2VNISpec{VRF: vrf, VNI: vni},
	}
}

func l2VNIWithGateway(name, vrf string, vni int32, gatewayIPs []string) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.L2VNISpec{
			VRF:          new(vrf),
			VNI:          vni,
			L2GatewayIPs: gatewayIPs,
		},
	}
}
