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

var noopHostConfigurator = HostConfigurator(func(_ context.Context, _ interfacesConfiguration) error {
	return nil
})

func TestReconcilePerResourceErrors(t *testing.T) {
	tests := []struct {
		name             string
		underlays        []v1alpha1.Underlay
		l3VNIs           []v1alpha1.L3VNI
		l3VPNs           []v1alpha1.L3VPN
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
			name:      "all valid L3VPNs pass through",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("vni-a", "vrfA", 100),
				l3VPN("vni-b", "vrfB", 200),
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
			name:      "duplicate rdAssignedNumber within L3VPNs skips second",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("first", "vrfA", 100),
				l3VPN("second", "vrfB", 100),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "second", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "duplicate rdAssignedNumber 100:L3VPN/first"},
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
			name:      "invalid VRF name skips L3VPN",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("bad-name", "this-is-way-too-long-for-interface", 100),
				l3VPN("good", "vrfA", 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "bad-name", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "invalid vrf name for vpn \"bad-name\", vrf \"this-is-way-too-long-for-interface\": interface name this-is-way-too-long-for-interface can't be longer than 15 characters"},
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
			name:      "valid connected and disconnected L2VNIs (L3VPN)",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("l3-a", "vrfA", 100),
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
			name:      "cross-type VNI conflict skips L2VNI (L3VPN)",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("good-l3", "vrfA", 100),
				l3VPN("conflict-l3", "vrfB", 300),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", nil, 200),
				l2VNI("conflict-l2", nil, 300),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "conflict-l2", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "duplicate vni 300:L3VPN/conflict-l3"},
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
			name:      "duplicate VRF skips second L3VPN",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("first", "same-vrf", 100),
				l3VPN("second", "same-vrf", 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "second", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `more than one L3VPN detected in VRF "same-vrf": "/first" already exists`},
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
					Message: `no valid L3 resource for VRF "vrfMissing"`},
			},
		},
		{
			name:      "L2VNI with no valid L3VNI or L3VPN reports DependencyFailed",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("good-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", new("vrfA"), 200),
				l2VNI("orphan-l2", new("vrfMissing"), 300),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "orphan-l2", Reason: v1alpha1.FailedResourceReasonDependencyFailed,
					Message: `no valid L3 resource for VRF "vrfMissing"`},
			},
		},
		{
			name:      "L2VNI with valid L3VPN reports no failures",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("good-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", new("vrfA"), 200),
			},
		},
		{
			name: "L3VPN without underlay reports failure",
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("bad-l3", "vrfA", 100),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `cannot specify L3VPN configuration without an underlay with SRV6 configuration`},
			},
		},
		{
			name: "L3VPN with underlay without SRV6 reports failure",
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("bad-l3", "vrfA", 100),
			},
			underlays: []v1alpha1.Underlay{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "underlay",
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN: 100,
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(101)),
							Address: new("192.168.1.1"),
						},
					},
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"2001:db8::/64"},
					},
				},
			}},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindUnderlay, Name: "underlay", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `cannot have an empty SRv6 section when L3VPNs are present`},
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `cannot specify L3VPN configuration without an underlay with SRV6 configuration`},
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
		{
			name:      "VRF subnet overlap reports all resources in failed VRF (L3VPN)",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("good-l3", "vrfGood", 100),
				l3VPN("bad-l3", "vrfBad", 200),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNIWithGateway("good-l2", "vrfGood", 300, []string{"10.0.1.1/24"}),
				l2VNIWithGateway("bad-l2-1", "vrfBad", 400, []string{"10.0.2.1/24"}),
				l2VNIWithGateway("bad-l2-2", "vrfBad", 500, []string{"10.0.2.100/24"}),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
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
				Underlays:     tt.underlays,
				L3VNIs:        tt.l3VNIs,
				L3VPNs:        tt.l3VPNs,
				L2VNIs:        tt.l2VNIs,
				L3Passthrough: tt.l3Passthroughs,
			}
			reconcileErr := Reconcile(context.Background(), config, 0, "",
				"", "", noopUpdater, noopHostConfigurator)

			failures := openpeerrors.CollectFailures(reconcileErr)

			if !reflect.DeepEqual(failures, tt.expectedFailures) {
				t.Errorf("FailedResources mismatch:\n  got:  %+v\n  want: %+v", failures, tt.expectedFailures)
			}
		})
	}
}

func TestReconcileFrrReloadFailure(t *testing.T) {
	reconcileErr := Reconcile(context.Background(), conversion.APIConfigData{}, 0, "",
		"", "", failingUpdater, noopHostConfigurator)
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

func l3VPN(name, vrf string, rdAssignedNumber int32) v1alpha1.L3VPN {
	return v1alpha1.L3VPN{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.L3VPNSpec{
			VRF:              vrf,
			RDAssignedNumber: rdAssignedNumber,
			ImportRTs:        []v1alpha1.RouteTarget{"100:100"},
		},
	}
}

func l2VNI(name string, vrf *string, vni int32) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       v1alpha1.L2VNISpec{VRF: vrf, VNI: vni},
	}
}

func srv6Underlays() []v1alpha1.Underlay {
	return []v1alpha1.Underlay{{
		Spec: v1alpha1.UnderlaySpec{
			ASN: 100,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(101)),
					Address: new("192.168.1.1"),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"2001:db8::/64"},
			},
			SRV6: &v1alpha1.SRV6Config{
				Locator: v1alpha1.SRV6Locator{
					BasePrefix: "ff00:0:01::/48",
					Format:     "usid-f3216",
				},
			},
		},
	}}
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
