// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"strings"
	"syscall"
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

const interfaceNotFoundOutput = `{"error":"interface lookup failed","errno":19}`

func TestEnsurePort(t *testing.T) {
	t.Run("ensure port when no port exists", func(t *testing.T) {

		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port p0 devargs net_tap0,remote=remote_i,iface=p0_tap",
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
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
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
				cmd:    "grcli --err-exit --json --socket sock address add 10.0.0.1/24 iface p0",
				output: `{"error":"command failed: File exists (EEXIST)","errno":17}`,
				err:    fmt.Errorf("exit status 1"),
			})()

		assert.NoError(t,
			NewClient("sock").ensureAddress(context.Background(), "p0", "10.0.0.1/24"),
		)
	})

	// EADDRINUSE reads as "already in use" but the address was not assigned:
	// grout only holds a dangling nexthop for it, left over by an earlier
	// failed add. Reporting success here loses the address.
	t.Run("fails when grout holds a dangling nexthop for the address", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock address add 2001:db8:11::3/64 iface p0",
				output: `{"error":"command failed: Address already in use (EADDRINUSE)","errno":98}`,
				err:    fmt.Errorf("exit status 1"),
			})()

		assert.Error(t,
			NewClient("sock").ensureAddress(context.Background(), "p0", "2001:db8:11::3/64"),
		)
	})

	t.Run("fails when grout is out of space", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock address add 2001:db8:11::3/64 iface p0",
				output: `{"error":"command failed: No space left on device (ENOSPC)","errno":28}`,
				err:    fmt.Errorf("exit status 1"),
			})()

		assert.Error(t,
			NewClient("sock").ensureAddress(context.Background(), "p0", "2001:db8:11::3/64"),
		)
	})
}

func TestIsGroutErrno(t *testing.T) {
	tests := []struct {
		name   string
		output string
		cmdErr error
		errno  syscall.Errno
		want   bool
	}{
		{
			name:   "matching errno",
			output: `{"error":"command failed: File exists (EEXIST)","errno":17}`,
			cmdErr: fmt.Errorf("exit status 1"),
			errno:  syscall.EEXIST,
			want:   true,
		},
		{
			name:   "different errno",
			output: `{"error":"command failed: Address already in use (EADDRINUSE)","errno":98}`,
			cmdErr: fmt.Errorf("exit status 1"),
			errno:  syscall.EEXIST,
			want:   false,
		},
		{
			name:   "output is not a grout error payload",
			output: "grcli: command not found",
			cmdErr: fmt.Errorf("exit status 127"),
			errno:  syscall.EEXIST,
			want:   false,
		},
		{
			name:  "no error at all",
			errno: syscall.EEXIST,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer mockCmdExec(
				cmdCall{
					cmd:    "grcli --err-exit --json --socket sock address add 10.0.0.1/24 iface p0",
					output: tt.output,
					err:    tt.cmdErr,
				})()

			err := NewClient("sock").run(context.Background(), "address", "add", "10.0.0.1/24", "iface", "p0")
			assert.Equal(t, tt.want, isGroutErrno(err, tt.errno))
		})
	}
}

func TestGetInterfaceInfoClassifiesErrorsByErrno(t *testing.T) {
	t.Run("ENODEV means interface is absent regardless of message", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			})()

		info, err := NewClient("sock").getInterfaceInfo(context.Background(), "p0")
		assert.NoError(t, err)
		assert.Nil(t, info)
	})

	t.Run("No such message with another errno remains an error", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: `{"error":"No such interface","errno":5}`,
				err:    fmt.Errorf("exit status 1"),
			})()

		info, err := NewClient("sock").getInterfaceInfo(context.Background(), "p0")
		assert.Error(t, err)
		assert.Nil(t, info)
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
