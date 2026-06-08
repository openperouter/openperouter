// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/static"
)

// ResolveNodeIndex returns the node index from the configuration.
// If NodeIndexInterfaceName is set, the index is derived from the
// host portion of the first IPv4 address on that interface.
// NodeIndex and NodeIndexInterfaceName are mutually exclusive.
func ResolveNodeIndex(config *static.NodeConfig) (int, error) {
	if config.NodeIndex != 0 && config.NodeIndexInterfaceName != "" {
		return 0, fmt.Errorf(
			"nodeIndex and nodeIndexInterfaceName are mutually exclusive, got nodeIndex %d and interface %q",
			config.NodeIndex, config.NodeIndexInterfaceName)
	}

	if config.NodeIndexInterfaceName != "" {
		return NodeIndexFromInterface(config.NodeIndexInterfaceName)
	}

	return config.NodeIndex, nil
}

// NodeIndexFromInterface derives a node index from the host portion
// of the first IPv4 address found on the named network interface.
func NodeIndexFromInterface(name string) (int, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("failed to find interface %s: %w", name, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return 0, fmt.Errorf("failed to get addresses for interface %s: %w", name, err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP.To4() == nil {
			continue
		}
		return hostPartFromIPNet(ipNet), nil
	}

	return 0, fmt.Errorf("no IPv4 address found on interface %s", name)
}

func hostPartFromIPNet(ipNet *net.IPNet) int {
	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return 0
	}

	mask := ipNet.Mask
	if len(mask) == 16 {
		mask = mask[12:]
	}
	if len(mask) < 4 {
		return 0
	}

	hostPart := 0
	for i := range 4 {
		hostPart = (hostPart << 8) | int(ip4[i] & ^mask[i])
	}

	return hostPart
}
