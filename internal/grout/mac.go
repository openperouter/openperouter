// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"crypto/rand"
	"net"
)

func randomMAC() (net.HardwareAddr, error) {
	mac := make(net.HardwareAddr, 6)
	if _, err := rand.Read(mac); err != nil {
		return nil, err
	}

	mac[0] = (mac[0] | 0x02) & 0xfe // locally administered, unicast
	return mac, nil
}
