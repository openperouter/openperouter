// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"net"
	"testing"

	"github.com/openperouter/openperouter/api/static"
)

func TestNodeIndexValidate(t *testing.T) {
	tests := []struct {
		name        string
		nodeIndex   static.NodeIndex
		expectError bool
	}{
		{
			name:      "index only",
			nodeIndex: static.NodeIndex{Index: 42},
		},
		{
			name:      "interface only",
			nodeIndex: static.NodeIndex{InterfaceName: "eth0"},
		},
		{
			name:      "neither set",
			nodeIndex: static.NodeIndex{},
		},
		{
			name:        "both set is an error",
			nodeIndex:   static.NodeIndex{Index: 5, InterfaceName: "eth0"},
			expectError: true,
		},
		{
			name:      "interface with valid cidr",
			nodeIndex: static.NodeIndex{InterfaceName: "eth0", CIDR: "192.168.1.0/24"},
		},
		{
			name:        "cidr without interface is an error",
			nodeIndex:   static.NodeIndex{CIDR: "192.168.1.0/24"},
			expectError: true,
		},
		{
			name:        "invalid cidr is an error",
			nodeIndex:   static.NodeIndex{InterfaceName: "eth0", CIDR: "not-a-cidr"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNodeIndex(tt.nodeIndex)
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestNodeIndexFromInterface(t *testing.T) {
	loopback := loopbackInterfaceName(t)

	t.Run("loopback resolves to 1", func(t *testing.T) {
		result, err := NodeIndexFromInterface(loopback, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 1 {
			t.Errorf("NodeIndexFromInterface(%s) = %d, want 1", loopback, result)
		}
	})

	t.Run("non-existent interface", func(t *testing.T) {
		_, err := NodeIndexFromInterface("nonexistent-iface-xyz", "")
		if err == nil {
			t.Error("expected error but got none")
		}
	})

	t.Run("loopback with matching cidr", func(t *testing.T) {
		result, err := NodeIndexFromInterface(loopback, "127.0.0.0/8")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 1 {
			t.Errorf("NodeIndexFromInterface(%s, 127.0.0.0/8) = %d, want 1", loopback, result)
		}
	})

	t.Run("loopback with non-matching cidr", func(t *testing.T) {
		_, err := NodeIndexFromInterface(loopback, "10.0.0.0/8")
		if err == nil {
			t.Error("expected error but got none")
		}
	})
}

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
		ipNet.Mask = make(net.IPMask, 16)
		copy(ipNet.Mask[12:], net.CIDRMask(24, 32))

		result := hostPartFromIPNet(ipNet)
		if result != 42 {
			t.Errorf("hostPartFromIPNet with 16-byte mask = %d, want 42", result)
		}
	})

	t.Run("IPv6 /64 host part", func(t *testing.T) {
		ip, ipNet, err := net.ParseCIDR("2001:db8::42/64")
		if err != nil {
			t.Fatalf("failed to parse CIDR: %v", err)
		}
		ipNet.IP = ip

		result := hostPartFromIPNet(ipNet)
		if result != 0x42 {
			t.Errorf("hostPartFromIPNet(2001:db8::42/64) = %d, want %d", result, 0x42)
		}
	})

	t.Run("IPv6 /128 host part is always 0", func(t *testing.T) {
		ip, ipNet, err := net.ParseCIDR("2001:db8::5/128")
		if err != nil {
			t.Fatalf("failed to parse CIDR: %v", err)
		}
		ipNet.IP = ip

		result := hostPartFromIPNet(ipNet)
		if result != 0 {
			t.Errorf("hostPartFromIPNet(2001:db8::5/128) = %d, want 0", result)
		}
	})
}

func loopbackInterfaceName(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"lo", "lo0"} {
		if _, err := net.InterfaceByName(name); err == nil {
			return name
		}
	}
	t.Skip("no loopback interface found")
	return ""
}
