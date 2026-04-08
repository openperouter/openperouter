// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"os/exec"
)

const DefaultVtyshPath = "/usr/bin/vtysh"

type Cli func(args string) (string, error)

func NewCli(path string) Cli {
	return func(args string) (string, error) {
		out, err := exec.Command(path, "-c", args).CombinedOutput()
		return string(out), err
	}
}
