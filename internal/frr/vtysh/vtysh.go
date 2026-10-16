// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"context"
	"os/exec"
	"time"
)

const DefaultTimeout = 10 * time.Second

type Cli func(args string) (string, error)

func NewCLIWithTimeout(timeout time.Duration) Cli {
	return newCLIWithTimeout(timeout, "")
}

// NewDaemonCLIWithTimeout returns a Cli that sends commands to a single FRR daemon.
func NewDaemonCLIWithTimeout(timeout time.Duration, daemon string) Cli {
	return newCLIWithTimeout(timeout, daemon)
}

func newCLIWithTimeout(timeout time.Duration, daemon string) Cli {
	return func(args string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		vtysh := exec.CommandContext(ctx, "/usr/bin/vtysh")
		if daemon != "" {
			vtysh.Args = append(vtysh.Args, "-d", daemon)
		}
		vtysh.Args = append(vtysh.Args, "-c", args)

		out, err := vtysh.CombinedOutput()
		return string(out), err
	}
}
