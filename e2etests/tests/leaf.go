// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"net"

	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
)

func changeLeafPrefixes(leaf infra.Leaf, defaultPrefixes, redPrefixes, bluePrefixes []string) {
	defaultIPv4, defaultIPv6 := separateIPFamilies(defaultPrefixes)
	redIPv4, redIPv6 := separateIPFamilies(redPrefixes)
	blueIPv4, blueIPv6 := separateIPFamilies(bluePrefixes)

	leafConfiguration := infra.LeafConfiguration{
		Leaf: leaf,
		Default: infra.Addresses{
			IPV4: defaultIPv4,
			IPV6: defaultIPv6,
		},
		Red: infra.Addresses{
			IPV4: redIPv4,
			IPV6: redIPv6,
		},
		Blue: infra.Addresses{
			IPV4: blueIPv4,
			IPV6: blueIPv6,
		},
	}
	config, err := infra.LeafConfigToFRR(leafConfiguration)
	Expect(err).NotTo(HaveOccurred())
	err = leaf.ReloadConfig(config)
	Expect(err).NotTo(HaveOccurred())
}

func removeLeafPrefixes(leaf infra.Leaf) {
	changeLeafPrefixes(leaf, []string{}, []string{}, []string{})
}

func redistributeConnectedForLeaf(leaf infra.Leaf) {
	leafConfiguration := infra.LeafConfiguration{
		Leaf: leaf,
		Red: infra.Addresses{
			RedistributeConnected: true,
		},
		Blue: infra.Addresses{
			RedistributeConnected: true,
		},
		Default: infra.Addresses{
			RedistributeConnected: true,
		},
	}
	config, err := infra.LeafConfigToFRR(leafConfiguration)
	Expect(err).NotTo(HaveOccurred())
	err = leaf.ReloadConfig(config)
	Expect(err).NotTo(HaveOccurred())
}

// separateIPFamilies separates a slice of CIDR prefixes into IPv4 and IPv6 slices
func separateIPFamilies(prefixes []string) ([]string, []string) {
	var ipv4Prefixes []string
	var ipv6Prefixes []string

	for _, prefix := range prefixes {
		_, ipNet, err := net.ParseCIDR(prefix)
		if err != nil {
			continue
		}

		if ipNet.IP.To4() != nil {
			ipv4Prefixes = append(ipv4Prefixes, prefix)
		} else {
			ipv6Prefixes = append(ipv6Prefixes, prefix)
		}
	}

	return ipv4Prefixes, ipv6Prefixes
}
