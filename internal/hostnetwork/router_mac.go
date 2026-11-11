// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// RouterMACForNode fills the host bits of a MAC prefix with the node index.
// An empty prefix preserves the existing bridge MAC behavior.
func RouterMACForNode(prefix string, nodeIndex int) (net.HardwareAddr, error) {
	if prefix == "" {
		return nil, nil
	}
	address, length, ok := strings.Cut(prefix, "/")
	if !ok {
		return nil, fmt.Errorf("router MAC prefix %q must include a prefix length", prefix)
	}
	if address == "" {
		return nil, fmt.Errorf("router MAC prefix %q is missing a MAC address", prefix)
	}
	mac, err := ParseRouterMAC(address)
	if err != nil {
		return nil, err
	}
	bits, err := strconv.Atoi(length)
	if err != nil || bits < 8 || bits > 47 {
		return nil, fmt.Errorf("router MAC prefix %q must have a length between 8 and 47", prefix)
	}
	var base uint64
	for _, b := range mac {
		base = base<<8 | uint64(b)
	}
	hostBits := 48 - bits
	hostMask := uint64(1)<<hostBits - 1
	if base&hostMask != 0 {
		return nil, fmt.Errorf("router MAC prefix %q has nonzero host bits", prefix)
	}
	if nodeIndex < 0 || uint64(nodeIndex) > hostMask {
		return nil, fmt.Errorf("node index %d does not fit router MAC prefix %q", nodeIndex, prefix)
	}
	value := base | uint64(nodeIndex)
	for i := len(mac) - 1; i >= 0; i-- {
		mac[i] = byte(value)
		value >>= 8
	}
	return mac, nil
}

// ParseRouterMAC parses an optional nonzero Ethernet unicast router MAC.
// An empty value leaves the bridge MAC unchanged.
func ParseRouterMAC(value string) (net.HardwareAddr, error) {
	if value == "" {
		return nil, nil
	}
	mac, err := net.ParseMAC(value)
	if err != nil {
		return nil, fmt.Errorf("invalid router MAC %q: %w", value, err)
	}
	if len(mac) != 6 || mac[0]&1 != 0 || bytes.Equal(mac, make([]byte, 6)) {
		return nil, fmt.Errorf("router MAC %q must be a nonzero 6-byte unicast address", value)
	}
	return mac, nil
}
