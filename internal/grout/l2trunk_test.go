// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/stretchr/testify/require"
)

const grcliPrefix = "grcli --err-exit --json --socket sock "

func TestTrunkPortName(t *testing.T) {
	tests := []struct {
		pciAddr string
		want    string
	}{
		{pciAddr: "0000:03:00.1", want: "t_000003001"},
		{pciAddr: "ffff:ff:1f.7", want: "t_ffffff1f7"},
	}
	for _, tt := range tests {
		t.Run(tt.pciAddr, func(t *testing.T) {
			got := trunkPortName(tt.pciAddr)
			require.Equal(t, tt.want, got)
			require.Less(t, len(got), 16, "grout interface names must fit in IFNAMSIZ")
		})
	}
}

func TestTrunkVLANName(t *testing.T) {
	tests := []struct {
		portName string
		tag      int32
		want     string
	}{
		{portName: "t_000003001", tag: 33, want: "t000003001.33"},
		{portName: "t_ffffff1f7", tag: 4094, want: "tffffff1f7.4094"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := trunkVLANName(tt.portName, tt.tag)
			require.Equal(t, tt.want, got)
			require.Less(t, len(got), 16, "grout interface names must fit in IFNAMSIZ")
		})
	}
}

func TestDeleteBridgeVLANs(t *testing.T) {
	const interfaces = `[
		{"name": "br-pe-100", "type": "bridge"},
		{"name": "t000003001.33", "type": "vlan", "domain": "br-pe-100", "vlan_parent": "t_000003001", "vlan_id": 33},
		{"name": "t000003002.44", "type": "vlan", "domain": "br-pe-100", "vlan_parent": "t_000003002", "vlan_id": 44},
		{"name": "t000003001.55", "type": "vlan", "domain": "br-pe-200", "vlan_parent": "t_000003001", "vlan_id": 55}
	]`
	tests := []struct {
		name string
		keep string
		want []string
	}{
		{
			name: "keeps the configured VLAN",
			keep: "t000003001.33",
			want: []string{
				grcliPrefix + "interface show",
				grcliPrefix + "interface show name t000003002.44",
				grcliPrefix + "interface del t000003002.44",
			},
		},
		{
			name: "deletes every VLAN of the bridge",
			want: []string{
				grcliPrefix + "interface show",
				grcliPrefix + "interface show name t000003001.33",
				grcliPrefix + "interface del t000003001.33",
				grcliPrefix + "interface show name t000003002.44",
				grcliPrefix + "interface del t000003002.44",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, map[string]string{
				grcliPrefix + "interface show":                    interfaces,
				grcliPrefix + "interface show name t000003001.33": vlanJSON("t000003001.33"),
				grcliPrefix + "interface show name t000003002.44": vlanJSON("t000003002.44"),
			})
			err := deleteBridgeVLANs(context.Background(), NewClient("sock"), "br-pe-100", tt.keep)
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func TestL2TrunkPortName(t *testing.T) {
	sysfs := pci.Sysfs{Root: t.TempDir()}
	deviceDir := filepath.Join(sysfs.Root, "class", "net", "enp1s0", "device")
	require.NoError(t, os.MkdirAll(deviceDir, 0o755))
	require.NoError(t, os.Symlink("../0000:03:00.2", filepath.Join(deviceDir, "virtfn1")))
	require.NoError(t, os.Symlink("../not-pci", filepath.Join(deviceDir, "virtfn3")))

	tests := []struct {
		name    string
		params  hostnetwork.L2TrunkParams
		want    string
		wantErr string
	}{
		{
			name: "SR-IOV VF",
			params: hostnetwork.L2TrunkParams{
				Name:  "workloads",
				SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 1},
			},
			want: "t_000003002",
		},
		{
			name: "missing VF",
			params: hostnetwork.L2TrunkParams{
				Name:  "workloads",
				SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 4},
			},
			wantErr: `failed to resolve VF 4 of PF "enp1s0" to PCI address`,
		},
		{
			name: "invalid virtfn target",
			params: hostnetwork.L2TrunkParams{
				Name:  "workloads",
				SRIOV: &hostnetwork.SRIOVTrunkParams{PFName: "enp1s0", VFIndex: 3},
			},
			wantErr: `resolved VF symlink target "not-pci" for VF 3 of "enp1s0" does not look like a PCI address`,
		},
		{
			name:    "host trunk",
			params:  hostnetwork.L2TrunkParams{Name: "workloads"},
			wantErr: errUnsupportedTrunk.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := L2TrunkPortName(tt.params, sysfs)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEnsureVLAN(t *testing.T) {
	const showTrunk100 = grcliPrefix + "interface show name t000003001.33"

	tests := []struct {
		name     string
		existing string
		want     []string
	}{
		{
			name: "creates a missing VLAN",
			want: []string{
				showTrunk100,
				grcliPrefix + "interface add vlan t000003001.33 parent t_000003001 vlan_id 33",
			},
		},
		{
			name: "keeps a matching VLAN",
			existing: `{"name": "t000003001.33", "type": "vlan", "mode": "bridge", "domain": "br-pe-100",
				"vlan_parent": "t_000003001", "vlan_id": 33}`,
			want: []string{showTrunk100},
		},
		{
			name: "keeps a matching unattached VLAN",
			existing: `{"name": "t000003001.33", "type": "vlan", "mode": "VRF", "vrf": "main",
				"vlan_parent": "t_000003001", "vlan_id": 33}`,
			want: []string{showTrunk100},
		},
		{
			name: "keeps a matching VLAN on another bridge",
			existing: `{"name": "t000003001.33", "type": "vlan", "domain": "br-pe-200",
				"vlan_parent": "t_000003001", "vlan_id": 33}`,
			want: []string{showTrunk100},
		},
		{
			name: "recreates a VLAN with a different tag",
			existing: `{"name": "t000003001.33", "type": "vlan", "mode": "bridge", "domain": "br-pe-100",
				"vlan_parent": "t_000003001", "vlan_id": 44}`,
			want: []string{
				showTrunk100,
				showTrunk100,
				grcliPrefix + "interface del t000003001.33",
				grcliPrefix + "interface add vlan t000003001.33 parent t_000003001 vlan_id 33",
			},
		},
		{
			name: "recreates a VLAN with a different parent",
			existing: `{"name": "t000003001.33", "type": "vlan",
				"vlan_parent": "t_000003002", "vlan_id": 33}`,
			want: []string{
				showTrunk100,
				showTrunk100,
				grcliPrefix + "interface del t000003001.33",
				grcliPrefix + "interface add vlan t000003001.33 parent t_000003001 vlan_id 33",
			},
		},
		{
			name:     "recreates an interface with a different type",
			existing: `{"name": "t000003001.33", "type": "port"}`,
			want: []string{
				showTrunk100,
				showTrunk100,
				grcliPrefix + "interface del t000003001.33",
				grcliPrefix + "interface add vlan t000003001.33 parent t_000003001 vlan_id 33",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recordGrcli(t, map[string]string{showTrunk100: tt.existing})

			err := NewClient("sock").ensureVLAN(context.Background(), "t000003001.33", "t_000003001", 33)
			require.NoError(t, err)
			require.Equal(t, tt.want, *got)
		})
	}
}

func TestRemoveNonConfiguredL2Trunks(t *testing.T) {
	const interfaces = `[
		{"name": "u_eth0", "type": "port", "devargs": "0000:01:00.0"},
		{"name": "t_000003001", "type": "port", "devargs": "0000:03:00.1"},
		{"name": "t_000003002", "type": "port", "devargs": "0000:03:00.2"},
		{"name": "t_000003003", "type": "port", "devargs": "0000:03:00.3"},
		{"name": "t000003001.33", "type": "vlan", "vlan_parent": "t_000003001", "vlan_id": 33},
		{"name": "t000003002.44", "type": "vlan", "vlan_parent": "t_000003002", "vlan_id": 44}
	]`

	kept := devicestate.State{
		PCIAddress: "0000:03:00.1", OriginalDriver: pci.DriverVFIOPCI,
	}
	removed := devicestate.State{
		PCIAddress: "0000:03:00.2", OriginalDriver: pci.DriverVFIOPCI,
	}
	store := devicestate.Store{Dir: t.TempDir()}
	for _, state := range []devicestate.State{kept, removed} {
		require.NoError(t, store.Save(state.PCIAddress, state))
	}

	got := recordGrcli(t, map[string]string{
		grcliPrefix + "interface show":                    interfaces,
		grcliPrefix + "interface show name t000003002.44": vlanJSON("t000003002.44"),
		grcliPrefix + "interface show name t_000003002":   portJSON("t_000003002"),
		grcliPrefix + "interface show name t_000003003":   portJSON("t_000003003"),
	})

	err := RemoveNonConfiguredL2Trunks(context.Background(), NewClient("sock"), "", []string{"t_000003001"}, store,
		pci.Sysfs{Root: t.TempDir()})
	require.NoError(t, err)

	// A port goes after its VLANs, as grout refuses to delete a port with
	// sub-interfaces, and so does the port without a saved state.
	want := []string{
		grcliPrefix + "interface show",
		grcliPrefix + "interface show name t000003002.44",
		grcliPrefix + "interface del t000003002.44",
		grcliPrefix + "interface show name t_000003002",
		grcliPrefix + "interface del t_000003002",
		grcliPrefix + "interface show",
		grcliPrefix + "interface show",
		grcliPrefix + "interface show name t_000003003",
		grcliPrefix + "interface del t_000003003",
	}
	require.Equal(t, want, *got)

	states, err := store.List()
	require.NoError(t, err)
	require.Equal(t, []devicestate.State{kept}, states)
}

func TestCheckVFIsFree(t *testing.T) {
	const pciAddr = "0000:03:00.2"
	tests := []struct {
		name       string
		interfaces string
		wantErr    error
	}{
		{
			name:       "no port",
			interfaces: `[]`,
		},
		{
			name: "ports on other devices",
			interfaces: `[
				{"name": "u_eth0", "type": "port", "devargs": "0000:01:00.0"},
				{"name": "t_000003001", "type": "port", "devargs": "0000:03:00.1"}
			]`,
		},
		{
			name: "the trunk's own port",
			interfaces: `[
				{"name": "t_000003002", "type": "port", "devargs": "0000:03:00.2"},
				{"name": "t000003002.33", "type": "vlan", "vlan_parent": "t_000003002", "vlan_id": 33}
			]`,
		},
		{
			name: "underlay port on the trunk VF",
			interfaces: `[
				{"name": "u_enp1s0v2", "type": "port", "devargs": "0000:03:00.2"}
			]`,
			wantErr: errTrunkVFInUse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recordGrcli(t, map[string]string{grcliPrefix + "interface show": tt.interfaces})
			err := checkVFIsFree(context.Background(), NewClient("sock"), pciAddr)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestEnsureTrunkDeviceState(t *testing.T) {
	const pciAddr = "0000:03:00.2"
	kernel := devicestate.State{
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
	vfio := devicestate.State{
		PCIAddress:     pciAddr,
		OriginalDriver: pci.DriverVFIOPCI,
	}

	tests := []struct {
		name   string
		saved  *devicestate.State
		driver string
		want   devicestate.State
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
			name:   "captures a VF already bound to vfio-pci",
			driver: pci.DriverVFIOPCI,
			want:   vfio,
		},
	}
	unrelated := devicestate.State{
		InterfaceName:  "enp1s0v1",
		PCIAddress:     "0000:03:00.1",
		OriginalDriver: "iavf",
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := devicestate.Store{Dir: t.TempDir()}
			require.NoError(t, store.Save(unrelated.PCIAddress, unrelated))
			if tt.saved != nil {
				require.NoError(t, store.Save(pciAddr, *tt.saved))
			}
			sysfs := newFakeSysfs(t, pciAddr, tt.driver)
			// The state is captured once and reused on the next pass.
			for range 2 {
				got, err := ensureTrunkDeviceState(pciAddr, store, sysfs.Sysfs)
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
	const portName = "t_000003002"
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
			existing: `{"name":"t_000003002","type":"port","devargs":"0000:03:00.2",` +
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
