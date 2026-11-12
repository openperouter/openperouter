// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/stretchr/testify/require"
)

const grcliPrefix = "grcli --err-exit --json --socket sock "

func TestGroutPortNameForTrunk(t *testing.T) {
	tests := []struct {
		name  string
		sriov *hostnetwork.SRIOVTrunkParams
		want  string
	}{
		{"VF index 0", &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 0}, "enp1s000"},
		{"VF index past one hex digit", &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 16}, "enp1s010"},
		{"highest VF index", &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 255}, "enp1s0ff"},
		{"PF name at the suffix limit", &hostnetwork.SRIOVTrunkParams{PFName: "enp131s0f0", VFIndex: 1}, "enp131s0f001"},
		{"PF name truncated", &hostnetwork.SRIOVTrunkParams{PFName: "enp131s0f0d1npx", VFIndex: 1}, "1s0f0d1npx01"},
		{"no SR-IOV parameters", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GroutPortNameForTrunk(hostnetwork.L2TrunkParams{Name: "workloads", SRIOV: tt.sriov})
			require.Equal(t, tt.want, got)
			require.Less(t, len(got), 16, "grout interface names must fit in IFNAMSIZ")
		})
	}
}

func TestTrunkVLANName(t *testing.T) {
	tests := []struct {
		name     string
		portName string
		tag      int32
		want     string
	}{
		{
			name:     "VLAN of a trunk VF",
			portName: "enp1s001",
			tag:      33,
			want:     "enp1s001021",
		},
		{
			name:     "full ten-character suffix with the highest VF index and tag",
			portName: "p131s0f0d1ff",
			tag:      4094,
			want:     "p131s0f0d1ffffe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trunkVLANPortName(tt.portName, tt.tag)
			require.Equal(t, tt.want, got)
			require.Less(t, len(got), 16, "grout interface names must fit in IFNAMSIZ")
		})
	}
}

