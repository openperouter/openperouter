// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
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

var links linksForRouters

func init() {
	links = linksForRouters{
		nodes: map[string]node{},
	}
	links.Add("clab-kind-leafkind", "pe-kind-control-plane", "192.168.11.2", "192.168.11.3")
	links.Add("clab-kind-leafkind", "pe-kind-worker", "192.168.11.2", "192.168.11.4")
	links.Add("clab-kind-leafkind", "clab-kind-spine", "192.168.1.5", "192.168.1.4")
	links.Add("clab-kind-leafA", "clab-kind-spine", "192.168.1.1", "192.168.1.0")
	links.Add("clab-kind-leafB", "clab-kind-spine", "192.168.1.3", "192.168.1.2")
	links.Add("clab-kind-leafA", "clab-kind-hostA_red", "192.168.20.1", HostARedIP)
	links.Add("clab-kind-leafA", "clab-kind-hostA_blue", "192.168.21.1", HostABlueIP)
	links.Add("clab-kind-leafB", "clab-kind-hostB_red", "192.169.20.1", HostBRedIP)
	links.Add("clab-kind-leafB", "clab-kind-hostB_blue", "192.169.21.1", HostBBlueIP)
}

type linksForRouters struct {
	nodes map[string]node
}

func NeighborIP(from, to string) (string, error) {
	fromNeighbors, ok := links.nodes[from]
	if !ok {
		return "", fmt.Errorf("node %s not found", from)
	}
	if fromNeighbors.neighs == nil {
		return "", fmt.Errorf("node %s has no neighbors", from)
	}
	toIP, ok := fromNeighbors.neighs[to]
	if !ok {
		return "", fmt.Errorf("node %s has no neighbor %s", from, to)
	}
	return toIP, nil
}

func (l *linksForRouters) Add(first, second, addressFirst, addressSecond string) {
	addLink := func(from, to, addressTo string) {
		n, ok := l.nodes[from]
		if !ok {
			n = node{
				neighs: map[string]string{},
			}
			l.nodes[from] = n
		}
		if n.neighs == nil {
			n.neighs = map[string]string{}
		}
		n.neighs[to] = addressTo
	}
	addLink(first, second, addressSecond)
	addLink(second, first, addressFirst)
}

type node struct {
	neighs map[string]string
}
