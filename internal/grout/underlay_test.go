// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnderlayInterfaces(t *testing.T) {
	store := devicestate.Store{Dir: t.TempDir()}
	require.NoError(t, store.Save(devicestate.State{InterfaceName: "eth0"}))
	require.NoError(t, store.Save(devicestate.State{InterfaceName: "ens1f0", PCIAddress: testPCIAddr}))

	got, err := UnderlayInterfaces(store)
	require.NoError(t, err)
	assert.ElementsMatch(t, []hostnetwork.UnderlayInterface{
		{InterfaceName: "eth0", Kind: hostnetwork.UnderlayInterfaceNetDev},
		{InterfaceName: "ens1f0", Kind: hostnetwork.UnderlayInterfaceNetDev},
	}, got)
}

func TestUnderlayPortOptions(t *testing.T) {
	queues, size := int32(4), int32(1024)
	iface := hostnetwork.UnderlayInterface{InterfaceName: "eth0", AcceleratedConfig: &hostnetwork.AcceleratedConfigParams{
		RXQueues: &queues, QSize: &size,
	}}
	assert.Equal(t, portOptions{RXQueues: &queues, QSize: &size},
		underlayPortOptions(iface))
	assert.Equal(t, portOptions{},
		underlayPortOptions(hostnetwork.UnderlayInterface{InterfaceName: "eth0"}))
}

func TestTapBackingNetdevName(t *testing.T) {
	tests := []struct {
		name              string
		underlayInterface string
		want              string
	}{
		{
			name:              "short interface name",
			underlayInterface: "eth0",
			want:              "gu9c32a8c7",
		},
		{
			name:              "interface name at the kernel length limit",
			underlayInterface: "a23456789012345",
			want:              "gue1058467",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tapBackingNetdevName(tt.underlayInterface))
		})
	}
}
