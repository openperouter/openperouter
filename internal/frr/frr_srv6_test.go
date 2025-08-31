// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"testing"

	"github.com/openperouter/openperouter/internal/ipfamily"
)

func TestBasicSRV6(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
		Underlay: UnderlayConfig{
			MyASN:    64512,
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:      64512,
					Addr:     "2001:db8::2",
					IPFamily: ipfamily.IPv6,
				},
			},
			SRV6: &UnderlaySrv6{
				Locator: "fd00:0:1::/64",
			},
		},
		VRFs: []VRFConfig{
			{
				VRF:      "red",
				ASN:      64512,
				RouterID: "10.0.0.1",
				SRV6:     &VRFSrv6{AssignedNumber: 101},
				LocalNeighbor: &NeighborConfig{
					ASN:      64512,
					Addr:     "192.168.1.2",
					IPFamily: ipfamily.IPv4,
				},
				ToAdvertiseIPv4: []string{
					"192.169.10.2/24",
				},
			},
		},
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}
