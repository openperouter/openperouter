// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"testing"

	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/stretchr/testify/assert"
)

func TestFindStaleVNIs(t *testing.T) {
	tests := []struct {
		name       string
		ifaces     []groutInterface
		configured map[int32]bool
		expected   []int32
	}{
		{
			name:       "no interfaces",
			ifaces:     nil,
			configured: map[int32]bool{},
			expected:   nil,
		},
		{
			name: "all configured",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]bool{100: true, 200: true},
			expected:   nil,
		},
		{
			name: "one stale",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]bool{100: true},
			expected:   []int32{200},
		},
		{
			name: "non-vni interfaces ignored",
			ifaces: []groutInterface{
				{Name: "p0", Type: "port"},
				{Name: "bridge100", Type: "bridge"},
				{Name: "vni100", Type: "vxlan"},
			},
			configured: map[int32]bool{},
			expected:   []int32{100},
		},
		{
			name: "all stale",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
				{Name: "vni300", Type: "vxlan"},
			},
			configured: map[int32]bool{},
			expected:   []int32{100, 200, 300},
		},
		{
			name: "non-vxlan type with vni name ignored",
			ifaces: []groutInterface{
				{Name: "vni100", Type: "bridge"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]bool{},
			expected:   []int32{200},
		},
		{
			name: "trailing text in name ignored",
			ifaces: []groutInterface{
				{Name: "vni100extra", Type: "vxlan"},
				{Name: "vni200", Type: "vxlan"},
			},
			configured: map[int32]bool{},
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
		HostSide:      "host-100",
		NamespaceSide: "pe-100",
	}, result)

	result = linkPairFromVNI(42)
	assert.Equal(t, hostnetwork.VethNames{
		HostSide:      "host-42",
		NamespaceSide: "pe-42",
	}, result)
}

func TestEnsureVRF(t *testing.T) {
	t.Run("creates VRF when it does not exist", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name red",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
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
				cmd: "grcli --err-exit --json --socket sock interface show name vni100",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
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
				cmd: "grcli --err-exit --json --socket sock interface show name vni100",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
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
				cmd: "grcli --err-exit --json --socket sock interface show name red",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			})()

		assert.NoError(t,
			NewClient("sock").deleteInterface(context.Background(), "red"),
		)
	})
}
