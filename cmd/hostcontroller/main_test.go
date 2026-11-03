// SPDX-License-Identifier:Apache-2.0

package main

import (
	"testing"

	"github.com/openperouter/openperouter/api/static"
	"github.com/stretchr/testify/require"
)

func TestRouterMACParameters(t *testing.T) {
	tests := []struct {
		name      string
		mac       string
		datapath  string
		wantError bool
	}{
		{name: "default", datapath: datapathKernel},
		{name: "kernel MAC", mac: "02:00:00:00:00:01", datapath: datapathKernel},
		{name: "invalid MAC", mac: "invalid", datapath: datapathKernel, wantError: true},
		{name: "grout default", datapath: datapathGrout},
		{name: "grout MAC unsupported", mac: "02:00:00:00:00:01", datapath: datapathGrout, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := parameters{
				mode: modeK8s, namespace: "openperouter", nodeName: "node-1",
				routerMAC: tt.mac, datapath: tt.datapath,
			}
			err := validateParameters(args, hostModeParameters{})
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRouterMACNodeOverride(t *testing.T) {
	for _, configured := range []string{"", "02:00:00:00:00:02"} {
		t.Run(configured, func(t *testing.T) {
			args := parameters{routerMAC: "02:00:00:00:00:01"}
			config := static.NodeConfig{NodeName: "node-1", RouterMAC: configured}
			require.NoError(t, overrideHostMode(&args, config))
			want := "02:00:00:00:00:01"
			if configured != "" {
				want = configured
			}
			require.Equal(t, want, args.routerMAC)
		})
	}
}
