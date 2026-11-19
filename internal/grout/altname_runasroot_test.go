// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package grout

import (
	"context"
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
				want.AltNames = []string{altName}
			}
			assert.Equal(t, want, state)
		})
	}
}

func TestRestoreAltNames(t *testing.T) {
	const deviceName = "probe0"
	tests := []struct {
		name          string
		existing      []string
		saved         []string
		squatterNames []string
		want          []string
	}{
		{
			name:  "restores names the device no longer has",
			saved: []string{"pe-uplink0", "enp65s0f0npf0vf12-uplink"},
			want:  []string{"pe-uplink0", "enp65s0f0npf0vf12-uplink"},
		},
		{
			name:     "is idempotent when udev already restored them",
			existing: []string{"pe-uplink0"},
			saved:    []string{"pe-uplink0"},
			want:     []string{"pe-uplink0"},
		},
		{
			name:          "skips a name another device has taken",
			saved:         []string{"pe-uplink0", "squatter0", "pe-uplink1"},
			squatterNames: []string{"pe-uplink0"},
			want:          []string{"pe-uplink1"},
		},
		{
			name:     "does nothing when no names were saved",
			existing: []string{"pe-uplink0"},
			want:     []string{"pe-uplink0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer inProbeNetns(t)()
			link := addProbeVeth(t, deviceName)
			for _, name := range tt.existing {
				require.NoError(t, netlink.LinkAddAltName(link, name))
			}
			squatter := addProbeVeth(t, "squatter0")
			for _, name := range tt.squatterNames {
				require.NoError(t, netlink.LinkAddAltName(squatter, name))
			}
			link, err := netlink.LinkByName(deviceName)
			require.NoError(t, err)

			require.NoError(t, restoreAltNames(context.Background(), link, tt.saved))

			assert.ElementsMatch(t, tt.want, altNamesOf(t, deviceName))
			assert.ElementsMatch(t, tt.squatterNames, altNamesOf(t, "squatter0"))
		})
	}
}

func TestRestoreAcceleratedDeviceByAltName(t *testing.T) {
	defer inProbeNetns(t)()
	const (
		deviceName = "probe0"
		altName    = "pe-uplink0"
		cidr       = "192.0.2.1/24"
	)
	addProbeVeth(t, deviceName)
	sysfs := newFakeSysfs(t, testPCIAddr, "igb")
	sysfs.addNetDevice(deviceName)
	state := devicestate.State{
		InterfaceName:  altName,
		PCIAddress:     testPCIAddr,
		OriginalDriver: "igb",
		AltNames:       []string{altName},
		Addresses:      []string{cidr},
		AddrGenMode:    "1",
		AcceptRA:       "2",
		MTU:            9000,
	}

	// The netdev recreated by the original driver has no alternative names yet.
	_, err := netlink.LinkByName(altName)
	require.ErrorAs(t, err, &netlink.LinkNotFoundError{})
	require.NoError(t, restoreAcceleratedDevice(context.Background(), "", state, sysfs.Sysfs))

	got, err := retrieveDeviceState(altName, PortModeAccelerated, sysfs.Sysfs)
	require.NoError(t, err)
	assert.Equal(t, state, got)
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

func altNamesOf(t *testing.T, name string) []string {
	t.Helper()

	link, err := netlink.LinkByName(name)
	require.NoError(t, err)
	return link.Attrs().AltNames
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
