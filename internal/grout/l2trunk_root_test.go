// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package grout

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/stretchr/testify/require"
)

var errInjectedTrunkSetup = errors.New("injected setup failure")

type trunkSetupFailure struct {
	name             string
	failAt           string
	failAfterCommand bool
}

func TestSetupL2TrunkRetries(t *testing.T) {
	// The port name of the trunk under test, of PF pf0 and VF 1.
	portName := GroutPortNameForTrunk(hostnetwork.L2TrunkParams{
		SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "pf0", VFIndex: 1},
	})
	addPort := "interface add port " + portName
	tests := []trunkSetupFailure{
		{name: "already configured"},
		{name: "namespace lookup", failAt: "namespace"},
		{name: "driver probe after unbind", failAt: "probe"},
		{name: "port lookup", failAt: "interface show name " + portName},
		{name: "port creation", failAt: addPort},
		{name: "port creation response lost", failAt: addPort, failAfterCommand: true},
		{name: "port activation", failAt: "interface set port " + portName + " up"},
	}
	// A VF already bound to vfio-pci with no saved state is rejected by the
	// state retrieval, so only kernel drivers are exercised here.
	for _, driver := range []string{"iavf", pci.DriverMlx5Core} {
		for _, tt := range tests {
			t.Run(driver+"/"+tt.name, func(t *testing.T) {
				testL2TrunkSetupRetry(t, driver, tt)
			})
		}
	}
}

func testL2TrunkSetupRetry(t *testing.T, driver string, failure trunkSetupFailure) {
	t.Helper()
	if failure.failAt == "probe" && driver != "iavf" {
		t.Skip("only non-bifurcated kernel drivers need rebinding")
	}
	ns := newAccelTestNS(t)
	sysfs := newFakeSysfs(t, testPCIAddr, driver)
	sysfs.addDriver(pci.DriverVFIOPCI)
	sysfs.addVFLink("pf0", 1)
	addHostDummy(t, accelCIDR)
	sysfs.addNetDevice(accelIface)
	netdev, err := sysfs.NetDeviceForPCIAddress(testPCIAddr)
	require.NoError(t, err)
	wantState, err := retrieveDeviceState(netdev, PortModeAccelerated, sysfs.Sysfs)
	require.NoError(t, err)
	store := devicestate.Store{Dir: t.TempDir()}
	params := hostnetwork.L2TrunkParams{
		Name: "workloads", TargetNS: accelTestNSPath,
		SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "pf0", VFIndex: 1},
	}
	portName := GroutPortNameForTrunk(params)
	wantState.DeviceID = portName // the key the store stamps on save
	setupTrunkProbe(t, sysfs, failure.failAt == "probe")
	grout := recordTrunkPortSetup(t, portName, wantState, failure)
	if failure.failAt == "namespace" {
		params.TargetNS = filepath.Join(t.TempDir(), "missing-netns")
	}
	err = SetupL2Trunk(context.Background(), NewClient("sock"), params, store, sysfs.Sysfs)
	switch failure.failAt {
	case "":
		require.NoError(t, err)
	case "namespace":
		require.ErrorContains(t, err, "failed to find network namespace "+params.TargetNS)
	default:
		require.ErrorIs(t, err, errInjectedTrunkSetup)
	}
	saved, err := store.Load(portName)
	require.NoError(t, err)
	require.Equal(t, wantState, saved)

	params.TargetNS = accelTestNSPath
	for range 2 {
		require.NoError(t, SetupL2Trunk(context.Background(), NewClient("sock"), params, store, sysfs.Sysfs))
		saved, err := store.Load(portName)
		require.NoError(t, err)
		require.Equal(t, wantState, saved, "retries must preserve the original VF state")
	}
	require.Equal(t, 1, grout.creations)
	require.True(t, grout.up)
	if driver == pci.DriverMlx5Core {
		link := nsLinkByName(t, ns, accelIface)
		require.Empty(t, nsLinkAddresses(t, ns, link))
		require.Equal(t, "0", nsSysctl(t, ns, "net/ipv6/conf/"+accelIface+"/accept_ra"))
	}
}

func setupTrunkProbe(t *testing.T, sysfs *fakeSysfs, failProbe bool) {
	t.Helper()
	sysfs.Probe = func(pciAddr string) error {
		if err := os.Remove(sysfs.devicePath("driver")); !errors.Is(err, os.ErrNotExist) {
			require.NoError(t, err)
		}
		deleteHostDummy(t)
		if failProbe {
			failProbe = false
			return errInjectedTrunkSetup
		}
		return sysfs.probe(pciAddr)
	}
}

type trunkSetupRecorder struct {
	port      *groutInterfaceInfo
	creations int
	up        bool
}

func recordTrunkPortSetup(t *testing.T, portName string, state devicestate.State, failure trunkSetupFailure) *trunkSetupRecorder {
	t.Helper()
	original := execCmd
	t.Cleanup(func() { execCmd = original })
	got := &trunkSetupRecorder{}
	execCmd = func(_ context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "grcli", name)
		cmd := strings.Join(args[4:], " ")
		matchesFailure := cmd == failure.failAt || strings.HasPrefix(cmd, failure.failAt+" ")
		if matchesFailure && !failure.failAfterCommand {
			failure.failAt = ""
			return nil, errInjectedTrunkSetup
		}
		switch {
		case cmd == "interface show name "+portName:
			if got.port == nil {
				return []byte(interfaceNotFoundOutput), os.ErrNotExist
			}
			return json.Marshal(got.port)
		case strings.HasPrefix(cmd, "interface add port "+portName+" "):
			require.Nil(t, got.port, "a retry must not recreate the existing trunk port")
			got.creations++
			got.port = &groutInterfaceInfo{Name: portName, Type: "port", Devargs: state.PCIAddress, MTU: state.MTU}
		case cmd == "interface set port "+portName+" up":
			require.NotNil(t, got.port)
			got.up = true
		default:
			t.Fatalf("unexpected command: %s", cmd)
		}
		if matchesFailure && failure.failAfterCommand {
			failure.failAt = ""
			return nil, errInjectedTrunkSetup
		}
		return nil, nil
	}
	return got
}
