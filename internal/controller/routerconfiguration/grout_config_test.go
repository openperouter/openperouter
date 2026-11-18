// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"testing"

	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/grout"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/stretchr/testify/require"
)

func TestGroutSetupL2Trunks(t *testing.T) {
	tests := []struct {
		name    string
		trunks  []hostnetwork.SRIOVTrunkParams
		wantErr error
	}{
		{name: "no trunks"},
		{
			name: "out-of-range VF index has no configured port",
			trunks: []hostnetwork.SRIOVTrunkParams{{
				L2TrunkParams: hostnetwork.L2TrunkParams{Name: "workloads"},
				PFName:        "enp131s0f0d1",
				VFIndex:       256,
			}},
			wantErr: errors.Join(wantResourceError(openpeerrors.KindL2Trunk, "workloads",
				"vfIndex 256 must be between 0 and 255")),
		},
		{
			name: "reports every failed trunk",
			trunks: []hostnetwork.SRIOVTrunkParams{
				{L2TrunkParams: hostnetwork.L2TrunkParams{Name: "workloads"}, PFName: "enp131s0f0d1", VFIndex: 256},
				{L2TrunkParams: hostnetwork.L2TrunkParams{Name: "storage"}, PFName: "enp131s0f0d1", VFIndex: 257},
			},
			wantErr: errors.Join(
				wantResourceError(openpeerrors.KindL2Trunk, "workloads", "vfIndex 256 must be between 0 and 255"),
				wantResourceError(openpeerrors.KindL2Trunk, "storage", "vfIndex 257 must be between 0 and 255"),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configurator := &GroutDatapathConfigurator{}
			configured, err := configurator.setupL2Trunks(context.Background(), grout.NewClient("sock"), tt.trunks)
			require.Equal(t, tt.wantErr, err)
			require.Equal(t, grout.ConfiguredTrunks{}, configured)
		})
	}
}
