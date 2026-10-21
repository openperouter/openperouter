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
	"devargs": "net_tap0,remote=remote_i,iface=p0_tap",
	"flags": ["up", "running", "allmulti", "tracing"],
	"mode": "VRF",
	"domain": "main",
	"mtu": 1500,
	"speed": "unknown"
}`

const interfaceNotFoundOutput = `{"error":"interface lookup failed","errno":19}`

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
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port p0 devargs net_tap0,remote=remote_i,iface=p0_tap mtu 9500 down",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"p0",
				portOptions{Devargs: "net_tap0,remote=remote_i,iface=p0_tap", MTU: 9500},
			),
		)
	})

	t.Run("ensure port when port already exists with the same MTU", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceShowP0Output,
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"p0",
				portOptions{Devargs: "net_tap0,remote=remote_i,iface=p0_tap", MTU: 1500},
			),
		)
	})

	t.Run("ensure port when port already exists with a different MTU", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name p0",
				output: interfaceShowP0Output,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del p0",
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port p0 devargs net_tap0,remote=remote_i,iface=p0_tap mtu 9500 down",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"p0",
				portOptions{Devargs: "net_tap0,remote=remote_i,iface=p0_tap", MTU: 9500},
			),
		)
	})
}

func TestEnsurePortWithVRF(t *testing.T) {
	defer mockCmdExec(
		cmdCall{
			cmd:    "grcli --err-exit --json --socket sock interface show name pe-100",
			output: interfaceNotFoundOutput,
			err:    fmt.Errorf("exit status 1"),
		},
		cmdCall{
			cmd: "grcli --err-exit --json --socket sock interface add port pe-100 devargs net_tap0,iface=host-100 vrf red down",
		},
	)()

	assert.NoError(t, NewClient("sock").ensurePort(
		context.Background(), "pe-100", portOptions{Devargs: "net_tap0,iface=host-100", VRF: "red"}))
}

func TestEnsureBridge(t *testing.T) {
	t.Run("creates bridge in VRF with neighbor suppression and snooping", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name br-pe-100",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add bridge br-pe-100 vrf red neigh_suppress on neigh_snoop on",
			},
		)()

		assert.NoError(t, NewClient("sock").ensureBridge(context.Background(), "br-pe-100", "red"))
	})

	t.Run("creates bridge without a VRF", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name br-pe-100",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add bridge br-pe-100 neigh_suppress on neigh_snoop on",
			},
		)()

		assert.NoError(t, NewClient("sock").ensureBridge(context.Background(), "br-pe-100", ""))
	})

	t.Run("keeps an existing bridge", func(t *testing.T) {
		defer mockCmdExec(cmdCall{
			cmd:    "grcli --err-exit --json --socket sock interface show name br-pe-100",
			output: `{"name":"br-pe-100","type":"bridge","vrf":"red"}`,
		})()

		assert.NoError(t, NewClient("sock").ensureBridge(context.Background(), "br-pe-100", "red"))
	})

	t.Run("keeps an existing bridge in the default VRF", func(t *testing.T) {
		defer mockCmdExec(cmdCall{
			cmd:    "grcli --err-exit --json --socket sock interface show name br-pe-100",
			output: `{"name":"br-pe-100","type":"bridge","vrf":"main"}`,
		})()

		assert.NoError(t, NewClient("sock").ensureBridge(context.Background(), "br-pe-100", ""))
	})

	t.Run("recreates a bridge in a different VRF", func(t *testing.T) {
		original := execCmd
		t.Cleanup(func() { execCmd = original })

		var got []string
		execCmd = func(_ context.Context, name string, args ...string) ([]byte, error) {
			cmd := name + " " + strings.Join(args, " ")
			got = append(got, cmd)
			switch cmd {
			case "grcli --err-exit --json --socket sock interface show name br-pe-100":
				return []byte(`{"name":"br-pe-100","type":"bridge","vrf":"red"}`), nil
			case "grcli --err-exit --json --socket sock interface del br-pe-100",
				"grcli --err-exit --json --socket sock interface add bridge br-pe-100 vrf blue neigh_suppress on neigh_snoop on":
				return nil, nil
			default:
				return nil, fmt.Errorf("unexpected command: %s", cmd)
			}
		}

		assert.NoError(t, NewClient("sock").ensureBridge(context.Background(), "br-pe-100", "blue"))
		assert.Equal(t, []string{
			"grcli --err-exit --json --socket sock interface show name br-pe-100",
			"grcli --err-exit --json --socket sock interface show name br-pe-100",
			"grcli --err-exit --json --socket sock interface del br-pe-100",
			"grcli --err-exit --json --socket sock interface add bridge br-pe-100 vrf blue neigh_suppress on neigh_snoop on",
		}, got)
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
	"devargs": "0000:03:02.0",
	"mac": "aa:bb:cc:dd:ee:ff",
	"n_rxq": 4,
	"rxq_size": 1024,
	"description": "underlay"
}`

