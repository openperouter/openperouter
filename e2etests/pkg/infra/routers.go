// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
)

const (
	ClabPrefix = "clab-kind-"
	KindLeaf   = ClabPrefix + "leafkind"
	LeafA      = ClabPrefix + "leafA"
	LeafB      = ClabPrefix + "leafB"
)

var (
	KindLeafContainer = frr.Container{
		Name:       KindLeaf,
		ConfigPath: "leafkind",
	}
	LeafAContainer = frr.Container{
		Name:       LeafA,
		ConfigPath: "leafA",
	}

	LeafBContainer = frr.Container{
		Name:       LeafB,
		ConfigPath: "leafB",
	}
)

var linksForFamily map[ipfamily.Family]map[link]linkAddresses

func init() {
	linksForFamily = map[ipfamily.Family]map[link]linkAddresses{}
	addLinkIPs("clab-kind-leafkind", "pe-kind-control-plane", "192.168.11.2", "192.168.11.3")
	addLinkIPv6s("clab-kind-leafkind", "pe-kind-control-plane", "2001:db8:11::2", "2001:db8:11::3")
	addLinkInterfaces("clab-kind-leafkind", "pe-kind-control-plane", "toleafkind", "tokindctrlpl")
	addLinkIPs("clab-kind-leafkind", "pe-kind-worker", "192.168.11.2", "192.168.11.4")
	addLinkIPv6s("clab-kind-leafkind", "pe-kind-worker", "2001:db8:11::2", "2001:db8:11::4")
	addLinkInterfaces("clab-kind-leafkind", "pe-kind-worker", "toleafkind", "tokindworker")
	addLinkIPs("clab-kind-leafkind", "clab-kind-spine", "192.168.1.5", "192.168.1.4")
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
	return n.Name, nil
}

// NeighborForFamily returns the neighbor information for the given IP family between two nodes.
// It returns the neighbor's name (IP address or interface name) and whether it's an interface.
func NeighborForFamily(from, to string, af ipfamily.Family) (Neighbor, error) {
	origFrom, origTo := from, to
	if from > to {
		from, to = to, from
	}
	l := link{from, to}
	pair, ok := linksForFamily[af][l]
	if !ok {
		return Neighbor{}, fmt.Errorf("link between nodes %q and %q not found", origFrom, origTo)
	}

	neighborName := pair.to
	if origFrom != from {
		neighborName = pair.from
	}
	if neighborName == "" {
		return Neighbor{}, fmt.Errorf("node %q has no address to neighbor %q for AF %s", origFrom, origTo, af)
	}
	return Neighbor{Name: neighborName, IsInterface: af == ipfamily.Unnumbered}, nil
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

func add(family ipfamily.Family, from, to, addressFrom, addressTo string) {
	if from > to {
		from, to = to, from
		addressFrom, addressTo = addressTo, addressFrom
	}
	l := link{from, to}
	if linksForFamily[family] == nil {
		linksForFamily[family] = map[link]linkAddresses{}
	}
	pair := linksForFamily[family][l]
	pair.from = addressFrom
	pair.to = addressTo
	linksForFamily[family][l] = pair
}
