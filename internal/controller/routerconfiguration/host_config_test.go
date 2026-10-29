// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
)

func TestSetupL2VNIs(t *testing.T) {
	red := l2VNIForTest("red", 100)
	blue := l2VNIForTest("blue", 200)
	errOVSDB := fmt.Errorf("%w: failed to connect to OVSDB", openpeerrors.ErrTransient)
	errMissingMaster := errors.New("could not find host master br-missing")

	tests := []struct {
		name      string
		l2VNIs    []hostnetwork.L2VNIParams
		setupErrs map[string]error
		wantKept  []hostnetwork.L2VNIParams
		wantErrs  []error
	}{
		{
			name:     "all set up",
			l2VNIs:   []hostnetwork.L2VNIParams{red, blue},
			wantKept: []hostnetwork.L2VNIParams{red, blue},
		},
		{
			name:      "a transient failure keeps the L2VNI",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			setupErrs: map[string]error{"red": errOVSDB},
			wantKept:  []hostnetwork.L2VNIParams{red, blue},
			wantErrs:  []error{wantL2VNIError("red", errOVSDB.Error())},
		},
		{
			name:      "any other failure leaves the L2VNI out",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			setupErrs: map[string]error{"blue": errMissingMaster},
			wantKept:  []hostnetwork.L2VNIParams{red},
			wantErrs:  []error{wantL2VNIError("blue", errMissingMaster.Error())},
		},
		{
			name:      "both kinds of failure in one pass",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			setupErrs: map[string]error{"red": errOVSDB, "blue": errMissingMaster},
			wantKept:  []hostnetwork.L2VNIParams{red},
			wantErrs: []error{
				wantL2VNIError("red", errOVSDB.Error()),
				wantL2VNIError("blue", errMissingMaster.Error()),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shared := &hostnetwork.VNISharedState{}
			var gotShared []*hostnetwork.VNISharedState
			originalSetup := setupSingleL2VNI
			t.Cleanup(func() { setupSingleL2VNI = originalSetup })
			setupSingleL2VNI = func(_ context.Context, _ *hostnetwork.OVSClient, vni hostnetwork.L2VNIParams, s *hostnetwork.VNISharedState) error {
				gotShared = append(gotShared, s)
				return tc.setupErrs[vni.Name]
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(func() {
				cancel()
				for _, vni := range tc.l2VNIs {
					require.NoError(t, bridgerefresh.StopForVNI(vni.VNI))
				}
			})

			kept, errs := setupL2VNIs(ctx, &hostnetwork.OVSClient{}, tc.l2VNIs, shared)

			require.Equal(t, tc.wantKept, kept)
			require.Equal(t, tc.wantErrs, errs)
			require.Len(t, gotShared, len(tc.l2VNIs))
			for _, s := range gotShared {
				require.Same(t, shared, s, "every L2VNI must reuse the state resolved for the pass")
			}
		})
	}
}

func TestResolveVNISharedState(t *testing.T) {
	stateErr := errors.New("failed to get network namespace /router")

	tests := []struct {
		name       string
		hostConfig conversion.HostConfigData
		stateErr   error
		wantState  bool
		wantErr    error
		wantCalls  int
	}{
		{
			name: "no VNIs",
		},
		{
			name: "state is resolved once for an L2 pass",
			hostConfig: conversion.HostConfigData{
				L2VNIs: []hostnetwork.L2VNIParams{l2VNIForTest("red", 100), l2VNIForTest("blue", 200)},
			},
			wantState: true,
			wantCalls: 1,
		},
		{
			name: "state is resolved once for an L3 pass",
			hostConfig: conversion.HostConfigData{
				L3VNIs: []hostnetwork.L3VNIParams{l3VNIForTest("red", true)},
			},
			wantState: true,
			wantCalls: 1,
		},
		{
			name: "a state failure is returned",
			hostConfig: conversion.HostConfigData{
				L2VNIs: []hostnetwork.L2VNIParams{l2VNIForTest("red", 100)},
			},
			stateErr:  stateErr,
			wantErr:   stateErr,
			wantCalls: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalSharedState := newVNISharedState
			t.Cleanup(func() { newVNISharedState = originalSharedState })
			calls := 0
			newVNISharedState = func(string, int) (*hostnetwork.VNISharedState, error) {
				calls++
				if tc.stateErr != nil {
					return nil, tc.stateErr
				}
				return &hostnetwork.VNISharedState{}, nil
			}

			state, err := resolveVNISharedState(tc.hostConfig, "/router", 9000)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.wantState {
				require.NotNil(t, state)
			} else {
				require.Nil(t, state)
			}
			require.Equal(t, tc.wantCalls, calls)
		})
	}
}

func TestFilterL2VNIsWithNoRoutingDomain(t *testing.T) {
	red := l2VNIForTest("red", 100)
	blue := l2VNIForTest("blue", 200)

	tests := []struct {
		name            string
		l2VNIs          []hostnetwork.L2VNIParams
		failedL3Domains sets.Set[string]
		wantHealthy     []hostnetwork.L2VNIParams
		wantErrs        []error
	}{
		{
			name:            "no failed L3 domain",
			l2VNIs:          []hostnetwork.L2VNIParams{red, blue},
			failedL3Domains: sets.New[string](),
			wantHealthy:     []hostnetwork.L2VNIParams{red, blue},
		},
		{
			name:            "an L2VNI of a failed L3 domain is reported and left out",
			l2VNIs:          []hostnetwork.L2VNIParams{red, blue},
			failedL3Domains: sets.New("red"),
			wantHealthy:     []hostnetwork.L2VNIParams{blue},
			wantErrs:        []error{wantL2VNIError("red", `L3 domain "red" failed netlink provisioning`)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			healthy, errs := filterL2VNIsWithNoRoutingDomain(tc.l2VNIs, tc.failedL3Domains)

			require.Equal(t, tc.wantHealthy, healthy)
			require.Equal(t, tc.wantErrs, errs)
		})
	}
}

