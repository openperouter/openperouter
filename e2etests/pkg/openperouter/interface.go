// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"regexp"
	"sort"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

// IsInterfaceInNS checks whether the interface exists in the given
// network namespace on nodeName. Pass NamedNetns for the perouter netns,
// or an empty string for the default netns.
func IsInterfaceInNS(nodeName, intf string, ns string) bool {
	exec := executor.ForContainer(nodeName)
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
	exec := executor.ForContainer(nodeName)
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

// InterfaceIsUp checks whether the interface in the default netns
// on nodeName has state UP.
func InterfaceIsUp(nodeName, intf string) bool {
	exec := executor.ForContainer(nodeName)
	out, err := exec.Exec("ip", "link", "show", intf, "up")
	if err != nil {
		return false
	}
	return out != ""
}
