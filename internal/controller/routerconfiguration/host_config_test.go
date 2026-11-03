// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netns"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
)

func TestSetupL2VNIs(t *testing.T) {
	red := l2VNIForTest("red", 100)
	blue := l2VNIForTest("blue", 200)
	const missingNamespace = "/does-not-exist"

	_, openErr := netns.GetFromPath(missingNamespace)
	require.Error(t, openErr, "the namespace path of the test must not exist")
	openFailure := fmt.Sprintf("failed to get network namespace %s: %v", missingNamespace, openErr)

	tests := []struct {
		name            string
		l2VNIs          []hostnetwork.L2VNIParams
		targetNamespace string
		wantErrs        []error
	}{
		{
			name:            "no L2VNIs does not open the namespace",
			targetNamespace: missingNamespace,
		},
		{
			name:            "a namespace open failure is reported for every L2VNI",
			l2VNIs:          []hostnetwork.L2VNIParams{red, blue},
			targetNamespace: missingNamespace,
			wantErrs: []error{
				wantL2VNIError("red", openFailure),
				wantL2VNIError("blue", openFailure),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kept, errs := setupL2VNIs(context.Background(), &hostnetwork.OVSClient{}, tc.l2VNIs, tc.targetNamespace)

			require.Nil(t, kept)
			require.Equal(t, tc.wantErrs, errs)
		})
	}
}

func TestSetupEachL2VNI(t *testing.T) {
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
			name: "no L2VNIs",
		},
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
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(func() {
				cancel()
				for _, vni := range tc.l2VNIs {
					require.NoError(t, bridgerefresh.StopForVNI(vni.VNI))
				}
			})
			provisioner := &fakeL2VNIProvisioner{setupErrs: tc.setupErrs}

			kept, errs := setupEachL2VNI(ctx, provisioner, tc.l2VNIs)

			require.Equal(t, tc.wantKept, kept)
			require.Equal(t, tc.wantErrs, errs)
			require.Equal(t, tc.l2VNIs, provisioner.setUp,
				"every L2VNI must be set up through the provisioner of the pass")
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

// fakeL2VNIProvisioner records the L2VNIs of a pass and fails the ones the
// test configured to fail.
type fakeL2VNIProvisioner struct {
	setupErrs map[string]error
	setUp     []hostnetwork.L2VNIParams
}

func (f *fakeL2VNIProvisioner) Setup(_ context.Context, params hostnetwork.L2VNIParams) error {
	f.setUp = append(f.setUp, params)
	return f.setupErrs[params.Name]
}
