// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"context"
	"os/exec"
	"time"
)

// vtyshTimeout is the maximum time to wait for a vtysh command to complete.
// Under heavy load (e.g. hundreds of VRFs), FRR daemons can be slow to respond
// to vtysh IPC, so we use a generous timeout to avoid blocking indefinitely.
const vtyshTimeout = 10 * time.Second

type Cli func(args string) (string, error)

func Run(args string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), vtyshTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/vtysh", "-c", args).CombinedOutput()
	return string(out), err
}

var _ Cli = Run
