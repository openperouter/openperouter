// SPDX-License-Identifier:Apache-2.0

package ipam

import (
	"fmt"
	"math/big"
	"net"

	gocidr "github.com/apparentlymart/go-cidr/cidr"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"k8s.io/utils/ptr"
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
func VethIPsFromPool(poolIPv4, poolIPv6 *string, index int) (VethIPs, error) {
	pIPv4 := ptr.Deref(poolIPv4, "")
	pIPv6 := ptr.Deref(poolIPv6, "")

	if pIPv4 == "" && pIPv6 == "" {
		return VethIPs{}, fmt.Errorf("at least one pool must be provided (IPv4 or IPv6)")
	}

	veths := VethIPs{}

	if pIPv4 != "" {
		ips, err := vethIPsForFamily(pIPv4, index)
		if err != nil {
			return VethIPs{}, fmt.Errorf("failed to get IPv4 veth IPs: %w", err)
		}
		veths.Ipv4 = ips
	}

	if pIPv6 != "" {
		ips, err := vethIPsForFamily(pIPv6, index)
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

// UnderlayIPs returns the underlay interface IPs to assign on the ith node,
// one per pool. The pool's address part marks the start of the allocation, so
// the ith node gets start+i; the prefix length is preserved so each address is
// on-link with the segment (suitable for the static CNI IPAM). When the address
// is the network address (host 0), the allocation starts at host 1 to skip the
// unusable network address (so "10.0.0.0/24" yields .1, .2, ... while
// "10.0.0.10/24" yields .10, .11, ... letting callers avoid reserved addresses
// such as the TOR).
func UnderlayIPs(pools []string, index int) ([]string, error) {
	res := make([]string, 0, len(pools))
	for _, pool := range pools {
		ip, cidr, err := net.ParseCIDR(pool)
		if err != nil {
			return nil, fmt.Errorf("failed to parse pool %s: %w", pool, err)
		}
		start := ip
		if ip.Equal(cidr.IP) {
			start = addToIP(ip, 1)
		}
		addr := addToIP(start, index)
		ones, _ := cidr.Mask.Size()
		res = append(res, fmt.Sprintf("%s/%d", addr.String(), ones))
	}
	return res, nil
}

// addToIP returns ip incremented by n, preserving the IPv4/IPv6 family.
func addToIP(ip net.IP, n int) net.IP {
	isV4 := ip.To4() != nil
	val := new(big.Int).SetBytes(ip.To16())
	val.Add(val, big.NewInt(int64(n)))
	out := val.Bytes()
	res := make(net.IP, net.IPv6len)
	copy(res[net.IPv6len-len(out):], out)
	if isV4 {
		return res.To4()
	}
	return res
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
	for i := range size {
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
