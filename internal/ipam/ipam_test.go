// SPDX-License-Identifier:Apache-2.0

package ipam

import (
	"net"
	"testing"
)

func TestSliceCIDR(t *testing.T) {
	tests := []struct {
		name        string
		cidr        string
		index       int
		expectedIP1 string
		expectedIP2 string
		shouldFail  bool
	}{
		{
			"first",
			"192.168.1.0/24",
			0,
			"192.168.1.0/24",
			"192.168.1.1/24",
			false,
		},
		{
			"second",
			"192.168.1.0/24",
			1,
			"192.168.1.2/24",
			"192.168.1.3/24",
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, cidr, err := net.ParseCIDR(tc.cidr)
			if err != nil {
				t.Fatalf("failed to parse cidr %s", tc.cidr)
			}
			ips, err := sliceCIDR(cidr, tc.index, 2)
			if err != nil && !tc.shouldFail {
				t.Fatalf("got error %s", err)
			}
			if err == nil && tc.shouldFail {
				t.Fatalf("expected error, did not happen")
			}
			if len(ips) != 2 {
				t.Fatalf("expecting 2 ips, got %v", ips)
			}
			ip1, ip2 := ips[0], ips[1]
			if ip1.String() != tc.expectedIP1 {
				t.Fatalf("expecting %s got %s", tc.expectedIP1, ip1.String())
			}
			if ip2.String() != tc.expectedIP2 {
				t.Fatalf("expecting %s got %s", tc.expectedIP2, ip2.String())
			}

		})
	}
}

func TestVethIPsFromPool(t *testing.T) {
	tests := []struct {
		name             string
		poolIPv4         string
		poolIPv6         string
		index            int
		expectedPEIPv4   string
		expectedHostIPv4 string
		expectedPEIPv6   string
		expectedHostIPv6 string
		shouldFail       bool
	}{
		{
			"ipv4_only",
			"192.168.1.0/24",
			"",
			0,
			"192.168.1.1/24",
			"192.168.1.2/24",
			"",
			"",
			false,
		},
		{
			"ipv6_only",
			"",
			"2001:db8::/64",
			0,
			"",
			"",
			"2001:db8::1/64",
			"2001:db8::2/64",
			false,
		},
		{
			"dual_stack",
			"192.168.1.0/24",
			"2001:db8::/64",
			0,
			"192.168.1.1/24",
			"192.168.1.2/24",
			"2001:db8::1/64",
			"2001:db8::2/64",
			false,
		},
		{
			"ipv4_not_ending_in_zero",
			"192.168.1.1/24",
			"",
			0,
			"192.168.1.1/24",
			"192.168.1.2/24",
			"",
			"",
			false,
		},
		{
			"ipv6_not_ending_in_zero",
			"",
			"2001:db8::1/64",
			0,
			"",
			"",
			"2001:db8::1/64",
			"2001:db8::2/64",
			false,
		},
		{
			"no_pools",
			"",
			"",
			0,
			"",
			"",
			"",
			"",
			true,
		},
		{
			"invalid_ipv4",
			"invalid",
			"2001:db8::/64",
			0,
			"",
			"",
			"",
			"",
			true,
		},
		{
			"invalid_ipv6",
			"192.168.1.0/24",
			"invalid",
			0,
			"",
			"",
			"",
			"",
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := VethIPsFromPool(tc.poolIPv4, tc.poolIPv6, tc.index)
			if err != nil && !tc.shouldFail {
				t.Fatalf("got error %v while should not fail", err)
			}
			if err == nil && tc.shouldFail {
				t.Fatalf("was expecting error, didn't fail")
			}

			if tc.poolIPv4 != "" && !tc.shouldFail {
				if res.Ipv4.HostSide.String() != tc.expectedHostIPv4 {
					t.Fatalf("was expecting %s, got %s on the host IPv4", tc.expectedHostIPv4, res.Ipv4.HostSide.String())
				}
				if res.Ipv4.PeSide.String() != tc.expectedPEIPv4 {
					t.Fatalf("was expecting %s, got %s on the container IPv4", tc.expectedPEIPv4, res.Ipv4.PeSide.String())
				}
			}

			if tc.poolIPv6 != "" && !tc.shouldFail {
				if res.Ipv6.HostSide.String() != tc.expectedHostIPv6 {
					t.Fatalf("was expecting %s, got %s on the host IPv6", tc.expectedHostIPv6, res.Ipv6.HostSide.String())
				}
				if res.Ipv6.PeSide.String() != tc.expectedPEIPv6 {
					t.Fatalf("was expecting %s, got %s on the container IPv6", tc.expectedPEIPv6, res.Ipv6.PeSide.String())
				}
			}
		})
	}
}

