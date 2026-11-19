// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"strings"
	"testing"
)

func TestEVPNDoesNotRedistributeDefaultGuards(t *testing.T) {
	peerASN, err := NewPeerASN(new(int64(64513)), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		neighbor *NeighborConfig
	}{
		{name: "network announcements"},
		{name: "BGP host session", neighbor: &NeighborConfig{
			ID: "192.0.2.2", ASN: peerASN,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := &Config{
				Underlay: UnderlayConfig{MyASN: 64512, RouterID: "192.0.2.1"},
				L3VNIs: []L3VNIConfig{{
					VRF: "red", VNI: 100, ASN: 64512, RouterID: "192.0.2.1", LocalNeighbor: tc.neighbor,
					ToAdvertiseIPv4: []string{"198.51.100.0/24"},
					ToAdvertiseIPv6: []string{"2001:db8:100::/64"},
				}},
			}
			got, err := templateConfig(config)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{
				"redistribute kernel", "redistribute static", "Null0", "4278198272",
				"network 0.0.0.0/0", "network ::/0",
			} {
				if strings.Contains(got, forbidden) {
					t.Errorf("generated config can originate a guard: %q", forbidden)
				}
			}
			for _, wanted := range []string{
				"network 198.51.100.0/24", "network 2001:db8:100::/64",
				"advertise ipv4 unicast", "advertise ipv6 unicast",
			} {
				if !strings.Contains(got, wanted) {
					t.Errorf("legitimate announcement missing: %q", wanted)
				}
			}
		})
	}
}
