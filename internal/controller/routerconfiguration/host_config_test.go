// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netns"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/hostnetwork/bridgerefresh"
)

func TestSetupVNIsWhenSharedStateFails(t *testing.T) {
	const missingNamespace = "/does-not-exist"
	l3VNI := l3VNIForTest("red", 100)
	l3VPN := l3VPNForTest("blue")
	l2VNI := l2VNIForTest("green", 300)
	hostConfig := conversion.HostConfigData{
		L3VNIs: []hostnetwork.L3VNIParams{l3VNI},
		L3VPNs: []hostnetwork.L3VPNParams{l3VPN},
		L2VNIs: []hostnetwork.L2VNIParams{l2VNI},
	}

	_, openErr := netns.GetFromPath(missingNamespace)
	require.Error(t, openErr, "the namespace path of the test must not exist")
	wantErr := fmt.Sprintf("transient host failure: failed to get network namespace %s: %v",
		missingNamespace, openErr)

	result, errs := setupVNIs(context.Background(), nil, missingNamespace, hostConfig)

	require.Equal(t, []hostnetwork.L3VNIParams{l3VNI}, result.l3VNIs)
	require.Equal(t, []hostnetwork.L3VPNParams{l3VPN}, result.l3VPNs)
	require.Equal(t, []hostnetwork.L2VNIParams{l2VNI}, result.l2VNIs)
	require.Equal(t, []error{
		wantResourceError(openpeerrors.KindL3VNI, "red", wantErr),
		wantResourceError(openpeerrors.KindL3VPN, "blue", wantErr),
		wantResourceError(openpeerrors.KindL2VNI, "green", wantErr),
	}, errs)
}

func TestSetupVNIsWithoutVNIs(t *testing.T) {
	// A reconcile without VNIs must not touch the host, so a missing router
	// namespace does not produce errors.
	result, errs := setupVNIs(context.Background(), nil, "/does-not-exist", conversion.HostConfigData{})

	require.Empty(t, result.l3VNIs)
	require.Empty(t, result.l3VPNs)
	require.Empty(t, result.l2VNIs)
	require.Empty(t, errs)
}

func TestSetupL2VNIs(t *testing.T) {
	red := l2VNIForTest("red", 100)
	blue := l2VNIForTest("blue", 200)
	errOVSDB := fmt.Errorf("%w: failed to connect to OVSDB", openpeerrors.ErrTransient)
	errMissingMaster := errors.New("could not find host master br-missing")

	tests := []struct {
		name            string
		l2VNIs          []hostnetwork.L2VNIParams
		failedL3Domains map[string]struct{}
		setupErrs       map[string]error
		wantSetUp       []hostnetwork.L2VNIParams
		wantKept        []hostnetwork.L2VNIParams
		wantErrs        []error
	}{
		{
			name: "no L2VNIs",
		},
		{
			name:      "all set up",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			wantSetUp: []hostnetwork.L2VNIParams{red, blue},
			wantKept:  []hostnetwork.L2VNIParams{red, blue},
		},
		{
			name:      "a transient failure keeps the L2VNI",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			setupErrs: map[string]error{"red": errOVSDB},
			wantSetUp: []hostnetwork.L2VNIParams{red, blue},
			wantKept:  []hostnetwork.L2VNIParams{red, blue},
			wantErrs:  []error{wantResourceError(openpeerrors.KindL2VNI, "red", errOVSDB.Error())},
		},
		{
			name:      "any other failure leaves the L2VNI out",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			setupErrs: map[string]error{"blue": errMissingMaster},
			wantSetUp: []hostnetwork.L2VNIParams{red, blue},
			wantKept:  []hostnetwork.L2VNIParams{red},
			wantErrs:  []error{wantResourceError(openpeerrors.KindL2VNI, "blue", errMissingMaster.Error())},
		},
		{
			name:      "both kinds of failure in one reconcile",
			l2VNIs:    []hostnetwork.L2VNIParams{red, blue},
			setupErrs: map[string]error{"red": errOVSDB, "blue": errMissingMaster},
			wantSetUp: []hostnetwork.L2VNIParams{red, blue},
			wantKept:  []hostnetwork.L2VNIParams{red},
			wantErrs: []error{
				wantResourceError(openpeerrors.KindL2VNI, "red", errOVSDB.Error()),
				wantResourceError(openpeerrors.KindL2VNI, "blue", errMissingMaster.Error()),
			},
		},
		{
			name:            "an L2VNI of a failed L3 domain is reported and left out",
			l2VNIs:          []hostnetwork.L2VNIParams{red, blue},
			failedL3Domains: map[string]struct{}{"red": {}},
			wantSetUp:       []hostnetwork.L2VNIParams{blue},
			wantKept:        []hostnetwork.L2VNIParams{blue},
			wantErrs:        []error{wantResourceError(openpeerrors.KindL2VNI, "red", `L3 domain "red" failed netlink provisioning`)},
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
			configurator := &fakeVNIConfigurator{setupErrs: tc.setupErrs}

			kept, errs := setupL2VNIs(ctx, configurator, tc.l2VNIs, tc.failedL3Domains)

			require.Equal(t, tc.wantKept, kept)
			require.Equal(t, tc.wantErrs, errs)
			require.Equal(t, tc.wantSetUp, configurator.l2VNIs,
				"only L2VNIs with a healthy routing domain reach the provisioner")
		})
	}
}

