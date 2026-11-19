// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"fmt"
	"net/netip"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
)

const (
	ClabPrefix = "clab-kind-"
	KindLeaf   = ClabPrefix + "leafkind1"
	KindLeaf2  = ClabPrefix + "leafkind2"
	LeafA      = ClabPrefix + "leafA"
	LeafB      = ClabPrefix + "leafB"
	LeafSRV6   = ClabPrefix + "leafSRV6"
	Spine      = ClabPrefix + "spine"
)

var (
	KindLeaf1Container = frr.Container{
		Name:       KindLeaf,
		ConfigPath: "leafkind1",
	}
	KindLeaf2Container = frr.Container{
		Name:       KindLeaf2,
		ConfigPath: "leafkind2",
	}
	LeafAContainer = frr.Container{
		Name:       LeafA,
		ConfigPath: "leafA",
	}

	LeafBContainer = frr.Container{
		Name:       LeafB,
		ConfigPath: "leafB",
	}

	LeafSRV6Container = frr.Container{
		Name:       LeafSRV6,
		ConfigPath: "leafSRV6",
	}
)

var linksForFamily map[ipfamily.Family]map[link]linkAddresses

func init() {
	linksForFamily = map[ipfamily.Family]map[link]linkAddresses{}
}

func registerFabricLinks() error {
	if err := addLinkIPs(KindLeaf, Spine, "192.168.1.5", "192.168.1.4"); err != nil {
		return err
	}
	if err := addLinkIPs(KindLeaf2, Spine, "192.168.1.7", "192.168.1.6"); err != nil {
		return err
	}
	if err := addLinkIPs(LeafA, Spine, "192.168.1.1", "192.168.1.0"); err != nil {
		return err
	}
	if err := addLinkIPs(LeafB, Spine, "192.168.1.3", "192.168.1.2"); err != nil {
		return err
	}
	if err := addLinkIPs(LeafA, ClabPrefix+"hostA_red", "192.168.20.1", HostARedIPv4); err != nil {
		return err
	}
	if err := addLinkIPs(LeafA, ClabPrefix+"hostA_blue", "192.168.21.1", HostABlueIPv4); err != nil {
		return err
	}
	if err := addLinkIPs(LeafB, ClabPrefix+"hostB_red", "192.169.20.1", HostBRedIPv4); err != nil {
		return err
	}
	if err := addLinkIPs(LeafB, ClabPrefix+"hostB_blue", "192.169.21.1", HostBBlueIPv4); err != nil {
		return err
	}
	return nil
}

// registerNodeLinks registers per-node underlay links from nodelink JSON.
func registerNodeLinks(cfg nodeLinksConfig) error {
	for nodeName, nc := range cfg.Nodes {
		if err := addLinkIPs(KindLeaf, nodeName, kindLeafIP, nc.IPForKindLeaf); err != nil {
			return err
		}
		if err := addLinkIPs(KindLeaf2, nodeName, kindLeaf2IP, nc.IPForKindLeaf2); err != nil {
			return err
		}

		if nc.IPv6ForKindLeaf != "" || nc.IPv6ForKindLeaf2 != "" {
			if err := addLinkIPv6s(KindLeaf, nodeName, kindLeafIPv6, nc.IPv6ForKindLeaf); err != nil {
				return err
			}
			if err := addLinkIPv6s(KindLeaf2, nodeName, kindLeaf2IPv6, nc.IPv6ForKindLeaf2); err != nil {
				return err
			}
		}

		if nc.LeafIfaceForKindLeaf != "" || nc.IfaceForKindLeaf != "" ||
			nc.LeafIfaceForKindLeaf2 != "" || nc.IfaceForKindLeaf2 != "" {
			if err := addLinkInterfaces(KindLeaf, nodeName, nc.LeafIfaceForKindLeaf, nc.IfaceForKindLeaf); err != nil {
				return err
			}
			if err := addLinkInterfaces(KindLeaf2, nodeName, nc.LeafIfaceForKindLeaf2, nc.IfaceForKindLeaf2); err != nil {
				return err
			}
		}
	}
	return nil
}

type link struct {
	from, to string
}

type linkAddresses struct {
	from, to string
}

// NeighborIP is a wrapper around NeighborForFamily for IPv4.
func NeighborIP(from, to string) (string, error) {
	n, err := NeighborForFamily(from, to, ipfamily.IPv4)
	if err != nil {
		return "", err
	}
	return n.ID, nil
}

// NeighborForFamily returns the neighbor information for the given IP family between two nodes.
// It returns the neighbor's name (IP address or interface name) and whether it's an interface.
func NeighborForFamily(from, to string, af ipfamily.Family) (Neighbor, error) {
	l := link{from, to}
	pair, ok := linksForFamily[af][l]
	if !ok {
		return Neighbor{}, fmt.Errorf("link between nodes %q and %q not found", from, to)
	}

	// For unnumbered BGP, use the local interface name (pair.from)
	// For numbered BGP, use the remote IP address (pair.to)
	neighborID := pair.to
	if af == ipfamily.Unnumbered {
		neighborID = pair.from
	}
	if neighborID == "" {
		return Neighbor{}, fmt.Errorf("node %q has no address to neighbor %q for AF %s", from, to, af)
	}
	return Neighbor{ID: neighborID, IsInterface: af == ipfamily.Unnumbered}, nil
}

func addLinkIPs(from, to, addressFrom, addressTo string) error {
	for _, address := range []string{addressFrom, addressTo} {
		ip, err := netip.ParseAddr(address)
		if err != nil {
			return fmt.Errorf("IPv4 link %q to %q: %w", from, to, err)
		}
		if !ip.Is4() {
			return fmt.Errorf("link %q to %q: expected IPv4 address, got %q", from, to, address)
		}
	}
	return add(ipfamily.IPv4, from, to, addressFrom, addressTo)
}

func addLinkIPv6s(from, to, addressFrom, addressTo string) error {
	for _, address := range []string{addressFrom, addressTo} {
		ip, err := netip.ParseAddr(address)
		if err != nil {
			return fmt.Errorf("IPv6 link %q to %q: %w", from, to, err)
		}
		if !ip.Is6() {
			return fmt.Errorf("link %q to %q: expected IPv6 address, got %q", from, to, address)
		}
	}
	return add(ipfamily.IPv6, from, to, addressFrom, addressTo)
}

func addLinkInterfaces(from, to, interfaceFrom, interfaceTo string) error {
	return add(ipfamily.Unnumbered, from, to, interfaceFrom, interfaceTo)
}

// add registers a link in both directions so that NeighborForFamily can
// resolve the remote address from either endpoint of the link.
func add(family ipfamily.Family, from, to, addressFrom, addressTo string) error {
	if from == "" || to == "" || addressFrom == "" || addressTo == "" {
		return fmt.Errorf("link %q to %q requires non-empty endpoints and addresses or interfaces", from, to)
	}
	addLink(family, from, to, addressFrom, addressTo)
	addLink(family, to, from, addressTo, addressFrom)
	return nil
}

// addLink stores one direction of a link, mapping (from, to) to the local
// and remote addresses for that direction.
func addLink(family ipfamily.Family, from, to, addressFrom, addressTo string) {
	l := link{from, to}
	if linksForFamily[family] == nil {
		linksForFamily[family] = map[link]linkAddresses{}
	}
	pair := linksForFamily[family][l]
	pair.from = addressFrom
	pair.to = addressTo
	linksForFamily[family][l] = pair
}