func TestEnsurePortOptions(t *testing.T) {
	rxqs := int32(4)
	qsize := int32(1024)
	opts := portOptions{
		Devargs:  "0000:03:02.0",
		RXQueues: &rxqs,
		QSize:    &qsize,
	}

	t.Run("appends optional port arguments", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_enp3s0f0v0 devargs 0000:03:02.0 rxqs 4 qsize 1024 down",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"u_enp3s0f0v0",
				opts,
			),
		)
	})

	t.Run("omits unset options", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				output: interfaceNotFoundOutput,
				err:    fmt.Errorf("exit status 1"),
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_enp3s0f0v0 devargs 0000:03:02.0 down",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"u_enp3s0f0v0",
				portOptions{Devargs: "0000:03:02.0"},
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
			NewClient("sock").ensurePort(
				context.Background(),
				"u_enp3s0f0v0",
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
				cmd: "grcli --err-exit --json --socket sock interface add port u_enp3s0f0v0 devargs 0000:03:02.0 rxqs 4 qsize 1024 down",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(
				context.Background(),
				"u_enp3s0f0v0",
				opts,
			),
		)
	})

	t.Run("recreates the port when devargs change without options", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				output: `{"name":"u_enp3s0f0v0","type":"port",` +
					`"devargs":"net_tap0,remote=enp3s0f0v0,iface=tap_enp3s0f0v0"}`,
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface del u_enp3s0f0v0",
			},
			cmdCall{
				cmd: "grcli --err-exit --json --socket sock interface add port u_enp3s0f0v0 devargs 0000:03:02.0 down",
			})()

		assert.NoError(t,
			NewClient("sock").ensurePort(context.Background(), "u_enp3s0f0v0", portOptions{Devargs: "0000:03:02.0"}))
	})
}

func TestGetInterfaceInfoParsesPortDetails(t *testing.T) {
	t.Run("parses port details", func(t *testing.T) {
		defer mockCmdExec(
			cmdCall{
				cmd:    "grcli --err-exit --json --socket sock interface show name u_enp3s0f0v0",
				output: interfaceShowUnderlayPortOutput,
			})()

		details, err := NewClient("sock").getInterfaceInfo(context.Background(), "u_enp3s0f0v0")
		assert.NoError(t, err)
		assert.Equal(t, "u_enp3s0f0v0", details.Name)
		assert.Equal(t, "port", details.Type)
		assert.Equal(t, "0000:03:02.0", details.Devargs)
		assert.Equal(t, "aa:bb:cc:dd:ee:ff", details.MAC)
		assert.Equal(t, int32(4), details.NRxq)
		assert.Equal(t, int32(1024), details.RxqSize)
	})
}

func TestMatchesRequested(t *testing.T) {
	rxqs := int32(4)
	qsize := int32(1024)
	matching := groutInterface{
		Devargs: "0000:03:02.0",
		MAC:     "aa:bb:cc:dd:ee:ff",
		NRxq:    4,
		RxqSize: 1024,
		MTU:     9500,
	}
	opts := portOptions{
		Devargs:  "0000:03:02.0",
		RXQueues: &rxqs,
		QSize:    &qsize,
		MTU:      9500,
	}

	t.Run("matching options", func(t *testing.T) {
		assert.True(t, matching.matchesRequested(opts))
	})

	t.Run("mismatching rx queues", func(t *testing.T) {
		details := matching
		details.NRxq = 1
		assert.False(t, details.matchesRequested(opts))
	})

	t.Run("mismatching queue size", func(t *testing.T) {
		details := matching
		details.RxqSize = 256
		assert.False(t, details.matchesRequested(opts))
	})

	t.Run("mismatching mtu", func(t *testing.T) {
		details := matching
		details.MTU = 1500
		assert.False(t, details.matchesRequested(opts))
	})

	t.Run("mismatching devargs", func(t *testing.T) {
		requested := opts
		requested.Devargs = "0000:ff:00.0"
		assert.False(t, matching.matchesRequested(requested))
	})

	t.Run("mismatching vrf", func(t *testing.T) {
		details := matching
		details.VRF = "blue"
		requested := opts
		requested.VRF = "red"
		assert.False(t, details.matchesRequested(requested))
	})

	t.Run("unspecified fields are ignored", func(t *testing.T) {
		assert.True(t, matching.matchesRequested(portOptions{Devargs: "0000:03:02.0"}))
	})

	t.Run("tap devargs with random suffix still match", func(t *testing.T) {
		tap := groutInterface{
			Devargs: "net_tapabc123,remote=eth0,iface=tap_eth0",
		}
		assert.True(t, tap.matchesRequested(portOptions{Devargs: "net_tapxyz789,remote=eth0,iface=tap_eth0"}))
		assert.False(t, tap.matchesRequested(portOptions{Devargs: "net_tapxyz789,remote=eth1,iface=tap_eth1"}))
	})

	t.Run("tap vs pci is a mismatch", func(t *testing.T) {
		requested := opts
		requested.Devargs = "net_tap0,remote=eth0,iface=tap_eth0"
		assert.False(t, matching.matchesRequested(requested))
	})
}

func TestDevargsMatch(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		existing  string
		want      bool
	}{
		{
			name:      "identical pci",
			requested: "0000:03:02.0",
			existing:  "0000:03:02.0",
			want:      true,
		},
		{
			name:      "different pci",
			requested: "0000:03:02.0",
			existing:  "0000:ff:00.0",
			want:      false,
		},
		{
			name:      "taps with different suffix and same arguments",
			requested: "net_tapabc123,remote=eth0,iface=tap_eth0",
			existing:  "net_tapxyz789,remote=eth0,iface=tap_eth0",
			want:      true,
		},
		{
			name:      "taps with different arguments",
			requested: "net_tapabc123,remote=eth0,iface=tap_eth0",
			existing:  "net_tapabc123,remote=eth1,iface=tap_eth1",
			want:      false,
		},
		{
			name:      "tap with arguments vs pci",
			requested: "net_tap0,remote=eth0,iface=tap_eth0",
			existing:  "0000:03:02.0",
			want:      false,
		},
		{
			// Both sides have empty arguments, so comparing arguments
			// instead of the whole string would wrongly report a match.
			name:      "tap without arguments vs pci",
			requested: "net_tap0",
			existing:  "0000:03:02.0",
			want:      false,
		},
		{
			name:      "pci vs tap without arguments",
			requested: "0000:03:02.0",
			existing:  "net_tap0",
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, devargsMatch(tc.requested, tc.existing))
		})
	}
}