func TestSetupL3VNIs(t *testing.T) {
	red := l3VNIForTest("red", 100)
	blue := l3VNIForTest("blue", 200)
	errTransient := fmt.Errorf("%w: failed to set up VNI", openpeerrors.ErrTransient)
	errInvalidVRF := errors.New("invalid VRF name")

	tests := []struct {
		name        string
		l3VNIs      []hostnetwork.L3VNIParams
		setupErrs   map[string]error
		wantKept    []hostnetwork.L3VNIParams
		wantErrs    []error
		wantDomains map[string]struct{}
	}{
		{
			name:        "no L3VNIs",
			wantDomains: map[string]struct{}{},
		},
		{
			name:        "all set up",
			l3VNIs:      []hostnetwork.L3VNIParams{red, blue},
			wantKept:    []hostnetwork.L3VNIParams{red, blue},
			wantDomains: map[string]struct{}{},
		},
		{
			name:        "an error marked transient leaves the L3VNI out and marks its domain",
			l3VNIs:      []hostnetwork.L3VNIParams{red, blue},
			setupErrs:   map[string]error{"red": errTransient},
			wantKept:    []hostnetwork.L3VNIParams{blue},
			wantErrs:    []error{wantResourceError(openpeerrors.KindL3VNI, "red", errTransient.Error())},
			wantDomains: map[string]struct{}{"red": {}},
		},
		{
			name:        "any other failure leaves the L3VNI out and marks its domain",
			l3VNIs:      []hostnetwork.L3VNIParams{red, blue},
			setupErrs:   map[string]error{"blue": errInvalidVRF},
			wantKept:    []hostnetwork.L3VNIParams{red},
			wantErrs:    []error{wantResourceError(openpeerrors.KindL3VNI, "blue", errInvalidVRF.Error())},
			wantDomains: map[string]struct{}{"blue": {}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failedL3Domains := map[string]struct{}{}
			configurator := &fakeVNIConfigurator{setupErrs: tc.setupErrs}

			kept, errs := setupL3VNIs(context.Background(), configurator, tc.l3VNIs, failedL3Domains)

			require.Equal(t, tc.wantKept, kept)
			require.Equal(t, tc.wantErrs, errs)
			require.Equal(t, tc.wantDomains, failedL3Domains)
			require.Equal(t, tc.l3VNIs, configurator.l3VNIs,
				"every L3VNI must be set up through the configurator")
		})
	}
}

