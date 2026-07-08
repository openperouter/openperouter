// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"fmt"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

func GroutDump(exec executor.Executor) string {
	out, err := exec.Exec("grcli", "--version")
	if err != nil {
		return "grcli not available"
	}

	var res strings.Builder
	res.WriteString("Grout version:\n" + out + "\n\n")

	commands := []command{
		{desc: "grcli interface show", cmd: []string{"grcli", "interface", "show"}},
		{desc: "grcli address show", cmd: []string{"grcli", "address", "show"}},
		{desc: "grcli route show", cmd: []string{"grcli", "route", "show"}},
		{desc: "grcli nexthop show", cmd: []string{"grcli", "nexthop", "show"}},
		{desc: "grcli stats show", cmd: []string{"grcli", "stats", "show"}},
		{desc: "tc filter show", cmd: []string{"tc", "filter", "show"}},
		{desc: "tc qdisc show", cmd: []string{"tc", "qdisc", "show"}},
	}

	for _, c := range commands {
		out, err := exec.Exec(c.cmd[0], c.cmd[1:]...)
		if err != nil && c.skipLogOnError {
			continue
		}
		fmt.Fprintf(&res, "\n######## %s\n\n", c.desc)
		if err != nil {
			fmt.Fprintf(&res, "\nFailed exec %q: %v", strings.Join(c.cmd, " "), err)
		}
		res.WriteString(out)
	}

	return res.String()
}
