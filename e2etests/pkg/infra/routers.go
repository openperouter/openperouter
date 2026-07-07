// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"fmt"

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

	// leafkind1 links - bridge connections use toswitch1
	addLinkIPs("clab-kind-leafkind1", "pe-kind-control-plane", "192.168.11.2", "192.168.11.3")
	addLinkIPv6s("clab-kind-leafkind1", "pe-kind-control-plane", "2001:db8:11::2", "2001:db8:11::3")
	addLinkInterfaces("clab-kind-leafkind1", "pe-kind-control-plane", "tokindctrlpl", "toleafkind1")
	addLinkIPs("clab-kind-leafkind1", "pe-kind-worker", "192.168.11.2", "192.168.11.4")
	addLinkIPv6s("clab-kind-leafkind1", "pe-kind-worker", "2001:db8:11::2", "2001:db8:11::4")
	addLinkInterfaces("clab-kind-leafkind1", "pe-kind-worker", "tokindworker", "toleafkind1")
	addLinkIPs("clab-kind-leafkind1", "clab-kind-spine", "192.168.1.5", "192.168.1.4")

	// leafkind2 links - bridge connections use toswitch2
	addLinkIPs("clab-kind-leafkind2", "pe-kind-control-plane", "192.168.12.2", "192.168.12.3")
	addLinkIPv6s("clab-kind-leafkind2", "pe-kind-control-plane", "2001:db8:12::2", "2001:db8:12::3")
	addLinkInterfaces("clab-kind-leafkind2", "pe-kind-control-plane", "tokindctrlpl", "toleafkind2")
	addLinkIPs("clab-kind-leafkind2", "pe-kind-worker", "192.168.12.2", "192.168.12.4")
	addLinkIPv6s("clab-kind-leafkind2", "pe-kind-worker", "2001:db8:12::2", "2001:db8:12::4")
	addLinkInterfaces("clab-kind-leafkind2", "pe-kind-worker", "tokindworker", "toleafkind2")
	addLinkIPs("clab-kind-leafkind2", "clab-kind-spine", "192.168.1.7", "192.168.1.6")

	// Other leaf links
	addLinkIPs("clab-kind-leafA", "clab-kind-spine", "192.168.1.1", "192.168.1.0")
	addLinkIPs("clab-kind-leafB", "clab-kind-spine", "192.168.1.3", "192.168.1.2")
	addLinkIPs("clab-kind-leafA", "clab-kind-hostA_red", "192.168.20.1", HostARedIPv4)
	addLinkIPs("clab-kind-leafA", "clab-kind-hostA_blue", "192.168.21.1", HostABlueIPv4)
	addLinkIPs("clab-kind-leafB", "clab-kind-hostB_red", "192.169.20.1", HostBRedIPv4)
	addLinkIPs("clab-kind-leafB", "clab-kind-hostB_blue", "192.169.21.1", HostBBlueIPv4)
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

func addLinkIPs(from, to, addressFrom, addressTo string) {
	family := ipfamily.IPv4
	add(family, from, to, addressFrom, addressTo)
}

func addLinkIPv6s(from, to, addressFrom, addressTo string) {
	family := ipfamily.IPv6
	add(family, from, to, addressFrom, addressTo)
}

func addLinkInterfaces(from, to, addressFrom, addressTo string) {
	family := ipfamily.Unnumbered
	add(family, from, to, addressFrom, addressTo)
}

// add registers a link in both directions so that NeighborForFamily can
// resolve the remote address from either endpoint of the link.
func add(family ipfamily.Family, from, to, addressFrom, addressTo string) {
	addLink(family, from, to, addressFrom, addressTo)
	addLink(family, to, from, addressTo, addressFrom)
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