func TestSetupL3VPNs(t *testing.T) {
	red := l3VPNForTest("red")
	blue := l3VPNForTest("blue")
	errTransient := fmt.Errorf("%w: failed to set up host veth", openpeerrors.ErrTransient)
	errPermanent := errors.New("at least one IP address must be provided")

	tests := []struct {
		name        string
		l3VPNs      []hostnetwork.L3VPNParams
		setupErrs   map[string]error
		wantKept    []hostnetwork.L3VPNParams
		wantErrs    []error
		wantDomains map[string]struct{}
	}{
		{
			name:        "no L3VPNs",
			wantDomains: map[string]struct{}{},
		},
		{
			name:        "all set up",
			l3VPNs:      []hostnetwork.L3VPNParams{red, blue},
			wantKept:    []hostnetwork.L3VPNParams{red, blue},
			wantDomains: map[string]struct{}{},
		},
		{
			name:        "an error marked transient leaves the L3VPN out and marks its domain",
			l3VPNs:      []hostnetwork.L3VPNParams{red, blue},
			setupErrs:   map[string]error{"red": errTransient},
			wantKept:    []hostnetwork.L3VPNParams{blue},
			wantErrs:    []error{wantResourceError(openpeerrors.KindL3VPN, "red", errTransient.Error())},
			wantDomains: map[string]struct{}{"red": {}},
		},
		{
			name:        "any other failure leaves the L3VPN out and marks its domain",
			l3VPNs:      []hostnetwork.L3VPNParams{red, blue},
			setupErrs:   map[string]error{"blue": errPermanent},
			wantKept:    []hostnetwork.L3VPNParams{red},
			wantErrs:    []error{wantResourceError(openpeerrors.KindL3VPN, "blue", errPermanent.Error())},
			wantDomains: map[string]struct{}{"blue": {}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			failedL3Domains := map[string]struct{}{}
			configurator := &fakeVNIConfigurator{setupErrs: tc.setupErrs}

			kept, errs := setupL3VPNs(context.Background(), configurator, tc.l3VPNs, failedL3Domains)

			require.Equal(t, tc.wantKept, kept)
			require.Equal(t, tc.wantErrs, errs)
			require.Equal(t, tc.wantDomains, failedL3Domains)
			require.Equal(t, tc.l3VPNs, configurator.l3VPNs,
				"every L3VPN must be set up through the configurator")
		})
	}
}

func l2VNIForTest(name string, vni int32) hostnetwork.L2VNIParams {
	return hostnetwork.L2VNIParams{
		VNIParams: hostnetwork.VNIParams{VRF: name, VNI: vni},
		Name:      name,
	}
}

func l3VNIForTest(name string, vni int32) hostnetwork.L3VNIParams {
	return hostnetwork.L3VNIParams{
		VNIParams: hostnetwork.VNIParams{VRF: name, VNI: vni},
		Name:      name,
	}
}

func l3VPNForTest(name string) hostnetwork.L3VPNParams {
	return hostnetwork.L3VPNParams{Name: name, VRF: name}
}

func wantResourceError(kind v1alpha1.FailedResourceKind, name, message string) error {
	return &openpeerrors.ResourceError{
		Obj: v1alpha1.FailedResource{
			Kind:    kind,
			Name:    name,
			Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
			Message: message,
		},
	}
}

// fakeVNIConfigurator records the VNIs it is asked to set up and fails the
// ones the test configured to fail.
type fakeVNIConfigurator struct {
	setupErrs map[string]error
	l3VNIs    []hostnetwork.L3VNIParams
	l3VPNs    []hostnetwork.L3VPNParams
	l2VNIs    []hostnetwork.L2VNIParams
}

func (f *fakeVNIConfigurator) SetupL3VNI(_ context.Context, params hostnetwork.L3VNIParams) error {
	f.l3VNIs = append(f.l3VNIs, params)
	return f.setupErrs[params.Name]
}

func (f *fakeVNIConfigurator) SetupL3VPN(_ context.Context, params hostnetwork.L3VPNParams) error {
	f.l3VPNs = append(f.l3VPNs, params)
	return f.setupErrs[params.Name]
}

func (f *fakeVNIConfigurator) SetupL2VNI(_ context.Context, params hostnetwork.L2VNIParams) error {
	f.l2VNIs = append(f.l2VNIs, params)
	return f.setupErrs[params.Name]
}
