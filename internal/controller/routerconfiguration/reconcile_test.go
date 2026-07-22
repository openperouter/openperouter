// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/google/go-cmp/cmp"
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
		underlays        []v1alpha1.Underlay
		l3VNIs           []v1alpha1.L3VNI
		l3VPNs           []v1alpha1.L3VPN
		l2VNIs           []v1alpha1.L2VNI
		l3Passthroughs   []v1alpha1.L3Passthrough
		expectedFailures []v1alpha1.FailedResource
		wantConfig       conversion.APIConfigData
	}{
		{
			name: "all valid L3VNIs pass through",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("vni-a", "vrfA", 100),
				l3VNI("vni-b", "vrfB", 200),
			},
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("vni-a", "vrfA", 100),
					l3VNI("vni-b", "vrfB", 200),
				},
			},
		},
		{
			name:      "all valid L3VPNs pass through",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("vni-a", "vrfA", 100),
				l3VPN("vni-b", "vrfB", 200),
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("vni-a", "vrfA", 100),
					l3VPN("vni-b", "vrfB", 200),
				},
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
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("first", "vrfA", 100),
				},
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
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("first", "vrfA", 100),
				},
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
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("good", "vrfA", 200),
				},
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
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("good", "vrfA", 200),
				},
			},
		},
		{
			name: "valid connected and disconnected L2VNIs",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("l3-a", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("connected", l3vniRoutingDomain("l3-a"), 200),
				l2VNI("disconnected", nil, 201),
			},
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("l3-a", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("connected", l3vniRoutingDomain("l3-a"), 200),
					l2VNI("disconnected", nil, 201),
				},
			},
		},
		{
			name:      "valid connected and disconnected L2VNIs (L3VPN)",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("l3-a", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("connected", l3vpnRoutingDomain("l3-a"), 200),
				l2VNI("disconnected", nil, 201),
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("l3-a", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("connected", l3vpnRoutingDomain("l3-a"), 200),
					l2VNI("disconnected", nil, 201),
				},
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
			wantConfig: conversion.APIConfigData{
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("first", nil, 200),
				},
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
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("conflict-l3", "vrfB", 300),
					l3VNI("good-l3", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", nil, 200),
				},
			},
		},
		{
			name:      "cross-type VNI conflict skips L2VNI (L3VPN)",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("conflict-l3", "vrfB", 300),
				l3VPN("good-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("conflict-l2", nil, 300),
				l2VNI("good-l2", nil, 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "conflict-l2", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: "duplicate vni 300:L3VPN/conflict-l3"},
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("conflict-l3", "vrfB", 300),
					l3VPN("good-l3", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", nil, 200),
				},
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
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("first", "same-vrf", 100),
				},
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
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("first", "same-vrf", 100),
				},
			},
		},
		{
			name: "L2VNI with no valid L3VNI reports DependencyFailed",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", l3vniRoutingDomain("good-l3"), 200),
				l2VNI("orphan-l2", l3vniRoutingDomain("l3vni-missing"), 300),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "orphan-l2", Reason: v1alpha1.FailedResourceReasonDependencyFailed,
					Message: `referenced L3VNI "l3vni-missing" not found`},
			},
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("good-l3", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", l3vniRoutingDomain("good-l3"), 200),
				},
			},
		},
		{
			name:      "L2VNI with no valid L3VNI or L3VPN reports DependencyFailed",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("good-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", l3vpnRoutingDomain("good-l3"), 200),
				l2VNI("orphan-l2", l3vpnRoutingDomain("vpn-missing"), 300),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL2VNI, Name: "orphan-l2", Reason: v1alpha1.FailedResourceReasonDependencyFailed,
					Message: `referenced L3VPN "vpn-missing" not found`},
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("good-l3", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", l3vpnRoutingDomain("good-l3"), 200),
				},
			},
		},
		{
			name:      "L2VNI with valid L3VPN reports no failures",
			underlays: srv6Underlays(),
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("good-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", l3vpnRoutingDomain("good-l3"), 200),
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("good-l3", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", l3vpnRoutingDomain("good-l3"), 200),
				},
			},
		},
		{
			name:      "L3VPN with L3VNI in same VRF reports failure but reconciles other resources (L2VNI ref: L3VNI)",
			underlays: srv6Underlays(),
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3-vni", "vrfA", 100),
			},
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("bad-l3-vpn", "vrfA", 101),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", l3vniRoutingDomain("good-l3-vni"), 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3-vpn", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `conflict with L3VNI detected in VRF "vrfA": "/good-l3-vni" already exists`},
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("good-l3-vni", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", l3vniRoutingDomain("good-l3-vni"), 200),
				},
			},
		},
		{
			name:      "L3VPN with L3VNI in same VRF reports failure but reconciles other resources (L2VNI ref: L3VPN)",
			underlays: srv6Underlays(),
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3-vni", "vrfA", 100),
			},
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("bad-l3-vpn", "vrfA", 101),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("bad-l2", l3vpnRoutingDomain("bad-l3-vpn"), 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3-vpn", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `conflict with L3VNI detected in VRF "vrfA": "/good-l3-vni" already exists`},
				{Kind: openpeerrors.KindL2VNI, Name: "bad-l2", Reason: v1alpha1.FailedResourceReasonDependencyFailed,
					Message: `referenced L3VPN "bad-l3-vpn" not found`},
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("good-l3-vni", "vrfA", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{},
			},
		},
		{
			name:      "L3VPN with L3VNI in different VRFs succeeds",
			underlays: srv6Underlays(),
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-vni", "vrfA", 100),
			},
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("good-vpn", "vrfB", 101),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", nil, 200),
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("good-vni", "vrfA", 100),
				},
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("good-vpn", "vrfB", 101),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", nil, 200),
				},
			},
		},
		{
			name: "L3VPN without underlay reports failure but reconciles other resources",
			l3VPNs: []v1alpha1.L3VPN{
				l3VPN("bad-l3", "vrfA", 100),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNI("good-l2", nil, 200),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `cannot specify L3VPN configuration without an underlay with SRV6 configuration`},
			},
			wantConfig: conversion.APIConfigData{
				L2VNIs: []v1alpha1.L2VNI{
					l2VNI("good-l2", nil, 200),
				},
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
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `cannot specify L3VPN configuration without an underlay with SRV6 configuration`},
			},
			wantConfig: conversion.APIConfigData{
				Underlays: []v1alpha1.Underlay{{
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
			},
		},
		{
			name: "VRF subnet overlap reports all resources in failed VRF",
			l3VNIs: []v1alpha1.L3VNI{
				l3VNI("good-l3", "vrfGood", 100),
				l3VNI("bad-l3", "vrfBad", 200),
			},
			l2VNIs: []v1alpha1.L2VNI{
				l2VNIWithGateway("good-l2", l3vniRoutingDomain("good-l3"), 300, []string{"10.0.1.1/24"}),
				l2VNIWithGateway("bad-l2-1", l3vniRoutingDomain("bad-l3"), 400, []string{"10.0.2.1/24"}),
				l2VNIWithGateway("bad-l2-2", l3vniRoutingDomain("bad-l3"), 500, []string{"10.0.2.100/24"}),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VNI, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
				{Kind: openpeerrors.KindL2VNI, Name: "bad-l2-1", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
				{Kind: openpeerrors.KindL2VNI, Name: "bad-l2-2", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
			},
			wantConfig: conversion.APIConfigData{
				L3VNIs: []v1alpha1.L3VNI{
					l3VNI("good-l3", "vrfGood", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNIWithGateway("good-l2", l3vniRoutingDomain("good-l3"), 300, []string{"10.0.1.1/24"}),
				},
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
				l2VNIWithGateway("good-l2", l3vpnRoutingDomain("good-l3"), 300, []string{"10.0.1.1/24"}),
				l2VNIWithGateway("bad-l2-1", l3vpnRoutingDomain("bad-l3"), 400, []string{"10.0.2.1/24"}),
				l2VNIWithGateway("bad-l2-2", l3vpnRoutingDomain("bad-l3"), 500, []string{"10.0.2.100/24"}),
			},
			expectedFailures: []v1alpha1.FailedResource{
				{Kind: openpeerrors.KindL3VPN, Name: "bad-l3", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
				{Kind: openpeerrors.KindL2VNI, Name: "bad-l2-1", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
				{Kind: openpeerrors.KindL2VNI, Name: "bad-l2-2", Reason: v1alpha1.FailedResourceReasonValidationFailed,
					Message: `subnet overlap in VRF "vrfBad": IPNet 10.0.2.0/24 (L2VNI /bad-l2-2) overlaps with IPNet 10.0.2.0/24 (L2VNI /bad-l2-1)`},
			},
			wantConfig: conversion.APIConfigData{
				Underlays: srv6Underlays(),
				L3VPNs: []v1alpha1.L3VPN{
					l3VPN("good-l3", "vrfGood", 100),
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2VNIWithGateway("good-l2", l3vpnRoutingDomain("good-l3"), 300, []string{"10.0.1.1/24"}),
				},
			},
		},
	}

	var gotConfig conversion.APIConfigData
	testConfigureFRR := func(_ context.Context, data frrConfigData) error { //nolint:unparam
		gotConfig = data.APIConfigData
		return nil
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

			gotConfig = conversion.APIConfigData{}
			reconcileErr := Reconcile(context.Background(), config, 0, "",
				"", "", noopUpdater, &noopDatapathConfigurator{}, testConfigureFRR)

			failures := openpeerrors.CollectFailures(reconcileErr)

			if !cmp.Equal(failures, tt.expectedFailures) {
				t.Errorf("FailedResources mismatch:\n  got:  %+v\n  want: %+v", failures, tt.expectedFailures)
			}

			if compareOutput := cmp.Diff(gotConfig, tt.wantConfig); compareOutput != "" {
				t.Errorf("Configuration mismatch after reconcile (-got, +want):\n%s", compareOutput)
			}
		})
	}
}

func TestReconcileFrrReloadFailure(t *testing.T) {
	reconcileErr := Reconcile(context.Background(), conversion.APIConfigData{}, 0, "",
		"", "", failingUpdater, &noopDatapathConfigurator{}, configureFRR)
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

func l3vniRoutingDomain(name string) *v1alpha1.RoutingDomain {
	return &v1alpha1.RoutingDomain{
		Type:  v1alpha1.RoutingDomainTypeL3VNI,
		L3VNI: &v1alpha1.L3VNIReference{Name: name},
	}
}

func l3vpnRoutingDomain(name string) *v1alpha1.RoutingDomain {
	return &v1alpha1.RoutingDomain{
		Type:  v1alpha1.RoutingDomainTypeL3VPN,
		L3VPN: &v1alpha1.L3VPNReference{Name: name},
	}
}

func l2VNI(name string, rd *v1alpha1.RoutingDomain, vni int32) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.L2VNISpec{
			VNI:           vni,
			RoutingDomain: rd,
		},
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

func l2VNIWithGateway(name string, rd *v1alpha1.RoutingDomain, vni int32, gatewayIPs []string) v1alpha1.L2VNI {
	l2 := l2VNI(name, rd, vni)
	l2.Spec.GatewayIPs = gatewayIPs
	return l2
}
