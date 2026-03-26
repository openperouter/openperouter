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

var linkAddresses map[link]addressPair

func init() {
	linkAddresses = map[link]addressPair{}
	add("clab-kind-leafkind", "pe-kind-control-plane", "192.168.11.2", "192.168.11.3")
	add("clab-kind-leafkind", "pe-kind-control-plane", "2001:db8:11::2", "2001:db8:11::3")
	add("clab-kind-leafkind", "pe-kind-control-plane", "toleafkind", "tokindctrlpl")
	add("clab-kind-leafkind", "pe-kind-worker", "192.168.11.2", "192.168.11.4")
	add("clab-kind-leafkind", "pe-kind-worker", "2001:db8:11::2", "2001:db8:11::4")
	add("clab-kind-leafkind", "pe-kind-worker", "toleafkind", "tokindworker")
	add("clab-kind-leafkind", "clab-kind-spine", "192.168.1.5", "192.168.1.4")
	add("clab-kind-leafA", "clab-kind-spine", "192.168.1.1", "192.168.1.0")
	add("clab-kind-leafB", "clab-kind-spine", "192.168.1.3", "192.168.1.2")
	add("clab-kind-leafA", "clab-kind-hostA_red", "192.168.20.1", HostARedIPv4)
	add("clab-kind-leafA", "clab-kind-hostA_blue", "192.168.21.1", HostABlueIPv4)
	add("clab-kind-leafB", "clab-kind-hostB_red", "192.169.20.1", HostBRedIPv4)
	add("clab-kind-leafB", "clab-kind-hostB_blue", "192.169.21.1", HostBBlueIPv4)
}

type link struct {
	from, to string
}

type addressPair struct {
	from, to address
}

// address holds a mapping of IP family to address. Examples for address are:
// - ipfamily.IPv4: 192.168.1.1
// - ipfamily.IPv6: fd00::1
// - ipfamily.Unnumbered: eth0
type address map[ipfamily.Family]string

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
	pair, ok := linkAddresses[l]
	if !ok {
		return Neighbor{}, fmt.Errorf("link between nodes %q and %q not found", origFrom, origTo)
	}

	neighborAddress := pair.to
	if origFrom != from {
		neighborAddress = pair.from
	}
	neighborName := neighborAddress[af]
	if neighborName == "" {
		return Neighbor{}, fmt.Errorf("node %q has no address to neighbor %q for AF %s", origFrom, origTo, af)
	}
	isInterface := af == ipfamily.Unnumbered
	return Neighbor{Name: neighborName, IsInterface: isInterface}, nil
}

func add(from, to, addressFrom, addressTo string) {
	family, err := ipfamily.ForAddresses(addressFrom)
	if err != nil {
		family = ipfamily.Unnumbered
	}
	familyTo, err := ipfamily.ForAddresses(addressTo)
	if err != nil {
		familyTo = ipfamily.Unnumbered
	}
	if family != familyTo {
		panic(fmt.Sprintf("IP address families differ: from %q vs to %q", addressFrom, addressTo))
	}

	if from > to {
		from, to = to, from
		addressFrom, addressTo = addressTo, addressFrom
	}
	l := link{from, to}
	pair, ok := linkAddresses[l]
	if !ok {
		pair = addressPair{
			from: make(address),
			to:   make(address),
		}
	}
	pair.from[family] = addressFrom
	pair.to[family] = addressTo
	linkAddresses[l] = pair
}
