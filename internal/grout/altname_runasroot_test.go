// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package grout

import (
	"runtime"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func TestSuppressAutoconfByAltName(t *testing.T) {
	const (
		deviceName = "probe0"
		altName    = "pe-uplink0"
	)
	defer inProbeNetns(t)()
	link := addProbeVeth(t, deviceName)
	require.NoError(t, netlink.LinkAddAltName(link, altName))
	require.NoError(t, sysctl.Ensure(sysctl.AcceptRA(deviceName, "2")))

	require.NoError(t, hostnetwork.SuppressAutoconf(altName))

	acceptRA, err := sysctl.Read(sysctl.AcceptRA(deviceName, ""))
	require.NoError(t, err)
	assert.Equal(t, "0", acceptRA)
}

func TestRetrieveDeviceStateByAltName(t *testing.T) {
	for _, mode := range []UnderlayPortMode{PortModeTAP, PortModeAccelerated} {
		t.Run(string(mode), func(t *testing.T) {
			defer inProbeNetns(t)()
			const (
				deviceName = "probe0"
				altName    = "pe-uplink0"
				cidr       = "192.0.2.1/24"
			)
			link := addProbeVeth(t, deviceName)
			require.NoError(t, netlink.LinkAddAltName(link, altName))
			require.NoError(t, netlink.LinkSetMTU(link, 9000))
			require.NoError(t, hostnetwork.AssignIPToInterface(link, cidr))
			require.NoError(t, sysctl.Ensure(sysctl.AddrGenMode(deviceName, "1"), sysctl.AcceptRA(deviceName, "2")))
			sysfs := newFakeSysfs(t, testPCIAddr, "igb")
			sysfs.addNetDevice(deviceName)

			state, err := retrieveDeviceState(altName, mode, sysfs.Sysfs)
			require.NoError(t, err)
			want := devicestate.State{
				InterfaceName: altName,
				Addresses:     []string{cidr},
				AddrGenMode:   "1",
				AcceptRA:      "2",
				MTU:           9000,
			}
			if mode == PortModeAccelerated {
				want.PCIAddress = testPCIAddr
				want.OriginalDriver = "igb"
			}
			assert.Equal(t, want, state)
		})
	}
}

func addProbeVeth(t *testing.T, name string) netlink.Link {
	t.Helper()

	require.NoError(t, netlink.LinkAdd(&netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: name},
		PeerName:  name + "peer",
	}))
	link, err := netlink.LinkByName(name)
	require.NoError(t, err)
	return link
}

// inProbeNetns moves the calling goroutine into a fresh network namespace and
// returns the function that puts it back.
func inProbeNetns(t *testing.T) func() {
	t.Helper()

	runtime.LockOSThread()
	origNS, err := netns.Get()
	require.NoError(t, err)

	probeNS, err := netns.New()
	if err != nil {
		assert.NoError(t, origNS.Close())
		runtime.UnlockOSThread()
		t.Fatal(err)
	}

	return func() {
		assert.NoError(t, netns.Set(origNS))
		assert.NoError(t, probeNS.Close())
		assert.NoError(t, origNS.Close())
		runtime.UnlockOSThread()
	}
}
