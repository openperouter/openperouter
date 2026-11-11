// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupL3VNIRouterMAC(t *testing.T) {
	const mac = "02:00:00:00:00:2a"
	failure := errors.New("cannot set MAC")
	tests := []struct {
		name       string
		currentMAC string
		routerMAC  string
		wantError  error
	}{
		{name: "new VXLANs", routerMAC: mac},
		{name: "matching MAC", currentMAC: mac, routerMAC: mac},
		{name: "restore drift", currentMAC: "02:ff:ff:ff:ff:ff", routerMAC: mac},
		{name: "omitted prefix", currentMAC: "02:ff:ff:ff:ff:ff"},
		{name: "set failure", currentMAC: "02:ff:ff:ff:ff:ff", routerMAC: mac, wantError: failure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := execCmd
			t.Cleanup(func() { execCmd = original })
			var routerMAC net.HardwareAddr
			if tt.routerMAC != "" {
				var err error
				routerMAC, err = net.ParseMAC(tt.routerMAC)
				require.NoError(t, err)
			}
			for _, vni := range []int32{100, 200} {
				calls := l3VNIRouterMACCommands(vni, tt.currentMAC, tt.routerMAC, tt.wantError)
				next := 0
				execCmd = func(ctx context.Context, command string, args ...string) ([]byte, error) {
					require.Less(t, next, len(calls), "unexpected command: %s %v", command, args)
					call := calls[next]
					next++
					require.Equal(t, call.cmd, command+" "+strings.Join(args, " "))
					return []byte(call.output), call.err
				}
				err := SetupL3VNI(context.Background(), NewClient("sock"), hostnetwork.L3VNIParams{
					VNIParams: hostnetwork.VNIParams{VNI: vni, VRF: "red", VTEPIP: "10.0.0.1/32"},
					RouterMAC: routerMAC,
				})
				require.ErrorIs(t, err, tt.wantError)
				require.Equal(t, len(calls), next, "missing grcli commands for VNI %d", vni)
			}
		})
	}
}

