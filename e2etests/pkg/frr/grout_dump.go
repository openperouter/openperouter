// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

func GroutDump(exec executor.Executor) (string, error) {
	out, err := exec.Exec("grcli", "--version")
	if err != nil {
		return "grcli not available", nil
	}

	res := "Grout version:\n" + out + "\n\n"
	allerrs := errors.New("")

	commands := []struct {
		desc string
		cmd  []string
	}{
		{"grcli interface show", []string{"grcli", "interface", "show"}},
		{"grcli address show", []string{"grcli", "address", "show"}},
		{"grcli route show", []string{"grcli", "route", "show"}},
		{"grcli nexthop show", []string{"grcli", "nexthop", "show"}},
		{"grcli stats show", []string{"grcli", "stats", "show"}},
		{"tc filter show", []string{"tc", "filter", "show"}},
		{"tc qdisc show", []string{"tc", "qdisc", "show"}},
	}

	for _, c := range commands {
		res += fmt.Sprintf("\n######## %s\n\n", c.desc)
		out, err := exec.Exec(c.cmd[0], c.cmd[1:]...)
		if err != nil {
			allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec %q: %v", strings.Join(c.cmd, " "), err))
		}
		res += out
	}

	if allerrs.Error() == "" {
		allerrs = nil
	}

	return res, allerrs
}
