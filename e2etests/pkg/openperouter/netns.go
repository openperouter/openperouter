// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

const namedNetns = "perouter"

// NamedNetnsExists checks whether /var/run/netns/perouter is present on nodeName.
func NamedNetnsExists(nodeName string) (bool, error) {
	exec := executor.ForContainer(nodeName)
	out, err := exec.Exec("ip", "netns", "list")
	if err != nil {
		return false, err
	}
	// Each line of "ip netns list" is "<name>" or "<name> (id: N)".
	// Use exact name comparison to avoid "perouter" matching inside "openperouter".
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == namedNetns {
			return true, nil
		}
	}
	return false, nil
}

// NamedNetnsHasInterfaceType checks whether the named netns contains at least one
// interface of the given link type (e.g. "vrf", "bridge", "vxlan").
func NamedNetnsHasInterfaceType(nodeName, linkType string) (bool, error) {
	exec := executor.ForContainer(nodeName)
	out, err := exec.Exec("ip", "netns", "exec", namedNetns, "ip", "link", "show", "type", linkType)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// DeleteNamedNetns runs "ip netns delete perouter" on nodeName.
func DeleteNamedNetns(nodeName string) error {
	exec := executor.ForContainer(nodeName)
	_, err := exec.Exec("ip", "netns", "delete", namedNetns)
	return err
}
