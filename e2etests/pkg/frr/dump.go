// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

func RawDump(exec executor.Executor) (string, error) {
	res := ""
	allerrs := errors.New("")

	commands := []struct {
		desc string
		cmd  []string
	}{
		{"Show version", []string{"vtysh", "-c", "show version"}},
		{"Show running config", []string{"vtysh", "-c", "show running-config"}},
		{"BGP Summary", []string{"vtysh", "-c", "show bgp vrf all summary"}},
		{"BGP Neighbors", []string{"vtysh", "-c", "show bgp vrf all neighbor"}},
		{"RIB ipv4", []string{"vtysh", "-c", "show ip route"}},
		{"RIB ipv6", []string{"vtysh", "-c", "show ipv6 route"}},
		{"BGP route table ipv4", []string{"vtysh", "-c", "show bgp vrf all ipv4"}},
		{"BGP route table ipv6", []string{"vtysh", "-c", "show bgp vrf all ipv6"}},
		{"EVPN Routes", []string{"vtysh", "-c", "show bgp l2vpn evpn"}},
		{"Zebra interface information", []string{"vtysh", "-c", "show interface"}},
		{"ip link", []string{"bash", "-c", "ip l"}},
		{"ip address", []string{"bash", "-c", "ip address"}},
		{"ip neigh", []string{"bash", "-c", "ip neigh"}},
		{"Detailed interface statistics", []string{"bash", "-c", "ip -s -s link ls"}},
		{"ip vrf", []string{"bash", "-c", "ip vrf"}},
		{"ip route table all", []string{"bash", "-c", "ip route show table all"}},
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
