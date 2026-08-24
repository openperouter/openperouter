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

const interfaceShowUnderlayPortOutput = `{
	"name": "u_enp3s0f0v0",
	"type": "port",
	"id": 2,
	"flags": ["up", "running", "promisc"],
	"devargs": "0000:03:02.0",
	"mac": "aa:bb:cc:dd:ee:ff",
	"n_rxq": 4,
	"rxq_size": 1024,
	"description": "underlay"
}`

func TestEnsurePortWithOptions(t *testing.T) {
	rxqs := int32(4)
	qsize := int32(1024)
	promisc := true
	mac := "aa:bb:cc:dd:ee:ff"
	opts := PortOptions{
		RXQueues:    &rxqs,
		QSize:       &qsize,
		Promiscuous: &promisc,
		MAC:         &mac,
		Description: UnderlayInterfaceDescriptionMarker,
	}

	t.Run("appends optional port arguments", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_enp3s0f0v0 devargs 0000:03:02.0 rxqs 4 qsize 1024 promisc on mac aa:bb:cc:dd:ee:ff description underlay",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePortWithOptions(
				context.Background(),
				"u_enp3s0f0v0",
				"0000:03:02.0",
				opts,
			),
		)
	})

	t.Run("omits unset options", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				err: fmt.Errorf("error: command failed: No such device (ENODEV)"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_enp3s0f0v0 devargs 0000:03:02.0 description underlay",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePortWithOptions(
				context.Background(),
				"u_enp3s0f0v0",
				"0000:03:02.0",
				PortOptions{Description: UnderlayInterfaceDescriptionMarker},
			),
		)
	})

	t.Run("no-op when existing port already matches", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				output: interfaceShowUnderlayPortOutput,
			})()

		assert.NoError(t,
			NewClient("sock").ensurePortWithOptions(
				context.Background(),
				"u_enp3s0f0v0",
				"0000:03:02.0",
				opts,
			),
		)
	})

	t.Run("deletes and recreates when existing port options differ", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				output: `{"name":"u_enp3s0f0v0","type":"port","devargs":"0000:03:02.0","n_rxq":1,"rxq_size":256,"description":"underlay","flags":["up"]}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del u_enp3s0f0v0",
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_enp3s0f0v0 devargs 0000:03:02.0 rxqs 4 qsize 1024 promisc on mac aa:bb:cc:dd:ee:ff description underlay",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePortWithOptions(
				context.Background(),
				"u_enp3s0f0v0",
				"0000:03:02.0",
				opts,
			),
		)
	})
}

func TestGetInterfaceDetails(t *testing.T) {
	t.Run("parses port details", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				output: interfaceShowUnderlayPortOutput,
			})()

		details, err := NewClient("sock").getInterfaceDetails(context.Background(), "u_enp3s0f0v0")
		assert.NoError(t, err)
		assert.Equal(t, "u_enp3s0f0v0", details.Name)
		assert.Equal(t, "port", details.Type)
		assert.Equal(t, "underlay", details.Description)
		assert.Equal(t, "0000:03:02.0", details.Devargs)
		assert.Equal(t, "aa:bb:cc:dd:ee:ff", details.MAC)
		assert.Equal(t, int32(4), details.NRxq)
		assert.Equal(t, int32(1024), details.RxqSize)
		assert.Equal(t, []string{"up", "running", "promisc"}, details.Flags)
	})
}

func TestMatchesRequested(t *testing.T) {
	rxqs := int32(4)
	qsize := int32(1024)
	promisc := true
	mac := "AA:BB:CC:DD:EE:FF"
	matching := groutInterfaceProperties{
		Devargs:     "0000:03:02.0",
		Description: "underlay",
		MAC:         "aa:bb:cc:dd:ee:ff",
		NRxq:        4,
		RxqSize:     1024,
		Flags:       []string{"up", "running", "promisc"},
	}
	opts := PortOptions{
		RXQueues:    &rxqs,
		QSize:       &qsize,
		Promiscuous: &promisc,
		MAC:         &mac,
		Description: "underlay",
	}

	t.Run("matching options", func(t *testing.T) {
		assert.True(t, matching.matchesRequested("0000:03:02.0", opts))
	})

	t.Run("mismatching rx queues", func(t *testing.T) {
		details := matching
		details.NRxq = 1
		assert.False(t, details.matchesRequested("0000:03:02.0", opts))
	})

	t.Run("mismatching queue size", func(t *testing.T) {
		details := matching
		details.RxqSize = 256
		assert.False(t, details.matchesRequested("0000:03:02.0", opts))
	})

	t.Run("mismatching mac", func(t *testing.T) {
		details := matching
		details.MAC = "00:00:00:00:00:00"
		assert.False(t, details.matchesRequested("0000:03:02.0", opts))
	})

	t.Run("mismatching promiscuous", func(t *testing.T) {
		details := matching
		details.Flags = []string{"up", "running"}
		assert.False(t, details.matchesRequested("0000:03:02.0", opts))
	})

	t.Run("mismatching description", func(t *testing.T) {
		details := matching
		details.Description = ""
		assert.False(t, details.matchesRequested("0000:03:02.0", opts))
	})

	t.Run("mismatching devargs", func(t *testing.T) {
		assert.False(t, matching.matchesRequested("0000:ff:00.0", opts))
	})

	t.Run("unspecified fields are ignored", func(t *testing.T) {
		assert.True(t, matching.matchesRequested("0000:03:02.0", PortOptions{Description: "underlay"}))
	})

	t.Run("tap devargs with random suffix still match", func(t *testing.T) {
		tap := groutInterfaceProperties{
			Devargs:     "net_tapabc123,remote=eth0,iface=tap_eth0",
			Description: "underlay",
			MTU:         1500,
		}
		mtu := int32(1500)
		assert.True(t, tap.matchesRequested("net_tapxyz789,remote=eth0,iface=tap_eth0", PortOptions{
			MTU:         &mtu,
			Description: "underlay",
		}))
	})

	t.Run("tap vs pci is a mismatch", func(t *testing.T) {
		assert.False(t, matching.matchesRequested("net_tap0,remote=eth0,iface=tap_eth0", opts))
	})

	t.Run("mismatching mtu", func(t *testing.T) {
		details := matching
		details.MTU = 1500
		mtu := int32(9000)
		assert.False(t, details.matchesRequested("0000:03:02.0", PortOptions{MTU: &mtu, Description: "underlay"}))
	})
}
