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
	return func(args string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "/usr/bin/vtysh", "-c", args).CombinedOutput()
		return string(out), err
	}
}

func NewCLI(args string) (string, error) {
	return NewCLIWithTimeout(DefaultTimeout)(args)
}

var _ Cli = NewCLI
