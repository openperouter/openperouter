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
	topo := Topology()

	links = linksForRouters{
		nodes: map[string]node{},
	}

	// Point-to-point links
	for _, link := range topo.Links {
		nodeAContainer := ContainerName(link.NodeA)
		nodeBContainer := ContainerName(link.NodeB)

		var ipA, ipB string

		// Get nodeA's IP
		if nodeA, ok := topo.Nodes[link.NodeA]; ok {
			if iface, ok := nodeA.Interfaces[link.InterfaceA]; ok {
				ipA = stripCIDR(iface.IPv4)
			}
		}

		// Get nodeB's IP: either from Nodes or computed as the other end of the /31
		if nodeB, ok := topo.Nodes[link.NodeB]; ok {
			if iface, ok := nodeB.Interfaces[link.InterfaceB]; ok {
				ipB = stripCIDR(iface.IPv4)
			}
		} else if ipA != "" {
			// nodeB is not in Nodes (e.g., a host); compute peer IP
			if nodeA, ok := topo.Nodes[link.NodeA]; ok {
				if iface, ok := nodeA.Interfaces[link.InterfaceA]; ok {
					ipB = stripCIDR(otherIP(iface.IPv4))
				}
			}
		}

		if ipA != "" && ipB != "" {
			links.Add(nodeAContainer, nodeBContainer, ipA, ipB)
		}
	}

	// Broadcast network links: add pairwise links between all members
	for _, bn := range topo.BroadcastNetworks {
		for i, memberA := range bn.Members {
			for j, memberB := range bn.Members {
				if i >= j {
					continue
				}
				containerA := ContainerName(memberA.NodeName)
				containerB := ContainerName(memberB.NodeName)
				ipA := stripCIDR(memberA.IPv4)
				ipB := stripCIDR(memberB.IPv4)
				if ipA != "" && ipB != "" {
					links.Add(containerA, containerB, ipA, ipB)
				}
			}
		}
	}
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
