// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package grout

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/stretchr/testify/require"
)

func TestSetupL2Trunk(t *testing.T) {
	newAccelTestNS(t)
	sysfs := newFakeSysfs(t, testPCIAddr, "iavf")
	sysfs.addDriver(pci.DriverVFIOPCI)
	sysfs.addVFLink("pf0", 1)
	addHostDummy(t, accelCIDR)
	sysfs.addNetDevice(accelIface)
	sysfs.Probe = func(pciAddr string) error {
		if err := os.Remove(sysfs.devicePath("driver")); !errors.Is(err, os.ErrNotExist) {
			require.NoError(t, err)
		}
		deleteHostDummy(t)
		return sysfs.probe(pciAddr)
	}
	wantState, err := retrieveDeviceState(accelIface, PortModeAccelerated, sysfs.Sysfs)
	require.NoError(t, err)
	commands := recordGroutCommands(t)

	params := hostnetwork.L2TrunkParams{
		Name:     "workloads",
		TargetNS: accelTestNSPath,
		SRIOV:    &hostnetwork.SRIOVTrunkParams{PFName: "pf0", VFIndex: 1},
	}
	store := devicestate.Store{Dir: t.TempDir()}
	require.NoError(t, SetupL2Trunk(context.Background(), NewClient("sock"), params, store, sysfs.Sysfs))

	portName := GroutPortNameForTrunk(params)
	wantState.DeviceID = portName
	saved, err := store.Load(portName)
	require.NoError(t, err)
	require.Equal(t, wantState, saved)
	require.Equal(t, []string{
		"interface show name " + portName,
		"interface add port " + portName + " devargs " + testPCIAddr + " mtu " + strconv.Itoa(wantState.MTU) + " down",
		"interface set port " + portName + " up",
	}, *commands)
}

// recordGroutCommands replaces grcli with a fake that records the commands
// and reports every interface as missing.
func recordGroutCommands(t *testing.T) *[]string {
	t.Helper()
	original := execCmd
	t.Cleanup(func() { execCmd = original })
	commands := &[]string{}
	execCmd = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		cmd := strings.Join(args[4:], " ")
		*commands = append(*commands, cmd)
		if strings.HasPrefix(cmd, "interface show ") {
			return []byte(interfaceNotFoundOutput), os.ErrNotExist
		}
		return nil, nil
	}
	return commands
}
