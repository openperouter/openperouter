// SPDX-License-Identifier:Apache-2.0

package main

import (
	"testing"

	"github.com/openperouter/openperouter/internal/controller/routerconfiguration"

	"github.com/openperouter/openperouter/api/static"
	"github.com/stretchr/testify/require"
)

func TestRouterMACPrefixParameters(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		datapath  string
		wantError bool
	}{
		{name: "default", datapath: datapathKernel},
		{name: "kernel prefix", prefix: "02:00:00:00:00:00/32", datapath: datapathKernel},
		{name: "invalid prefix", prefix: "invalid", datapath: datapathKernel, wantError: true},
		{name: "grout default", datapath: datapathGrout},
		{name: "grout prefix", prefix: "02:00:00:00:00:00/32", datapath: datapathGrout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := parameters{
				mode: modeK8s, namespace: "openperouter", nodeName: "node-1",
				routerMACPrefix: tt.prefix, datapath: tt.datapath,
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

func TestRouterMACPrefixNodeOverride(t *testing.T) {
	for _, configured := range []string{"", "02:00:00:01:00:00/32"} {
		t.Run(configured, func(t *testing.T) {
			args := parameters{routerMACPrefix: "02:00:00:00:00:00/32"}
			config := static.NodeConfig{NodeName: "node-1", RouterMACPrefix: configured}
			require.NoError(t, overrideHostMode(&args, config))
			want := "02:00:00:00:00:00/32"
			if configured != "" {
				want = configured
			}
			require.Equal(t, want, args.routerMACPrefix)
		})
	}
}

func TestNewDatapathConfigurator(t *testing.T) {
	for _, datapath := range []string{datapathKernel, datapathGrout} {
		for _, prefix := range []string{"", "02:00:00:00:00:00/32"} {
			t.Run(datapath+prefix, func(t *testing.T) {
				config := newDatapathConfigurator(parameters{
					datapath: datapath, routerMACPrefix: prefix,
				})
				switch config := config.(type) {
				case *routerconfiguration.KernelDatapathConfigurator:
					require.Equal(t, datapathKernel, datapath)
					require.Equal(t, prefix, config.RouterMACPrefix)
				case *routerconfiguration.GroutDatapathConfigurator:
					require.Equal(t, datapathGrout, datapath)
					require.Equal(t, prefix, config.RouterMACPrefix)
				default:
					t.Fatalf("unexpected configurator type %T", config)
				}
			})
		}
	}
}
