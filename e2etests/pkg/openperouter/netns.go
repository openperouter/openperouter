// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"log"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

const (
	NamedNetns              = "perouter"
	TunnelEndpointInterface = "vtep-lo"
)

// NamedNetnsExists checks whether /var/run/netns/perouter is present on nodeName.
func NamedNetnsExists(nodeName string) (bool, error) {
	exec := executor.ForNode(nodeName)
	out, err := exec.Exec("ip", "netns", "list")
	if err != nil {
		return false, err
	}
	// Each line of "ip netns list" is "<name>" or "<name> (id: N)".
	// Use exact name comparison to avoid "perouter" matching inside "openperouter".
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == NamedNetns {
			return true, nil
		}
	}
	return false, nil
}

// NamedNetnsHasInterfaceType checks whether the named netns contains at least one
// interface of the given link type (e.g. "vrf", "bridge", "vxlan").
func NamedNetnsHasInterfaceType(nodeName, linkType string) (bool, error) {
	exec := executor.ForNode(nodeName)
	out, err := exec.Exec("ip", "netns", "exec", NamedNetns, "ip", "link", "show", "type", linkType)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// DeleteNamedNetns pre-deletes all non-loopback devices inside the perouter
// netns, then runs "ip netns delete perouter" on nodeName. Pre-deleting
// devices accelerates the kernel's async cleanup_net() from ~28s to <2s.
func DeleteNamedNetns(nodeName string) error {
	e := executor.ForNode(nodeName)

	if err := deleteNetnsDevices(e); err != nil {
		log.Printf("pre-deletion of devices failed for %q, proceeding with netns delete: %v", nodeName, err)
	}

	_, err := e.Exec("ip", "netns", "delete", NamedNetns)
	return err
}

// UnderlayConfigured checks whether the underlay is configured inside
// the perouter netns on nodeName by looking for a global IP address on the
// tunnel endpoint interface (vtep-lo), regardless of the interface state.
func UnderlayConfigured(nodeName string) (bool, error) {
	exists, err := NamedNetnsExists(nodeName)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	exec := executor.ForNode(nodeName)
	out, err := exec.Exec("ip", "netns", "exec", NamedNetns,
		"ip", "-o", "address", "show", "dev", TunnelEndpointInterface, "scope", "global")
	if strings.Contains(out, "does not exist") {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get %s addresses on %s: %w", TunnelEndpointInterface, nodeName, err)
	}
	return strings.TrimSpace(out) != "", nil
}

// InterfacesExists checks whether the underlay interfaces exist on nodeName,
// either in the default netns or inside the perouter netns.
func InterfacesExists(nodeName string, ifaces []string) bool {
	exec := executor.ForNode(nodeName)
	for _, iface := range ifaces {
		if _, err := exec.Exec("ip", "link", "show", iface); err == nil {
			return true
		}
		if _, err := exec.Exec("ip", "netns", "exec", NamedNetns, "ip", "link", "show", iface); err == nil {
			return true
		}
	}
	return false
}

// UnderlaySysctls checks whether the provided sysctl settings are set on nodeName, inside the
// perouter netns.
func UnderlaySysctls(nodeName string, expectedSysctls map[string]string) error {
	exec := executor.ForNode(nodeName)

	for sysctlKey, expected := range expectedSysctls {
		cmd := fmt.Sprintf("ip netns exec %s sysctl %s", NamedNetns, sysctlKey)
		out, err := exec.Exec("/bin/bash", "-c", cmd)
		if err != nil {
			return fmt.Errorf("cmd %q: failed with err: %w", cmd, err)
		}
		s := strings.Split(out, "=")
		if len(s) != 2 {
			return fmt.Errorf("cmd %q: expected valid sysctl but got %q instead", cmd, out)
		}
		if got := strings.TrimSpace(s[1]); got != expected {
			return fmt.Errorf("cmd %q: expected to get value %q but got %q instead", cmd, expected, got)
		}
	}

	return nil
}

// deleteNetnsDevices lists all devices inside the perouter netns and deletes
// them one by one, skipping loopback.
func deleteNetnsDevices(e executor.Executor) error {
	out, err := e.Exec("ip", "netns", "exec", NamedNetns, "ip", "-o", "link", "show")
	if err != nil {
		return err
	}

	for line := range strings.SplitSeq(out, "\n") {
		name, err := ifaceName(line)
		if err != nil {
			log.Printf("could not get interface name from line %v", err)
			continue
		}
		if name == "lo" {
			continue
		}
		if out, err := e.Exec("ip", "netns", "exec", NamedNetns, "ip", "link", "delete", name); err != nil {
			if strings.Contains(out, "Cannot find device") {
				continue
			}
			return fmt.Errorf("failed to delete link %q; output %q; error: %v", name, out, err)
		}
	}
	return nil
}

func ifaceName(line string) (string, error) {
	// ip -o link show format: "N: name[@suffix]: <flags> ..."
	parts := strings.SplitN(line, ": ", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected line from 'ip -o link show': %q", line)
	}
	return strings.SplitN(parts[1], "@", 2)[0], nil
}