func TestVethIPsForFamily(t *testing.T) {
	tests := []struct {
		name         string
		pool         string
		index        int
		expectedPE   string
		expectedHost string
		shouldFail   bool
	}{
		{
			"ipv4_first_ending_in_zero",
			"192.168.1.0/24",
			0,
			"192.168.1.1/24",
			"192.168.1.2/24",
			false,
		},
		{
			"ipv4_second_ending_in_zero",
			"192.168.1.0/24",
			1,
			"192.168.1.1/24",
			"192.168.1.3/24",
			false,
		},
		{
			"ipv4_first_not_ending_in_zero",
			"192.168.1.1/24",
			0,
			"192.168.1.1/24",
			"192.168.1.2/24",
			false,
		},
		{
			"ipv4_second_not_ending_in_zero",
			"192.168.1.1/24",
			1,
			"192.168.1.1/24",
			"192.168.1.3/24",
			false,
		},
		{
			"ipv6_first_ending_in_zero",
			"2001:db8::/64",
			0,
			"2001:db8::1/64",
			"2001:db8::2/64",
			false,
		},
		{
			"ipv6_second_ending_in_zero",
			"2001:db8::/64",
			1,
			"2001:db8::1/64",
			"2001:db8::3/64",
			false,
		},
		{
			"ipv6_first_not_ending_in_zero",
			"2001:db8::1/64",
			0,
			"2001:db8::1/64",
			"2001:db8::2/64",
			false,
		},
		{
			"ipv6_second_not_ending_in_zero",
			"2001:db8::1/64",
			1,
			"2001:db8::1/64",
			"2001:db8::3/64",
			false,
		},
		{
			"invalid_pool",
			"invalid",
			0,
			"",
			"",
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := vethIPsForFamily(tc.pool, tc.index)
			if err != nil && !tc.shouldFail {
				t.Fatalf("got error %v while should not fail", err)
			}
			if err == nil && tc.shouldFail {
				t.Fatalf("was expecting error, didn't fail")
			}

			if !tc.shouldFail {
				if res.HostSide.String() != tc.expectedHost {
					t.Fatalf("was expecting %s, got %s on the host", tc.expectedHost, res.HostSide.String())
				}
				if res.PeSide.String() != tc.expectedPE {
					t.Fatalf("was expecting %s, got %s on the container", tc.expectedPE, res.PeSide.String())
				}
			}
		})
	}
}

func TestVTEPIP(t *testing.T) {
	tests := []struct {
		name           string
		pool           string
		index          int
		expectedVTEPIP string
		shouldFail     bool
	}{
		{
			"first",
			"192.168.1.0/24",
			0,
			"192.168.1.0/32",
			false,
		}, {
			"second",
			"192.168.1.0/24",
			1,
			"192.168.1.1/32",
			false,
		}, {
			"invalid",
			"hellothisisnotanip",
			0,
			"",
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := VTEPIp(tc.pool, tc.index)
			if err != nil && !tc.shouldFail {
				t.Fatalf("got error %v while should not fail", err)
			}
			if err == nil && tc.shouldFail {
				t.Fatalf("was expecting error, didn't fail")
			}

			if !tc.shouldFail && res.String() != tc.expectedVTEPIP {
				t.Fatalf("was expecting %s, got %s on the VTEPIP", tc.expectedVTEPIP, res.String())
			}
		})
	}

}

