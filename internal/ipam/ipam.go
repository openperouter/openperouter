// SPDX-License-Identifier:Apache-2.0

package ipam

import (
	"fmt"
	"net"

	gocidr "github.com/apparentlymart/go-cidr/cidr"
	"github.com/openperouter/openperouter/internal/ipfamily"
)

type VethIPs struct {
	Ipv4 VethIPsForFamily
	Ipv6 VethIPsForFamily
}

type VethIPsForFamily struct {
	HostSide net.IPNet
	PeSide   net.IPNet
}

// VethIPsFromPool returns the IPs for the host side and the PE side
// for both IPv4 and IPv6 pools on the ith node.
func VethIPsFromPool(poolIPv4, poolIPv6 string, index int) (VethIPs, error) {
	if poolIPv4 == "" && poolIPv6 == "" {
		return VethIPs{}, fmt.Errorf("at least one pool must be provided (IPv4 or IPv6)")
	}

	veths := VethIPs{}

	if poolIPv4 != "" {
		ips, err := vethIPsForFamily(poolIPv4, index)
		if err != nil {
			return VethIPs{}, fmt.Errorf("failed to get IPv4 veth IPs: %w", err)
		}
		veths.Ipv4 = ips
	}

	if poolIPv6 != "" {
		ips, err := vethIPsForFamily(poolIPv6, index)
		if err != nil {
			return VethIPs{}, fmt.Errorf("failed to get IPv6 veth IPs: %w", err)
		}
		veths.Ipv6 = ips
	}

	return veths, nil
}

// VTEPIp returns the IP to be used for the local VTEP on the ith node.
func VTEPIp(pool string, index int) (net.IPNet, error) {
	_, cidr, err := net.ParseCIDR(pool)
	if err != nil {
		return net.IPNet{}, fmt.Errorf("failed to parse pool %s: %w", pool, err)
	}

	ips, err := sliceCIDR(cidr, index, 1)
	if err != nil {
		return net.IPNet{}, err
	}
	if len(ips) != 1 {
		return net.IPNet{}, fmt.Errorf("vtepIP, expecting 1 ip, got %v", ips)
	}
	res := net.IPNet{
		IP:   ips[0].IP,
		Mask: net.CIDRMask(32, 32),
	}
	if ipfamily.ForAddress(res.IP) == ipfamily.IPv6 {
		res.Mask = net.CIDRMask(128, 128)
	}
	return res, nil
}

// RouterID returns the IP to be used for the router ID on the ith node.
func RouterID(pool string, index int) (string, error) {
	_, cidr, err := net.ParseCIDR(pool)
	if err != nil {
		return "", fmt.Errorf("failed to parse pool %s: %w", pool, err)
	}

	ip, err := gocidr.Host(cidr, index+1)
	if err != nil {
		return "", fmt.Errorf("failed to get router id for node %d from cidr %s: %w", index, cidr, err)
	}

	return ip.String(), nil
}

func Locator(pool string, index int) (net.IPNet, error) {
	_, ipNet, err := net.ParseCIDR(pool)
	if err != nil {
		return net.IPNet{}, fmt.Errorf("failed to parse cidr %s: %w", pool, err)
	}

	// Only support IPv6
	if len(ipNet.IP) != 16 {
		return net.IPNet{}, fmt.Errorf("only IPv6 addresses are supported")
	}

	// Get the network portion and prefix length
	ones, _ := ipNet.Mask.Size()

	// Only support /48, /64, and /96
	if ones != 48 && ones != 64 && ones != 96 {
		return net.IPNet{}, fmt.Errorf("only /48, /64, and /96 prefix lengths are supported, got /%d", ones)
	}

	// Make a copy of the IP to avoid modifying the original
	resultIP := make(net.IP, len(ipNet.IP))
	copy(resultIP, ipNet.IP)

	// Apply the network mask to ensure we start from the network address
	for i := range resultIP {
		resultIP[i] &= ipNet.Mask[i]
	}

	// Determine where to add the index based on the prefix length
	// IPv6 addresses have 8 groups of 16 bits each:
	// [0:1] [2:3] [4:5] [6:7] [8:9] [10:11] [12:13] [14:15]
	// For /48: increment at position [6:7] (4th group)
	// For /64: increment at position [4:5] (3rd group)
	// For /96: increment at position [10:11] (6th group)

	var pos int
	switch ones {
	case 48:
		pos = 6 // 4th 16-bit group
	case 64:
		pos = 4 // 3rd 16-bit group
	case 96:
		pos = 10 // 6th 16-bit group
	}

	// Add the index with overflow handling (big-endian arithmetic)
	carry := uint64(index)
	for i := pos + 1; i >= 0 && carry > 0; i -= 2 {
		// Read current 16-bit value
		current := uint64(resultIP[i-1])<<8 + uint64(resultIP[i])
		current += carry

		// Write back the result
		resultIP[i-1] = byte(current >> 8)
		resultIP[i] = byte(current & 0xFF)

		// Propagate carry to next higher-order group
		carry = current >> 16
	}

	return net.IPNet{
		IP:   resultIP,
		Mask: ipNet.Mask,
	}, nil
}

// cidrElem returns the ith elem of len size for the given cidr.
func cidrElem(pool *net.IPNet, index int) (*net.IPNet, error) {
	ip, err := gocidr.Host(pool, index)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d address from %s: %w", index, pool, err)
	}
	return &net.IPNet{
		IP:   ip,
		Mask: pool.Mask,
	}, nil
}

// sliceCIDR returns the ith block of len size for the given cidr.
func sliceCIDR(pool *net.IPNet, index, size int) ([]net.IPNet, error) {
	res := []net.IPNet{}
	for i := 0; i < size; i++ {
		ipIndex := size*index + i
		ip, err := gocidr.Host(pool, ipIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to get %d address from %s: %w", ipIndex, pool, err)
		}
		ipNet := net.IPNet{
			IP:   ip,
			Mask: pool.Mask,
		}

		res = append(res, ipNet)
	}

	return res, nil
}

// IPsInCDIR returns the number of IPs in the given CIDR.
func IPsInCIDR(pool string) (uint64, error) {
	_, ipNet, err := net.ParseCIDR(pool)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cidr %s: %w", pool, err)
	}

	return gocidr.AddressCount(ipNet), nil
}

// vethIPsForFamily returns the host side and PE side IPs for a given pool and index.
func vethIPsForFamily(pool string, index int) (VethIPsForFamily, error) {
	_, cidr, err := net.ParseCIDR(pool)
	if err != nil {
		return VethIPsForFamily{}, fmt.Errorf("failed to parse pool %s: %w", pool, err)
	}

	peSide, err := cidrElem(cidr, 0)
	if err != nil {
		return VethIPsForFamily{}, err
	}

	hostSideIndex := index + 1
	if peSide.IP[len(peSide.IP)-1] == 0 {
		peSide, err = cidrElem(cidr, 1)
		if err != nil {
			return VethIPsForFamily{}, err
		}
		hostSideIndex = index + 2
	}

	hostSide, err := cidrElem(cidr, hostSideIndex)
	if err != nil {
		return VethIPsForFamily{}, err
	}
	return VethIPsForFamily{
		HostSide: *hostSide,
		PeSide:   *peSide,
	}, nil
}
