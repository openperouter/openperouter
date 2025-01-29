// SPDX-License-Identifier:Apache-2.0

package ipam

import (
	"fmt"
	"net"

	"github.com/apparentlymart/go-cidr/cidr"
)

type Veths struct {
	HostSide      net.IPNet
	ContainerSide net.IPNet
}

// VethIPs returns the IPs for the host side and the PE side
// for a given pool on the ith node.
func VethIPs(pool string, index int) (Veths, error) {
	_, cidr, err := net.ParseCIDR(pool)
	if err != nil {
		return Veths{}, fmt.Errorf("failed to parse pool %s: %w", pool, err)
	}
	ones, _ := cidr.Mask.Size()
	peSide, err := cidrElem(pool, ones, 0)
	if err != nil {
		return Veths{}, err
	}
	hostSide, err := cidrElem(pool, ones, index+1)
	if err != nil {
		return Veths{}, err
	}
	return Veths{HostSide: *hostSide, ContainerSide: *peSide}, nil
}

// VTEPIp returns the IP to be used for the local VTEP on the ith node.
func VTEPIp(pool string, index int) (net.IPNet, error) {
	ips, err := sliceCIDR(pool, index, 1)
	if err != nil {
		return net.IPNet{}, err
	}
	if len(ips) != 1 {
		return net.IPNet{}, fmt.Errorf("vtepIP, expecting 1 ip, got %v", ips)
	}
	return ips[0], nil
}

// cidrElem returns the ith elem of len size for the given cidr.
func cidrElem(pool string, mask int, index int) (*net.IPNet, error) {
	_, ipNet, err := net.ParseCIDR(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cidr %s: %w", pool, err)
	}

	ip, err := cidr.Host(ipNet, index)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d address from %s: %w", index, pool, err)
	}
	return &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(mask, 32),
	}, nil
}

// sliceCIDR returns the ith block of len size for the given cidr.
func sliceCIDR(pool string, index, size int) ([]net.IPNet, error) {
	_, ipNet, err := net.ParseCIDR(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cidr %s: %w", pool, err)
	}

	res := []net.IPNet{}
	for i := 0; i < size; i++ {
		ipIndex := size*index + i
		ip, err := cidr.Host(ipNet, ipIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to get %d address from %s: %w", ipIndex, pool, err)
		}
		ipNet := net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(31, 32),
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

	return cidr.AddressCount(ipNet), nil
}
