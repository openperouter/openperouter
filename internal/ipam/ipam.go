// SPDX-License-Identifier:Apache-2.0

package ipam

import (
	"fmt"
	"net"

	gocidr "github.com/apparentlymart/go-cidr/cidr"
	"github.com/openperouter/openperouter/internal/ipfamily"
)

type Veths struct {
	HostSide net.IPNet
	PeSide   net.IPNet
}

// VethIPs returns the IPs for the host side and the PE side
// for a given pool on the ith node.
func VethIPs(pool string, index int) (Veths, error) {
	_, cidr, err := net.ParseCIDR(pool)
	if err != nil {
		return Veths{}, fmt.Errorf("failed to parse pool %s: %w", pool, err)
	}
	peSide, err := cidrElem(cidr, 0)
	if err != nil {
		return Veths{}, err
	}
	hostSide, err := cidrElem(cidr, index+1)
	if err != nil {
		return Veths{}, err
	}
	return Veths{HostSide: *hostSide, PeSide: *peSide}, nil
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
