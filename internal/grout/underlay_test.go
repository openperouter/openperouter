// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

func TestSetupTunnelEndpointRequiresAddress(t *testing.T) {
	err := setupTunnelEndpoint(context.Background(), NewClient("sock"), hostnetwork.UnderlayTunnelEndpointParams{})
	require.ErrorContains(t, err, "at least one tunnel endpoint IP must be provided")
}

func TestRetrieveDeviceStateRejectsMissingTAPInterface(t *testing.T) {
	t.Parallel()

	state, err := retrieveDeviceState("missing0", PortModeTAP, pci.Sysfs{})
	require.ErrorAs(t, err, &netlink.LinkNotFoundError{})
	require.ErrorContains(t, err, "failed to find interface missing0")
	assert.Equal(t, devicestate.State{}, state)
}

func TestRetrieveDeviceStateRejectsMissingAcceleratedInterface(t *testing.T) {
	t.Parallel()

	state, err := retrieveDeviceState("missing0", PortModeAccelerated, pci.Sysfs{})
	require.ErrorAs(t, err, &netlink.LinkNotFoundError{})
	require.ErrorContains(t, err, "failed to find interface missing0")
	assert.Equal(t, devicestate.State{}, state)
}

func TestUnderlayInterfaces(t *testing.T) {
	store := devicestate.Store{Dir: t.TempDir()}
	require.NoError(t, store.Save("eth0", devicestate.State{InterfaceName: "eth0"}))
	require.NoError(t, store.Save("ens1f0", devicestate.State{InterfaceName: "ens1f0", PCIAddress: testPCIAddr}))

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
