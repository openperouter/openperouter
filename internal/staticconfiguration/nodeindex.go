// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/static"
)

func ValidateNodeIndex(n static.NodeIndex) error {
	if n.Index != 0 && n.InterfaceName != "" {
		return fmt.Errorf(
			"index and interfaceName are mutually exclusive, got index %d and interface %q",
			n.Index, n.InterfaceName)
	}
	return nil
}

func NodeIndexFromInterface(name string) (int, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("failed to find interface %s: %w", name, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return 0, fmt.Errorf("failed to get addresses for interface %s: %w", name, err)
	}

	var v6Fallback *net.IPNet
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP.To4() != nil {
			return hostPartFromIPNet(ipNet), nil
		}
		if v6Fallback == nil {
			v6Fallback = ipNet
		}
	}

	if v6Fallback != nil {
		return hostPartFromIPNet(v6Fallback), nil
	}

	return 0, fmt.Errorf("no IP address found on interface %s", name)
}

func hostPartFromIPNet(ipNet *net.IPNet) int {
	ip := ipNet.IP
	mask := ipNet.Mask

	if ip4 := ip.To4(); ip4 != nil {
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

	if len(ip) != net.IPv6len || len(mask) != net.IPv6len {
		return 0
	}
	hostPart := 0
	for i := range net.IPv6len {
		hostPart = (hostPart << 8) | int(ip[i] & ^mask[i])
	}
	return hostPart
}
