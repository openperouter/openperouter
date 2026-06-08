// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"net"
	"testing"

	"github.com/openperouter/openperouter/api/static"
)

func TestHostPartFromIPNet(t *testing.T) {
	tests := []struct {
		name     string
		cidr     string
		expected int
	}{
		{
			name:     "/24 host part 80",
			cidr:     "192.168.111.80/24",
			expected: 80,
		},
		{
			name:     "/24 host part 1",
			cidr:     "10.0.0.1/24",
			expected: 1,
		},
		{
			name:     "/24 host part 254",
			cidr:     "172.16.0.254/24",
			expected: 254,
		},
		{
			name:     "/16 host part",
			cidr:     "10.5.3.7/16",
			expected: 3*256 + 7,
		},
		{
			name:     "/28 host part",
			cidr:     "192.168.1.67/28",
			expected: 3,
		},
		{
			name:     "/32 host part is always 0",
			cidr:     "10.0.0.5/32",
			expected: 0,
		},
		{
			name:     "/8 host part",
			cidr:     "10.1.2.3/8",
			expected: 1*65536 + 2*256 + 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, ipNet, err := net.ParseCIDR(tt.cidr)
			if err != nil {
				t.Fatalf("failed to parse CIDR %s: %v", tt.cidr, err)
			}
			ipNet.IP = ip

			result := hostPartFromIPNet(ipNet)
			if result != tt.expected {
				t.Errorf("hostPartFromIPNet(%s) = %d, want %d", tt.cidr, result, tt.expected)
			}
		})
	}

	t.Run("16-byte IPv4 mask", func(t *testing.T) {
		ipNet := &net.IPNet{
			IP:   net.IPv4(192, 168, 1, 42),
			Mask: net.CIDRMask(24, 128)[:16],
		}
		// CIDRMask(24, 128) gives a 16-byte mask; the last 4 bytes are
		// all zeros, so after slicing to [12:] we get 255.255.255.0.
		// However, CIDRMask(24, 128) sets the first 24 bits of 128,
		// which means bytes 0-2 are 0xFF and bytes 3-15 are 0x00.
		// After our fix slices mask[12:] we get [0,0,0,0] → host part is full IP.
		// Instead, simulate a realistic 16-byte mask that iface.Addrs() would return:
		// an IPv4-mapped-in-IPv6 mask where the IPv4 /24 portion sits in the last 4 bytes.
		ipNet.Mask = make(net.IPMask, 16)
		copy(ipNet.Mask[12:], net.CIDRMask(24, 32))

		result := hostPartFromIPNet(ipNet)
		if result != 42 {
			t.Errorf("hostPartFromIPNet with 16-byte mask = %d, want 42", result)
		}
	})
}

func TestResolveNodeIndex(t *testing.T) {
	tests := []struct {
		name        string
		config      *static.NodeConfig
		expected    int
		expectError bool
	}{
		{
			name:     "nodeIndex only",
			config:   &static.NodeConfig{NodeIndex: 42},
			expected: 42,
		},
		{
			name:     "zero nodeIndex",
			config:   &static.NodeConfig{NodeIndex: 0},
			expected: 0,
		},
		{
			name:     "neither set",
			config:   &static.NodeConfig{},
			expected: 0,
		},
		{
			name: "both set is an error",
			config: &static.NodeConfig{
				NodeIndex:              5,
				NodeIndexInterfaceName: "eth0",
			},
			expectError: true,
		},
		{
			name: "interface name resolves from loopback",
			config: &static.NodeConfig{
				NodeIndexInterfaceName: "lo",
			},
			expected: 1, // 127.0.0.1/8 → host part = 1
		},
		{
			name: "interface name with non-existent interface",
			config: &static.NodeConfig{
				NodeIndexInterfaceName: "nonexistent-iface-xyz",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveNodeIndex(tt.config)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("ResolveNodeIndex() = %d, want %d", result, tt.expected)
			}
		})
	}
}
