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
			shared := &hostnetwork.L2VNISharedState{}
			var gotShared []*hostnetwork.L2VNISharedState
			originalSetup := setupSingleL2VNI
			t.Cleanup(func() { setupSingleL2VNI = originalSetup })
			setupSingleL2VNI = func(_ context.Context, _ *hostnetwork.OVSClient, vni hostnetwork.L2VNIParams, s *hostnetwork.L2VNISharedState) error {
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

func TestResolveL2VNISharedState(t *testing.T) {
	red := l2VNIForTest("red", 100)
	blue := l2VNIForTest("blue", 200)
	stateErr := errors.New("could not find underlay MTU")

	tests := []struct {
		name      string
		l2VNIs    []hostnetwork.L2VNIParams
		stateErr  error
		wantState bool
		wantErrs  []error
	}{
		{
			name: "no L2VNIs",
		},
		{
			name:      "state is resolved once for the whole pass",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			wantState: true,
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
			originalSharedState := newL2VNISharedState
			t.Cleanup(func() { newL2VNISharedState = originalSharedState })
			calls := 0
			newL2VNISharedState = func(string) (*hostnetwork.L2VNISharedState, error) {
				calls++
				if tc.stateErr != nil {
					return nil, tc.stateErr
				}
				return &hostnetwork.L2VNISharedState{}, nil
			}

			state, errs := resolveL2VNISharedState(tc.l2VNIs, "/router")

			require.Equal(t, tc.wantErrs, errs)
			if tc.wantState {
				require.NotNil(t, state)
			} else {
				require.Nil(t, state)
			}
			if len(tc.l2VNIs) == 0 {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
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

func l2VNIForTest(name string, vni int32) hostnetwork.L2VNIParams {
	return hostnetwork.L2VNIParams{
		VNIParams: hostnetwork.VNIParams{VRF: name, VNI: vni},
		Name:      name,
	}
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