func TestSetupL2TrunkRejectsInvalidParams(t *testing.T) {
	tests := []struct {
		name    string
		sriov   *hostnetwork.SRIOVTrunkParams
		wantErr string
	}{
		{
			name:    "missing SR-IOV parameters",
			wantErr: errUnsupportedTrunk.Error(),
		},
		{
			name:    "negative VF index",
			sriov:   &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: -1},
			wantErr: "vfIndex -1 must be between 0 and 255",
		},
		{
			name:    "VF index exceeds two hexadecimal digits",
			sriov:   &hostnetwork.SRIOVTrunkParams{PFName: "enp131s0f0d1", VFIndex: 256},
			wantErr: "vfIndex 256 must be between 0 and 255",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := hostnetwork.L2TrunkParams{Name: "workloads", SRIOV: tt.sriov}
			err := SetupL2Trunk(context.Background(), nil, params, devicestate.Store{}, pci.Sysfs{})
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestDeleteBridgeAttachedPorts(t *testing.T) {
	const interfaces = `[
		{"name": "br-pe-100", "type": "bridge"},
		{"name": "enp1s001021", "type": "vlan", "domain": "br-pe-100", "info": "parent=enp1s001 vlan_id=33"},
		{"name": "enp1s00202c", "type": "vlan", "domain": "br-pe-100", "info": "parent=enp1s002 vlan_id=44"},
		{"name": "vni100", "type": "vxlan", "domain": "br-pe-100"},
		{"name": "enp1s001037", "type": "vlan", "domain": "br-pe-200", "info": "parent=enp1s001 vlan_id=55"}
	]`
	tests := []struct {
		name       string
		bridgeName string
		want       []string
	}{
		{
			name:       "deletes every interface attached to the bridge",
			bridgeName: "br-pe-100",
			want: []string{
				grcliPrefix + "interface show",
				grcliPrefix + "interface show name enp1s001021",
				grcliPrefix + "interface del enp1s001021",
				grcliPrefix + "interface show name enp1s00202c",
				grcliPrefix + "interface del enp1s00202c",
				grcliPrefix + "interface show name vni100",
				grcliPrefix + "interface del vni100",
			},
		},
		{
			name:       "keeps interfaces attached to other bridges",
			bridgeName: "br-pe-200",
			want: []string{
				grcliPrefix + "interface show",
				grcliPrefix + "interface show name enp1s001037",
				grcliPrefix + "interface del enp1s001037",
			},
		},
		{
			name:       "no interfaces attached to the bridge",
			bridgeName: "br-pe-300",
			want:       []string{grcliPrefix + "interface show"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, map[string]string{
				grcliPrefix + "interface show":                  interfaces,
				grcliPrefix + "interface show name enp1s001021": vlanJSON("enp1s001021"),
				grcliPrefix + "interface show name enp1s00202c": vlanJSON("enp1s00202c"),
				grcliPrefix + "interface show name enp1s001037": vlanJSON("enp1s001037"),
				grcliPrefix + "interface show name vni100":      `{"name": "vni100", "type": "vxlan"}`,
			})
			err := deleteBridgeAttachedPorts(context.Background(), NewClient("sock"), tt.bridgeName)
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func TestEnsureVLAN(t *testing.T) {
	const showVLAN = grcliPrefix + "interface show name enp1s001021"

	tests := []struct {
		name     string
		existing string
		want     []string
	}{
		{
			name: "creates a missing VLAN",
			want: []string{
				showVLAN,
				grcliPrefix + "interface add vlan enp1s001021 parent enp1s001 vlan_id 33",
			},
		},
		{
			name: "keeps a matching VLAN",
			existing: `{"name": "enp1s001021", "type": "vlan", "mode": "bridge", "domain": "br-pe-100",
				"vlan_parent": "enp1s001", "vlan_id": 33}`,
			want: []string{showVLAN},
		},
		{
			name: "keeps a matching unattached VLAN",
			existing: `{"name": "enp1s001021", "type": "vlan", "mode": "VRF", "vrf": "main",
				"vlan_parent": "enp1s001", "vlan_id": 33}`,
			want: []string{showVLAN},
		},
		{
			name: "keeps a matching VLAN on another bridge",
			existing: `{"name": "enp1s001021", "type": "vlan", "domain": "br-pe-200",
				"vlan_parent": "enp1s001", "vlan_id": 33}`,
			want: []string{showVLAN},
		},
		{
			name: "recreates a VLAN with a different tag",
			existing: `{"name": "enp1s001021", "type": "vlan", "mode": "bridge", "domain": "br-pe-100",
				"vlan_parent": "enp1s001", "vlan_id": 44}`,
			want: []string{
				showVLAN,
				showVLAN,
				grcliPrefix + "interface del enp1s001021",
				grcliPrefix + "interface add vlan enp1s001021 parent enp1s001 vlan_id 33",
			},
		},
		{
			name: "recreates a VLAN with a different parent",
			existing: `{"name": "enp1s001021", "type": "vlan",
				"vlan_parent": "enp1s002", "vlan_id": 33}`,
			want: []string{
				showVLAN,
				showVLAN,
				grcliPrefix + "interface del enp1s001021",
				grcliPrefix + "interface add vlan enp1s001021 parent enp1s001 vlan_id 33",
			},
		},
		{
			name:     "recreates an interface with a different type",
			existing: `{"name": "enp1s001021", "type": "port"}`,
			want: []string{
				showVLAN,
				showVLAN,
				grcliPrefix + "interface del enp1s001021",
				grcliPrefix + "interface add vlan enp1s001021 parent enp1s001 vlan_id 33",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, map[string]string{showVLAN: tt.existing})

			err := NewClient("sock").ensureVLAN(context.Background(), "enp1s001021", "enp1s001", 33)
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func TestRemoveNonConfiguredL2Trunks(t *testing.T) {
	const interfaces = `[
		{"name": "u_eth0", "type": "port", "domain": "main", "info": "devargs=0000:01:00.0 mac=02:00:00:00:00:01"},
		{"name": "enp1s001", "type": "port", "domain": "main", "info": "devargs=0000:03:00.1 mac=02:00:00:00:00:02"},
		{"name": "enp1s002", "type": "port", "domain": "main", "info": "devargs=0000:03:00.2 mac=02:00:00:00:00:03"},
		{"name": "eno207", "type": "port", "domain": "main", "info": "devargs=0000:03:00.3 mac=02:00:00:00:00:04"},
		{"name": "pe-100", "type": "port", "domain": "br-pe-100", "info": "devargs=net_tap0,iface=host-100 mac=02:00:00:00:00:05"},
		{"name": "enp1s001021", "type": "vlan", "domain": "br-pe-100", "info": "parent=enp1s001 vlan_id=33"},
		{"name": "enp1s00202c", "type": "vlan", "domain": "br-pe-200", "info": "parent=enp1s002 vlan_id=44"}
	]`

	// The states record no original driver, so the teardown has nothing to
	// restore on the host and the test stays on the grout-side removal.
	kept := devicestate.State{
		DeviceID:   "enp1s001",
		PCIAddress: "0000:03:00.1",
	}
	removed := devicestate.State{
		DeviceID:   "enp1s002",
		PCIAddress: "0000:03:00.2",
	}
	store := devicestate.Store{Dir: t.TempDir()}
	for _, state := range []devicestate.State{kept, removed} {
		require.NoError(t, store.Save(state.DeviceID, state))
	}

	got := recordGrcli(t, map[string]string{
		grcliPrefix + "interface show": interfaces,
		grcliPrefix + "interface show name enp1s001021": `{"name":"enp1s001021","type":"vlan",
			"vlan_parent":"enp1s001","vlan_id":33}`,
		grcliPrefix + "interface show name enp1s00202c": `{"name":"enp1s00202c","type":"vlan",
			"vlan_parent":"enp1s002","vlan_id":44}`,
		grcliPrefix + "interface show name enp1s002": portJSON("enp1s002"),
		grcliPrefix + "interface show name eno207":   portJSON("eno207"),
	})

	desired := []hostnetwork.L2TrunkParams{
		{Name: "workloads", SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 1}},
	}
	err := RemoveNonConfiguredL2Trunks(context.Background(), NewClient("sock"), "", desired, store,
		pci.Sysfs{Root: t.TempDir()})
	require.NoError(t, err)

	// Grout refuses to delete a port with sub-interfaces. Ports without a
	// saved trunk state are not owned by trunk cleanup.
	want := []string{
		grcliPrefix + "interface show",
		grcliPrefix + "interface show name enp1s001021",
		grcliPrefix + "interface show name enp1s00202c",
		grcliPrefix + "interface show name enp1s00202c",
		grcliPrefix + "interface del enp1s00202c",
		grcliPrefix + "interface show name enp1s002",
		grcliPrefix + "interface del enp1s002",
	}
	require.Equal(t, want, *got)

	states, err := store.List()
	require.NoError(t, err)
	require.Equal(t, []devicestate.State{kept}, states)
}

func TestEnsureTrunkDeviceState(t *testing.T) {
	const pciAddr = "0000:03:00.2"
	const portName = "enp1s002"
	kernel := devicestate.State{
		DeviceID:       portName,
		InterfaceName:  "enp1s0v2",
		PCIAddress:     pciAddr,
		OriginalDriver: "iavf",
		Addresses:      []string{"192.0.2.2/24"},
		AddrGenMode:    "1",
		AcceptRA:       "2",
		MTU:            9000,
	}
	bifurcated := kernel
	bifurcated.OriginalDriver = pci.DriverMlx5Core

	sriov := &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 2}

	tests := []struct {
		name    string
		saved   *devicestate.State
		driver  string
		want    devicestate.State
		wantErr string
	}{
		{
			name:   "reuses the state after the kernel netdev disappears",
			saved:  &kernel,
			driver: pci.DriverVFIOPCI,
			want:   kernel,
		},
		{
			name:   "reuses a bifurcated VF state after its netdev moves",
			saved:  &bifurcated,
			driver: pci.DriverMlx5Core,
			want:   bifurcated,
		},
		{
			name:    "rejects a VF bound to vfio-pci with no saved state",
			driver:  pci.DriverVFIOPCI,
			wantErr: "no kernel net device found for PCI device " + pciAddr,
		},
	}
	unrelated := devicestate.State{
		DeviceID:       "enp1s001",
		InterfaceName:  "enp1s0v1",
		PCIAddress:     "0000:03:00.1",
		OriginalDriver: "iavf",
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := devicestate.Store{Dir: t.TempDir()}
			require.NoError(t, store.Save(unrelated.DeviceID, unrelated))
			if tt.saved != nil {
				require.NoError(t, store.Save(portName, *tt.saved))
			}
			sysfs := newFakeSysfs(t, pciAddr, tt.driver)
			sysfs.addVFLink(sriov.PFName, int(sriov.VFIndex))
			if tt.wantErr != "" {
				_, err := ensureTrunkDeviceState(portName, sriov, store, sysfs.Sysfs)
				require.ErrorContains(t, err, tt.wantErr)
				states, err := store.List()
				require.NoError(t, err)
				require.Equal(t, []devicestate.State{unrelated}, states)
				return
			}
			// The state is captured once and reused on the next pass.
			for range 2 {
				got, err := ensureTrunkDeviceState(portName, sriov, store, sysfs.Sysfs)
				require.NoError(t, err)
				require.Equal(t, tt.want, got)

				states, err := store.List()
				require.NoError(t, err)
				require.ElementsMatch(t, []devicestate.State{unrelated, tt.want}, states)
			}
		})
	}
}

func TestConfigureTrunkPort(t *testing.T) {
	const portName = "enp1s002"
	const showPort = grcliPrefix + "interface show name " + portName
	const setPortUp = grcliPrefix + "interface set port " + portName + " up"
	tests := []struct {
		name     string
		existing string
		want     []string
	}{
		{
			name: "creates a port using saved device properties",
			want: []string{
				showPort,
				grcliPrefix + "interface add port " + portName +
					" devargs 0000:03:00.2 rxqs 4 qsize 1024 mtu 9000 down",
				setPortUp,
			},
		},
		{
			name: "keeps a port matching saved device properties",
			existing: `{"name":"enp1s002","type":"port","devargs":"0000:03:00.2",` +
				`"n_rxq":4,"rxq_size":1024,"mtu":9000}`,
			want: []string{showPort, setPortUp},
		},
	}
	state := devicestate.State{
		InterfaceName:  "enp1s0v2",
		PCIAddress:     "0000:03:00.2",
		OriginalDriver: "iavf",
		MTU:            9000,
	}
	sriov := &hostnetwork.SRIOVTrunkParams{
		AcceleratedConfig: &hostnetwork.AcceleratedConfigParams{RXQueues: new(int32(4)), QSize: new(int32(1024))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, map[string]string{showPort: tt.existing})
			err := configureTrunkPort(context.Background(), NewClient("sock"), portName, trunkPortOptions(sriov), state)
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func portJSON(name string) string {
	return fmt.Sprintf(`{"name": %q, "type": "port"}`, name)
}

func vlanJSON(name string) string {
	return fmt.Sprintf(`{"name": %q, "type": "vlan"}`, name)
}

// recordGrcli replaces grcli with a fake that records every command and
// answers the ones in outputs. Any other "interface show name" lookup reports
// a missing interface, and every other command succeeds.
func recordGrcli(t *testing.T, outputs map[string]string) *[]string {
	t.Helper()
	original := execCmd
	t.Cleanup(func() { execCmd = original })

	var got []string
	execCmd = func(_ context.Context, name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		got = append(got, cmd)
		if out, ok := outputs[cmd]; ok && out != "" {
			return []byte(out), nil
		}
		if strings.HasPrefix(cmd, grcliPrefix+"interface show name ") {
			return []byte(interfaceNotFoundOutput), fmt.Errorf("exit status 1")
		}
		return nil, nil
	}
	return &got
}
