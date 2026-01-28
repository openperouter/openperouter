// SPDX-License-Identifier:Apache-2.0

package ipfamily // import "go.universe.tf/metallb/internal/ipfamily"

import (
	"fmt"
	"net"
	"strings"

	v1 "k8s.io/api/core/v1"
)

// IP family helps identifying single stack IPv4/IPv6 vs Dual-stack ["IPv4", "IPv6"] or ["IPv6", "Ipv4"].
type Family string

const (
	IPv4      Family = "ipv4"
	IPv6      Family = "ipv6"
	DualStack Family = "dual"
	Unknown   Family = "unknown"
)

// ForAddresses returns the address family given list of addresses strings.
func ForAddresses(ipsStrings ...string) (Family, error) {
	ips := []net.IP{}
	for _, ipString := range ipsStrings {
		ip := net.ParseIP(ipString)
		if ip == nil {
			return Unknown, fmt.Errorf("invalid ip: %s", ipString)
		}
		ips = append(ips, ip)
	}
	return ForAddressesIPs(ips)
}

// ForCIDRStrings returns the address family given list of addresses strings.
func ForCIDRStrings(cidrsStrings ...string) (Family, error) {
	ips := []net.IP{}
	for _, cidrString := range cidrsStrings {
		ip, _, err := net.ParseCIDR(cidrString)
		if err != nil {
			return Unknown, fmt.Errorf("invalid cidr: %w", err)
		}
		ips = append(ips, ip)
	}
	return ForAddressesIPs(ips)
}

// ForAddressesIPs returns the address family from a given list of addresses IPs.
func ForAddressesIPs(ips []net.IP) (Family, error) {
	switch len(ips) {
	case 1:
		ip := ips[0]
		if ip == nil {
			return Unknown, fmt.Errorf("IPFamilyForAddresses: Invalid address %q", ips)
		}
		res := ForAddress(ip)
		return res, nil
	case 2:
		ip1 := ips[0]
		ip2 := ips[1]
		if ip1 == nil || ip2 == nil {
			return Unknown, fmt.Errorf("IPFamilyForAddresses: Invalid address %q", ips)
		}
		if (ip1.To4() == nil) == (ip2.To4() == nil) {
			return Unknown, fmt.Errorf("IPFamilyForAddresses: same address family %q", ips)
		}
		return DualStack, nil
	default:
		return Unknown, fmt.Errorf("IPFamilyForAddresses: invalid ips length %d %q", len(ips), ips)
	}
}

// ForCIDRString returns the address family from a given CIDR in string format.
func ForCIDRString(cidr string) Family {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return Unknown
	}
	if ip.To4() == nil {
		return IPv6
	}
	return IPv4
}

// ForCIDR returns the address family from a given CIDR.
func ForCIDR(cidr *net.IPNet) Family {
	if cidr.IP.To4() == nil {
		return IPv6
	}
	return IPv4
}

// ForAddress returns the address family for a given address.
func ForAddress(ip net.IP) Family {
	if ip.To4() == nil {
		return IPv6
	}
	return IPv4
}

// ForService returns the address family of a given service.
func ForService(svc *v1.Service) (Family, error) {
	if len(svc.Spec.ClusterIPs) > 0 {
		return ForAddresses(svc.Spec.ClusterIPs...)
	}
	// fallback to clusterip if clusterips are not set
	addresses := []string{svc.Spec.ClusterIP}
	return ForAddresses(addresses...)
}

// StripCIDRMask removes the CIDR mask from an IP address string.
// e.g., "192.168.1.1/24" -> "192.168.1.1"
func StripCIDRMask(ipCIDR string) string {
	parts := strings.SplitN(ipCIDR, "/", 2)
	return parts[0]
}
