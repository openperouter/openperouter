// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"fmt"
	"net"
)

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