func TestFindStaleVNIs(t *testing.T) {
	tests := []struct {
		name       string
		ifaces     []groutInterface
		configured map[int32]struct{}
		expected   []int32
	}{
		{
			name:       "no interfaces",
			ifaces:     nil,
			configured: map[int32]struct{}{},
			expected:   nil,
		},
		{
			name: "all configured",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]struct{}{100: {}, 200: {}},
			expected:   nil,
		},
		{
			name: "one stale",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]struct{}{100: {}},
			expected:   []int32{200},
		},
		{
			name: "non-vni interfaces ignored",
			ifaces: []groutInterface{
				{Name: "p0", Type: "port"},
				{Name: "bridge100", Type: "bridge"},
				{Name: "vni100", Type: "vxlan"},
			},
			configured: map[int32]struct{}{},
			expected:   []int32{100},
		},
		{
			name: "all stale",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
				{Name: "vni300", Type: "vxlan"},
			},
			configured: map[int32]struct{}{},
			expected:   []int32{100, 200, 300},
		},
		{
			name: "non-vxlan type with vni name ignored",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "bridge"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]struct{}{},
			expected:   []int32{200},
		},
		{
			name: "trailing text in name ignored",
			ifaces: []groutInterface{
				{Name: "vni100extra", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]struct{}{},
			expected:   []int32{200},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findStaleVNIs(tt.ifaces, tt.configured)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindStaleVRFs(t *testing.T) {
	tests := []struct {
		name       string
		ifaces     []groutInterface
		configured []string
		expected   []string
	}{
		{
			name:       "no interfaces",
			ifaces:     nil,
			configured: []string{},
			expected:   nil,
		},
		{
			name: "all configured",
			ifaces: []groutInterface{
				{Name: "red", Type: "vrf"},
				{Name: "blue", Type: "vrf"},
			},
			configured: []string{"red", "blue"},
			expected:   nil,
		},
		{
			name: "one stale",
			ifaces: []groutInterface{
				{Name: "red", Type: "vrf"},
				{Name: "blue", Type: "vrf"},
			},
			configured: []string{"red"},
			expected:   []string{"blue"},
		},
		{
			name: "non-vrf interfaces ignored",
			ifaces: []groutInterface{
				{Name: "p0", Type: "port"},
				{Name: "vni100", Type: "vxlan"},
				{Name: "stale", Type: "vrf"},
			},
			configured: []string{},
			expected:   []string{"stale"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findStaleVRFs(tt.ifaces, tt.configured)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLinkPairNamesFromVNI(t *testing.T) {
	result := linkPairFromVNI(100)
	assert.Equal(t, hostnetwork.VethNames{
		HostSide:      "host-e-100",
		NamespaceSide: "pe-e-100",
	}, result)

	result = linkPairFromVNI(42)
	assert.Equal(t, hostnetwork.VethNames{
		HostSide:      "host-e-42",
		NamespaceSide: "pe-e-42",
	}, result)
}

func TestEnsureVRF(t *testing.T) {
	t.Run("creates VRF when it does not exist", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name red",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add vrf red",
			})()

		assert.NoError(t,
			NewClient("sock").ensureVRF(context.Background(), "red"),
		)
	})

	t.Run("no-op when VRF already exists", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name red",
				output: `{"name":"red","type":"vrf"}`,
			})()

		assert.NoError(t,
			NewClient("sock").ensureVRF(context.Background(), "red"),
		)
	})

	t.Run("recreates VRF when interface exists with wrong type", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name red",
				output: `{"name":"red","type":"port"}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del red",
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add vrf red",
			})()

		assert.NoError(t,
			NewClient("sock").ensureVRF(context.Background(), "red"),
		)
	})
}

func TestEnsureVXLAN(t *testing.T) {
	t.Run("creates VXLAN when it does not exist", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vni100",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add vxlan vni100 vni 100 local 10.0.0.1 dst_port 4789 vrf red encap_vrf main",
			})()

		assert.NoError(t,
			NewClient("sock").ensureVXLAN(context.Background(), "vni100", "10.0.0.1", "red", 100, 4789),
		)
	})

	t.Run("creates VXLAN without VRF", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vni100",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add vxlan vni100 vni 100 local 10.0.0.1 dst_port 4789 encap_vrf main",
			})()

		assert.NoError(t,
			NewClient("sock").ensureVXLAN(context.Background(), "vni100", "10.0.0.1", "", 100, 4789),
		)
	})

	t.Run("no-op when VXLAN already exists with same config", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vni100",
				output: `{"name":"vni100","type":"vxlan","vni":100,"local":"10.0.0.1","dst_port":4789,"vrf":"red"}`,
			})()

		assert.NoError(t,
			NewClient("sock").ensureVXLAN(context.Background(), "vni100", "10.0.0.1", "red", 100, 4789),
		)
	})

	t.Run("recreates VXLAN when config changed", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vni100",
				output: `{"name":"vni100","type":"vxlan","vni":100,"local":"10.0.0.1","dst_port":4789,"vrf":"red"}`,
			},
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vni100",
				output: `{"name":"vni100","type":"vxlan","vni":100,"local":"10.0.0.1","dst_port":4789,"vrf":"red"}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del vni100",
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add vxlan vni100 vni 100 local 10.0.0.2 dst_port 4789 vrf red encap_vrf main",
			})()

		assert.NoError(t,
			NewClient("sock").ensureVXLAN(context.Background(), "vni100", "10.0.0.2", "red", 100, 4789),
		)
	})

	t.Run("recreates VXLAN when port changed", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vni100",
				output: `{"name":"vni100","type":"vxlan","vni":100,"local":"10.0.0.1","dst_port":4789,"vrf":"red"}`,
			},
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vni100",
				output: `{"name":"vni100","type":"vxlan","vni":100,"local":"10.0.0.1","dst_port":4789,"vrf":"red"}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del vni100",
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add vxlan vni100 vni 100 local 10.0.0.1 dst_port 5000 vrf red encap_vrf main",
			})()

		assert.NoError(t,
			NewClient("sock").ensureVXLAN(context.Background(), "vni100", "10.0.0.1", "red", 100, 5000),
		)
	})
}

func TestDeleteInterface(t *testing.T) {
	t.Run("deletes existing interface", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name red",
				output: `{"name":"red","type":"vrf"}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del red",
			})()

		assert.NoError(t,
			NewClient("sock").deleteInterface(context.Background(), "red"),
		)
	})

	t.Run("no-op when interface does not exist", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name red",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			})()

		assert.NoError(t,
			NewClient("sock").deleteInterface(context.Background(), "red"),
		)
	})
}

func l3VNIRouterMACCommands(vni int32, currentMAC, routerMAC string, setError error) []cmdCall {
	const base = "grcli --err-exit --json --socket sock "
	name := fmt.Sprintf("vni%d", vni)
	info := fmt.Sprintf(`{"vni":%d,"local":"10.0.0.1","dst_port":4789,"vrf":"red","mac":%q}`, vni, currentMAC)
	show := cmdCall{cmd: base + "interface show name " + name, output: info}
	calls := []cmdCall{{cmd: base + "interface show name red", output: `{"name":"red","type":"vrf"}`}}
	if currentMAC == "" {
		calls = append(calls,
			cmdCall{cmd: show.cmd, output: interfaceNotFoundOutput, err: errors.New("exit status 1")},
			cmdCall{cmd: fmt.Sprintf("%sinterface add vxlan %s vni %d local 10.0.0.1 dst_port 4789 vrf red encap_vrf main", base, name, vni)},
		)
	} else {
		calls = append(calls, show)
	}
	if routerMAC == "" {
		return calls
	}
	calls = append(calls, show)
	if currentMAC != routerMAC {
		calls = append(calls, cmdCall{cmd: base + "interface set vxlan " + name + " mac " + routerMAC, err: setError})
	}
	return calls
}
