// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

// IsInterfaceInNS checks whether the interface exists in the given
// network namespace on nodeName. Pass NamedNetns for the perouter netns,
// or an empty string for the default netns.
func IsInterfaceInNS(nodeName, intf string, ns string) bool {
	exec := executor.ForNode(nodeName)
	if ns == "" {
		_, err := exec.Exec("ip", "link", "show", intf)
		return err == nil
	}
	_, err := exec.Exec("ip", "netns", "exec", ns, "ip", "link", "show", intf)
	return err == nil
}

// IsInterfaceInDefaultNetns checks whether the interface exists
// in the default network namespace on nodeName.
func IsInterfaceInDefaultNetns(nodeName, intf string) bool {
	return IsInterfaceInNS(nodeName, intf, "")
}

var addrRegexp = regexp.MustCompile(`\s(inet6?\s+\S+)`)

// InterfaceIPAddresses returns the non-link-local IP addresses assigned to the
// interface in the default netns on nodeName, sorted and newline-joined.
func InterfaceIPAddresses(nodeName, intf string) (string, error) {
	exec := executor.ForNode(nodeName)
	out, err := exec.Exec("ip", "-o", "a", "ls", "dev", intf, "scope", "global")
	if err != nil {
		return "", err
	}
	var addrs []string
	for line := range strings.SplitSeq(out, "\n") {
		m := addrRegexp.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		addr := m[1]
		addrs = append(addrs, addr)
	}
	sort.Strings(addrs)
	return strings.Join(addrs, "\n"), nil
}

// InterfaceIPv4InNetns returns the first global-scope IPv4 address (without
// prefix length) assigned to intf inside the named netns on nodeName, or an
// error if none is found.
func InterfaceIPv4InNetns(nodeName, intf, ns string) (string, error) {
	exec := executor.ForNode(nodeName)
	out, err := exec.Exec("ip", "netns", "exec", ns, "ip", "-4", "-o", "a", "ls", "dev", intf, "scope", "global")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(out, "\n") {
		m := addrRegexp.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		// m[1] is e.g. "inet 192.168.11.100/24"; extract the bare IP.
		fields := strings.Fields(m[1])
		if len(fields) < 2 {
			continue
		}
		ip, _, _ := strings.Cut(fields[1], "/")
		return ip, nil
	}
	return "", fmt.Errorf("no global IPv4 address on %s/%s in netns %s", nodeName, intf, ns)
}

// NetnsLinkLocalOwners returns, for the given netns on nodeName, a map from
// each IPv6 link-local (fe80::/10) address to the interfaces that carry it.
// A correct setup has a single owner per address; a link-local shared across
// interfaces (the issue #720 failure signature, caused by a duplicate MAC)
// shows up as more than one owner.
func NetnsLinkLocalOwners(nodeName, ns string) (map[string][]string, error) {
	exec := executor.ForNode(nodeName)
	out, err := exec.Exec("ip", "netns", "exec", ns, "ip", "-o", "-6", "addr", "show", "scope", "link")
	if err != nil {
		return nil, fmt.Errorf("listing link-local addresses in netns %s on %s: %w", ns, nodeName, err)
	}

	owners := map[string][]string{}
	for line := range strings.SplitSeq(out, "\n") {
		// e.g. "36: u_toswitch1    inet6 fe80::a8c1:abff:fe84:5cc8/128 scope link nodad"
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet6" {
			continue
		}
		iface := fields[1]
		addr, _, _ := strings.Cut(fields[3], "/")
		owners[addr] = append(owners[addr], iface)
	}
	return owners, nil
}

// InterfaceIsUp checks whether the interface in the default netns
// on nodeName has state UP.
func InterfaceIsUp(nodeName, intf string) bool {
	exec := executor.ForNode(nodeName)
	out, err := exec.Exec("ip", "link", "show", intf, "up")
	if err != nil {
		return false
	}
	return out != ""
}
