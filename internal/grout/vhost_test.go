// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVhostDevargs(t *testing.T) {
	t.Run("stable index and length under GR_PORT_DEVARGS_SIZE", func(t *testing.T) {
		path := "/var/run/grout-vhost/11111111-2222-3333-4444-555555555555/vhu.sock"
		a, err := VhostDevargs("vhu-poc", path, 1, false)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(a), GRPortDevargsSize)
		assert.True(t, strings.HasPrefix(a, "net_vhost"))
		assert.Contains(t, a, "iface="+path)
		assert.Contains(t, a, "queues=1")
		assert.NotContains(t, a, "client=1")
		b, err := VhostDevargs("vhu-poc", path, 1, false)
		require.NoError(t, err)
		assert.Equal(t, a, b, "instance index must be stable for the same name")
	})

	t.Run("different names get different indices", func(t *testing.T) {
		a, err := VhostDevargs("vhu-a", "/var/run/grout-vhost/a/vhu.sock", 1, false)
		require.NoError(t, err)
		b, err := VhostDevargs("vhu-b", "/var/run/grout-vhost/b/vhu.sock", 1, false)
		require.NoError(t, err)
		assert.NotEqual(t, a, b)
	})

	t.Run("rejects oversized path", func(t *testing.T) {
		long := "/var/run/grout-vhost/" + strings.Repeat("x", GRPortDevargsSize) + "/vhu.sock"
		_, err := VhostDevargs("vhu-poc", long, 1, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "GR_PORT_DEVARGS_SIZE")
	})

	t.Run("client mode appends client=1", func(t *testing.T) {
		a, err := VhostDevargs("vhu-poc", "/var/run/grout-vhost/a/vhost.sock", 1, true)
		require.NoError(t, err)
		assert.Contains(t, a, "client=1")
	})
}

func TestCreateVhostPort(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "vhu.sock")
	devargs, err := VhostDevargs("vhu-poc", sock, 1, false)
	require.NoError(t, err)
	idx := vhostInstanceIndex("vhu-poc")

	t.Run("creates port with rxqs qsize up mac and gateway", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name vhu-poc",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			},
			cmdCall{
				cmd: fmt.Sprintf("grcli --err-exit --json --socket sock interface add port vhu-poc devargs %s rxqs 1 qsize 1024 up", devargs),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface set port vhu-poc mac 52:54:00:67:72:01",
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock address add 192.169.20.1/24 iface vhu-poc",
			},
		)()

		err := NewClient("sock").CreateVhostPort(context.Background(), VhostPortParams{
			Name:        "vhu-poc",
			SocketPath:  sock,
			Queues:      1,
			MAC:         "52:54:00:67:72:01",
			GatewayCIDR: VhostGuestGatewayCIDR,
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, idx, uint32(100))
		assert.Less(t, idx, uint32(1000))
	})

	t.Run("idempotent when port exists", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vhu-poc",
				output: `{"name":"vhu-poc","type":"port"}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface set port vhu-poc mac 52:54:00:67:72:01",
				err: fmt.Errorf("grcli interface set port vhu-poc mac 52:54:00:67:72:01 failed: exit status 1, output: {\"error\":\"command failed: Operation not supported (EOPNOTSUPP)\",\"errno\":95}"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock address add 192.169.20.1/24 iface vhu-poc",
				err: fmt.Errorf("address already exists"),
			},
		)()

		err := NewClient("sock").CreateVhostPort(context.Background(), VhostPortParams{
			Name:        "vhu-poc",
			SocketPath:  sock,
			Queues:      1,
			MAC:         "52:54:00:67:72:01",
			GatewayCIDR: VhostGuestGatewayCIDR,
		})
		assert.NoError(t, err)
	})

	t.Run("client recreates port that exists but is not running", func(t *testing.T) {
		clientArgs, err := VhostDevargs("vhu-poc", sock, 1, true)
		require.NoError(t, err)
		defer mockCmdExecSeq(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vhu-poc",
				output: `{"name":"vhu-poc","type":"port","flags":["up","allmulti"]}`,
			},
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vhu-poc",
				output: `{"name":"vhu-poc","type":"port","flags":["up","allmulti"]}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del vhu-poc",
			},
			cmdCall{
				cmd: fmt.Sprintf("grcli --err-exit --json --socket sock interface add port vhu-poc devargs %s rxqs 1 qsize 1024 up", clientArgs),
			},
		)()

		err = NewClient("sock").CreateVhostPort(context.Background(), VhostPortParams{
			Name:       "vhu-poc",
			SocketPath: sock,
			Queues:     1,
			Client:     true,
		})
		assert.NoError(t, err)
	})
}

func TestPortIsRunning(t *testing.T) {
	t.Run("running flag", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceShowP0Output,
			})()
		ok, err := NewClient("sock").PortIsRunning(context.Background(), "p0")
		assert.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("missing port", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name p0",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			})()
		ok, err := NewClient("sock").PortIsRunning(context.Background(), "p0")
		assert.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestDeleteVhostPort(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "vhu.sock")
	require.NoError(t, os.WriteFile(sock, []byte{}, 0o600))

	t.Run("deletes port and unlinks socket", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name vhu-poc",
				output: `{"name":"vhu-poc","type":"port"}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del vhu-poc",
			},
		)()

		assert.NoError(t, NewClient("sock").DeleteVhostPort(context.Background(), "vhu-poc", sock))
		_, err := os.Stat(sock)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("no-op when port gone and socket already gone", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name vhu-poc",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			},
		)()

		assert.NoError(t, NewClient("sock").DeleteVhostPort(context.Background(), "vhu-poc", sock))
	})
}