func TestLocator(t *testing.T) {
	tests := []struct {
		name       string
		pool       string
		index      int
		expected   string
		shouldFail bool
	}{
		// /64 tests - increment subnet ID (3rd 16-bit group)
		{
			name:       "IPv6 /64 index 0",
			pool:       "fd00:0:0::/64",
			index:      0,
			expected:   "fd00:0:0::/64",
			shouldFail: false,
		},
		{
			name:       "IPv6 /64 index 1",
			pool:       "fd00:0:0::/64",
			index:      1,
			expected:   "fd00:0:1::/64",
			shouldFail: false,
		},
		{
			name:       "IPv6 /64 index 2",
			pool:       "fd00:0:0::/64",
			index:      2,
			expected:   "fd00:0:2::/64",
			shouldFail: false,
		},
		{
			name:       "IPv6 /64 index 255",
			pool:       "fd00:0:0::/64",
			index:      255,
			expected:   "fd00:0:ff::/64",
			shouldFail: false,
		},
		{
			name:       "IPv6 /64 index 256",
			pool:       "fd00:0:0::/64",
			index:      256,
			expected:   "fd00:0:100::/64",
			shouldFail: false,
		},
		{
			name:       "IPv6 /64 index 65536",
			pool:       "fd00:0:0::/64",
			index:      65536,
			expected:   "fd00:1:0::/64",
			shouldFail: false,
		},
		// /48 tests - increment 4th 16-bit group
		{
			name:       "IPv6 /48 index 0",
			pool:       "fd00:0:0::/48",
			index:      0,
			expected:   "fd00:0:0::/48",
			shouldFail: false,
		},
		{
			name:       "IPv6 /48 index 1",
			pool:       "fd00:0:0::/48",
			index:      1,
			expected:   "fd00:0:0:1::/48",
			shouldFail: false,
		},
		{
			name:       "IPv6 /48 index 256",
			pool:       "fd00:0:0::/48",
			index:      256,
			expected:   "fd00:0:0:100::/48",
			shouldFail: false,
		},
		// /96 tests - increment 7th 16-bit group
		{
			name:       "IPv6 /96 index 0",
			pool:       "fd00:0:0:0:0:0::/96",
			index:      0,
			expected:   "fd00:0:0:0:0:0::/96",
			shouldFail: false,
		},
		{
			name:       "IPv6 /96 index 1",
			pool:       "fd00:0:0:0:0:0::/96",
			index:      1,
			expected:   "fd00:0:0:0:0:1::/96",
			shouldFail: false,
		},
		{
			name:       "IPv6 /96 index 256",
			pool:       "fd00:0:0:0:0:0::/96",
			index:      256,
			expected:   "fd00:0:0:0:0:100::/96",
			shouldFail: false,
		},
		// Error cases
		{
			name:       "IPv4 not supported",
			pool:       "192.168.0.0/24",
			index:      1,
			expected:   "",
			shouldFail: true,
		},
		{
			name:       "invalid CIDR",
			pool:       "invalid",
			index:      1,
			expected:   "",
			shouldFail: true,
		},
		{
			name:       "IPv6 /32 not supported",
			pool:       "fd00::/32",
			index:      1,
			expected:   "",
			shouldFail: true,
		},
		{
			name:       "IPv6 /56 not supported",
			pool:       "fd00:0:0::/56",
			index:      1,
			expected:   "",
			shouldFail: true,
		},
		{
			name:       "IPv6 /128 not supported",
			pool:       "fd00::1/128",
			index:      1,
			expected:   "",
			shouldFail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Locator(tc.pool, tc.index)

			if tc.shouldFail {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Parse both expected and actual to compare as IP networks
			expectedIP, expectedNet, err := net.ParseCIDR(tc.expected)
			if err != nil {
				t.Fatalf("failed to parse expected CIDR %s: %v", tc.expected, err)
			}

			// Compare the IP addresses and masks
			if !result.IP.Equal(expectedIP) || result.Mask.String() != expectedNet.Mask.String() {
				t.Fatalf("expected %s, got %s", tc.expected, result.String())
			}
		})
	}
}
