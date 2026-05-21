// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

type command struct {
	desc     string
	cmd      []string
	optional bool // optional = true means that we will not log anything (not even the failure) in case of an error.
}

func RawDump(exec executor.Executor) string {
	var res strings.Builder

	commands := []command{
		{desc: "Show version", cmd: []string{"vtysh", "-c", "show version"}},
		{desc: "Show running config", cmd: []string{"vtysh", "-c", "show running-config"}},
		{desc: "BGP Summary", cmd: []string{"vtysh", "-c", "show bgp vrf all summary"}},
		{desc: "BGP Neighbors", cmd: []string{"vtysh", "-c", "show bgp vrf all neighbor"}},
		{desc: "RIB ipv4", cmd: []string{"vtysh", "-c", "show ip route"}},
		{desc: "RIB ipv6", cmd: []string{"vtysh", "-c", "show ipv6 route"}},
		{desc: "BGP route table ipv4", cmd: []string{"vtysh", "-c", "show bgp vrf all ipv4"}},
		{desc: "BGP route table ipv6", cmd: []string{"vtysh", "-c", "show bgp vrf all ipv6"}},
		{desc: "EVPN Routes", cmd: []string{"vtysh", "-c", "show bgp l2vpn evpn"}},
		{desc: "Zebra interface information", cmd: []string{"vtysh", "-c", "show interface"}},
		{desc: "ip link", cmd: []string{"bash", "-c", "ip l"}},
		{desc: "ip address", cmd: []string{"bash", "-c", "ip address"}},
		{desc: "ip neigh", cmd: []string{"bash", "-c", "ip neigh"}},
		{desc: "Detailed interface statistics", cmd: []string{"bash", "-c", "ip -s -s link ls"}},
		{desc: "ip vrf", cmd: []string{"bash", "-c", "ip vrf"}},
		{desc: "ip route table all", cmd: []string{"bash", "-c", "ip route show table all"}},
		{desc: "bridge fdb show", cmd: []string{"bash", "-c", "bridge fdb show"}},
	}

	neighbors, err := getBGPNeighbors(exec)
	if err == nil {
		perNeighborCommands := []string{
			"show ip bgp neighbors %s advertised-routes",
			"show ip bgp neighbors %s advertised-routes detail",
			"show bgp neighbors %s graceful-restart",
		}
		for _, neighbor := range neighbors {
			for _, perNeighborCommand := range perNeighborCommands {
				cmd := fmt.Sprintf(perNeighborCommand, neighbor)
				commands = append(commands, command{
					desc: cmd, cmd: []string{"vtysh", "-c", cmd},
				})
			}
		}
	}

	// Collect logs from /etc/frr/frr.log. This is for the leaf and leafkind
	// docker containers only and will fail for the router pods.
	commands = append(commands, command{
		"cat /etc/frr/frr.log", []string{"bash", "-c", "cat /etc/frr/frr.log"}, true, // Will only work on leaf/leafkind.
	})

	for _, c := range commands {
		out, err := exec.Exec(c.cmd[0], c.cmd[1:]...)
		if err != nil && c.optional {
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

func getBGPNeighbors(exec executor.Executor) ([]string, error) {
	out, err := exec.Exec("vtysh", "-c", "show ip bgp neighbors json")
	if err != nil {
		return nil, fmt.Errorf("getBGPNeighbors: command failed: %w", err)
	}
	neighbors := map[string]any{}
	if err = json.Unmarshal([]byte(out), &neighbors); err != nil {
		return nil, fmt.Errorf("getBGPNeighbors: unmarchalling failed: out: %s, err: %w", out, err)
	}
	return slices.Collect(maps.Keys(neighbors)), nil
}
