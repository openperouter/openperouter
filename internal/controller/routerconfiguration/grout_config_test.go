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

func TestSetupL2Trunks(t *testing.T) {
	const unsupported = "only SR-IOV L2Trunks are supported by the grout datapath"
	tests := []struct {
		name    string
		trunks  []hostnetwork.L2TrunkParams
		wantErr error
	}{
		{name: "no trunks"},
		{
			name:    "failed trunk has no configured port",
			trunks:  []hostnetwork.L2TrunkParams{{Name: "workloads"}},
			wantErr: errors.Join(wantResourceError(openpeerrors.KindL2Trunk, "workloads", unsupported)),
		},
		{
			name: "out-of-range VF index has no configured port",
			trunks: []hostnetwork.L2TrunkParams{{
				Name:  "workloads",
				SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "enp131s0f0d1", VFIndex: 256},
			}},
			wantErr: errors.Join(wantResourceError(openpeerrors.KindL2Trunk, "workloads",
				"vfIndex 256 must be between 0 and 255")),
		},
		{
			name:   "reports every failed trunk",
			trunks: []hostnetwork.L2TrunkParams{{Name: "workloads"}, {Name: "storage"}},
			wantErr: errors.Join(
				wantResourceError(openpeerrors.KindL2Trunk, "workloads", unsupported),
				wantResourceError(openpeerrors.KindL2Trunk, "storage", unsupported),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configurator := &GroutDatapathConfigurator{}
			ports, err := configurator.setupL2Trunks(context.Background(), grout.NewClient("sock"), tt.trunks)
			require.Equal(t, tt.wantErr, err)
			require.Equal(t, map[string]string{}, ports)
		})
	}
}
