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

const interfaceShowP0Output = `NAME      ID  FLAGS                        MODE  DOMAIN  TYPE  INFO
p0   2  up running allmulti tracing  VRF   main    port  devargs=net_tap0,remote=toswitch,iface=tap_toswitch mac=02:ed:97:46:82:57
`

func TestEnsurePort(t *testing.T) {
	t.Run("ensure port when no port exists", func(t *testing.T) {

		defer mockCmdExec(
			cmdCall{
				cmd: "grcli -e -s sock interface show name p0",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			},
			cmdCall{
				cmd: "grcli -e -s sock interface add port p0 devargs net_tap0,remote=remote_i,iface=p0_tap",
			})()

		c := NewClient("sock")
		err := c.ensurePort(context.Background(), "p0", "net_tap0,remote=remote_i,iface=p0_tap")
		assert.NoError(t, err)
	})

	t.Run("ensure port when port already exists", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli -e -s sock interface show name p0",
				output: interfaceShowP0Output,
			})()

		c := NewClient("sock")
		err := c.ensurePort(context.Background(), "p0", "net_tap0,remote=remote_i,iface=p0_tap")
		assert.NoError(t, err)
	})
}

func TestDeletePort(t *testing.T) {
	t.Run("deletes existing port", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli -e -s sock interface show name p0",
				output: interfaceShowP0Output,
			},
			cmdCall{
				cmd: "grcli -e -s sock interface del p0",
			})()

		c := NewClient("sock")
		err := c.deletePort(context.Background(), "p0")
		assert.NoError(t, err)
	})

	t.Run("no-op when port does not exist", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli -e -s sock interface show name p0",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			})()

		c := NewClient("sock")
		err := c.deletePort(context.Background(), "p0")
		assert.NoError(t, err)
	})
}

func TestEnsureAddress(t *testing.T) {
	t.Run("assigns address successfully", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli -e -s sock address add 10.0.0.1/24 iface p0",
			})()

		c := NewClient("sock")
		err := c.ensureAddress(context.Background(), "p0", "10.0.0.1/24")
		assert.NoError(t, err)
	})

	t.Run("no-op when address already assigned", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli -e -s sock address add 10.0.0.1/24 iface p0",
				err: fmt.Errorf("address already exists"),
			})()

		c := NewClient("sock")
		err := c.ensureAddress(context.Background(), "p0", "10.0.0.1/24")
		assert.NoError(t, err)
	})

}

func TestGetAddresses(t *testing.T) {
	t.Run("returns addresses for interface", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli -e -s sock address show iface p0",
				output: `	
				IFACE     FAMILY  ADDRESS
				p0  ipv4    10.0.0.1/24
				p0  ipv6    fd00::1/64
				`,
			})()

		c := NewClient("sock")
		addrs, err := c.getAddresses(context.Background(), "p0")
		assert.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.1/24", "fd00::1/64"}, addrs)
	})

	t.Run("returns empty list when no addresses", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli -e -s sock address show iface p0",
				output: "",
			})()

		c := NewClient("sock")
		addrs, err := c.getAddresses(context.Background(), "p0")
		assert.NoError(t, err)
		assert.Empty(t, addrs)
	})
}
