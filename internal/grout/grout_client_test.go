// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type cmdCall struct {
	cmd    string
	output string
	err    error
}

func mockCmdExec(cmdCalls ...cmdCall) func() {
	original := execCmd

	execCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		for _, call := range cmdCalls {
			if call.cmd == cmd {
				fmt.Printf("mockCmdExec matched: %s %s\n", name, strings.Join(args, " "))
				return []byte(call.output), call.err
			}
		}

		return nil, fmt.Errorf("unexpected command: [%s]", cmd)
	}
	return func() {
		execCmd = original
	}
}

const interfaceShowP0Output = `{
	"name": "p0",
	"type": "port",
	"id": 2,
	"flags": ["up", "running", "allmulti", "tracing"],
	"mode": "VRF",
	"domain": "main",
	"mtu": 1500,
	"speed": "unknown"
}`

func TestSetPortUp(t *testing.T) {
	defer mockCmdExec(cmdCall{
		cmd: "grcli --err-exit --json --socket sock interface set port pe-100 up",
	})()

	assert.NoError(t, NewClient("sock").setPortUp(context.Background(), "pe-100"))
}

func TestEnsurePort(t *testing.T) {
	t.Run("ensure port when no port exists", func(t *testing.T) {

		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name p0",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port p0 devargs net_tap0,remote=remote_i,iface=p0_tap down",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"p0",
				"net_tap0,remote=remote_i,iface=p0_tap",
			),
		)
	})

	t.Run("ensure port when port already exists", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceShowP0Output,
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"p0",
				"net_tap0,remote=remote_i,iface=p0_tap",
			),
		)
	})
}

func TestEnsurePortInVRF(t *testing.T) {
	defer mockCmdExec(
		cmdCall{
			cmd: "grcli --err-exit --json --socket sock interface show name pe-100",
			err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
		},
		cmdCall{
			cmd: "grcli --err-exit --json --socket sock interface add port pe-100 devargs net_tap0,iface=host-100 vrf red down",
		},
	)()

	assert.NoError(t, NewClient("sock").ensurePortInVRF(
		context.Background(), "pe-100", "net_tap0,iface=host-100", "red"))
}

func TestEnsureBridge(t *testing.T) {
	t.Run("creates bridge in VRF with neighbor suppression", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name br-pe-100",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add bridge br-pe-100 vrf red neigh_suppress on",
			},
		)()

		assert.NoError(t, NewClient("sock").ensureBridge(context.Background(), "br-pe-100", "red"))
	})

	t.Run("keeps an existing bridge", func(t *testing.T) {
		defer mockCmdExec(cmdCall{
			cmd:    "grcli --err-exit --json --socket sock interface show name br-pe-100",
			output: `{"name":"br-pe-100","type":"bridge"}`,
		})()

		assert.NoError(t, NewClient("sock").ensureBridge(context.Background(), "br-pe-100", "red"))
	})
}

func TestConfigureBridge(t *testing.T) {
	client := NewClient("sock")

	t.Run("sets the anycast MAC", func(t *testing.T) {
		defer mockCmdExec(cmdCall{
			cmd: "grcli --err-exit --json --socket sock interface set bridge br-pe-100 mac 00:f3:00:00:00:65",
		})()

		assert.NoError(t, client.setBridgeMAC(context.Background(), "br-pe-100", "00:f3:00:00:00:65"))
	})

	t.Run("attaches a member", func(t *testing.T) {
		defer mockCmdExec(cmdCall{
			cmd: "grcli --err-exit --json --socket sock interface set vxlan vni100 domain br-pe-100",
		})()

		assert.NoError(t, client.ensureBridgeMember(context.Background(), "vxlan", "br-pe-100", "vni100"))
	})
}

func TestDeletePort(t *testing.T) {
	t.Run("deletes existing port", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceShowP0Output,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del p0",
			})()

		assert.NoError(t,
			NewClient("sock").deletePort(context.Background(), "p0"),
		)
	})

	t.Run("no-op when port does not exist", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name p0",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			})()

		assert.NoError(t,
			NewClient("sock").deletePort(context.Background(), "p0"),
		)
	})
}

func TestEnsureAddress(t *testing.T) {
	t.Run("assigns address successfully", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock address add 10.0.0.1/24 iface p0",
			})()

		assert.NoError(t,
			NewClient("sock").ensureAddress(context.Background(), "p0", "10.0.0.1/24"),
		)
	})

	t.Run("no-op when address already assigned", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock address add 10.0.0.1/24 iface p0",
				err: fmt.Errorf("address already exists"),
			})()

		assert.NoError(t,
			NewClient("sock").ensureAddress(context.Background(), "p0", "10.0.0.1/24"),
		)
	})

}

func TestGetAddresses(t *testing.T) {
	t.Run("returns addresses for interface", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock address show iface p0",
				output: `[{"iface":"p0","family":"ipv4","address":"10.0.0.1/24"},{"iface":"p0","family":"ipv6","address":"fd00::1/64"}]`,
			})()

		addrs, err := NewClient("sock").getAddresses(context.Background(), "p0")
		assert.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.1/24", "fd00::1/64"}, addrs)
	})

	t.Run("returns empty list when no addresses", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock address show iface p0",
				output: "[]",
			})()

		addrs, err := NewClient("sock").getAddresses(context.Background(), "p0")
		assert.NoError(t, err)
		assert.Empty(t, addrs)
	})
}
