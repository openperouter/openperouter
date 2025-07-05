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
			"192.168.1.0/24",
			"192.168.1.1/24",
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
			"2001:db8::/64",
			"2001:db8::1/64",
			false,
		},
		{
			"dual_stack",
			"192.168.1.0/24",
			"2001:db8::/64",
			0,
			"192.168.1.0/24",
			"192.168.1.1/24",
			"2001:db8::/64",
			"2001:db8::1/64",
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
			"ipv4_first",
			"192.168.1.0/24",
			0,
			"192.168.1.0/24",
			"192.168.1.1/24",
			false,
		},
		{
			"ipv4_second",
			"192.168.1.0/24",
			1,
			"192.168.1.0/24",
			"192.168.1.2/24",
			false,
		},
		{
			"ipv6_first",
			"2001:db8::/64",
			0,
			"2001:db8::/64",
			"2001:db8::1/64",
			false,
		},
		{
			"ipv6_second",
			"2001:db8::/64",
			1,
			"2001:db8::/64",
			"2001:db8::2/64",
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