func TestL2VNIStateErrors(t *testing.T) {
	red := l2VNIForTest("red", 100)
	blue := l2VNIForTest("blue", 200)
	mtuErr := errors.New("failed to list links")
	stateErr := errors.New("failed to get network namespace /router")

	tests := []struct {
		name     string
		l2VNIs   []hostnetwork.L2VNIParams
		mtuErr   error
		stateErr error
		wantErrs []error
	}{
		{
			name: "no L2VNIs",
		},
		{
			name:   "resolved state needs no errors",
			l2VNIs: []hostnetwork.L2VNIParams{red, blue},
		},
		{
			name:   "an MTU failure is reported for every L2VNI",
			l2VNIs: []hostnetwork.L2VNIParams{red, blue},
			mtuErr: mtuErr,
			wantErrs: []error{
				wantL2VNIError("red", "could not find underlay MTU: failed to list links"),
				wantL2VNIError("blue", "could not find underlay MTU: failed to list links"),
			},
		},
		{
			name:     "a state failure is reported for every L2VNI",
			l2VNIs:   []hostnetwork.L2VNIParams{red, blue},
			stateErr: stateErr,
			wantErrs: []error{
				wantL2VNIError("red", stateErr.Error()),
				wantL2VNIError("blue", stateErr.Error()),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantErrs, l2VNIStateErrors(tc.l2VNIs, tc.mtuErr, tc.stateErr))
		})
	}
}

func TestResolveUnderlayMTU(t *testing.T) {
	mtuErr := errors.New("failed to list links")

	tests := []struct {
		name       string
		hostConfig conversion.HostConfigData
		resolveErr error
		wantMTU    int
		wantErr    error
		wantCalls  int
	}{
		{
			name: "no VNIs",
		},
		{
			name: "L3 resources without a host veth need no MTU",
			hostConfig: conversion.HostConfigData{
				L3VNIs: []hostnetwork.L3VNIParams{l3VNIForTest("red", false)},
				L3VPNs: []hostnetwork.L3VPNParams{l3VPNForTest("blue", false)},
			},
		},
		{
			name: "resolved once for every L3VNI of the pass",
			hostConfig: conversion.HostConfigData{
				L3VNIs: []hostnetwork.L3VNIParams{l3VNIForTest("red", true), l3VNIForTest("blue", true)},
			},
			wantMTU:   9000,
			wantCalls: 1,
		},
		{
			name: "an L3VPN host veth triggers the lookup",
			hostConfig: conversion.HostConfigData{
				L3VPNs: []hostnetwork.L3VPNParams{l3VPNForTest("red", true)},
			},
			wantMTU:   9000,
			wantCalls: 1,
		},
		{
			name: "an L2VNI triggers the lookup",
			hostConfig: conversion.HostConfigData{
				L2VNIs: []hostnetwork.L2VNIParams{l2VNIForTest("red", 100)},
			},
			wantMTU:   9000,
			wantCalls: 1,
		},
		{
			name: "a resolution failure is returned",
			hostConfig: conversion.HostConfigData{
				L3VNIs: []hostnetwork.L3VNIParams{l3VNIForTest("red", true)},
			},
			resolveErr: mtuErr,
			wantErr:    mtuErr,
			wantCalls:  1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			originalFindUnderlayMTU := findUnderlayMTU
			t.Cleanup(func() { findUnderlayMTU = originalFindUnderlayMTU })
			calls := 0
			findUnderlayMTU = func(string) (int, error) {
				calls++
				if tc.resolveErr != nil {
					return 0, tc.resolveErr
				}
				return 9000, nil
			}

			mtu, err := resolveUnderlayMTU(tc.hostConfig, "/router")

			require.Equal(t, tc.wantMTU, mtu)
			require.Equal(t, tc.wantErr, err)
			require.Equal(t, tc.wantCalls, calls)
		})
	}
}

func l2VNIForTest(name string, vni int32) hostnetwork.L2VNIParams {
	return hostnetwork.L2VNIParams{
		VNIParams: hostnetwork.VNIParams{VRF: name, VNI: vni},
		Name:      name,
	}
}

func l3VNIForTest(name string, withHostVeth bool) hostnetwork.L3VNIParams {
	params := hostnetwork.L3VNIParams{
		VNIParams: hostnetwork.VNIParams{VRF: name},
		Name:      name,
	}
	if withHostVeth {
		params.LinkIPs = &hostnetwork.LinkIPs{HostIPv4: "192.168.9.1/32"}
	}
	return params
}

func l3VPNForTest(name string, withHostVeth bool) hostnetwork.L3VPNParams {
	params := hostnetwork.L3VPNParams{VRF: name}
	if withHostVeth {
		params.LinkIPs = &hostnetwork.LinkIPs{HostIPv4: "192.168.9.1/32"}
	}
	return params
}

func wantL2VNIError(name, message string) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind:    openpeerrors.KindL2VNI,
			Name:    name,
			Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
			Message: message,
		},
	}
}
