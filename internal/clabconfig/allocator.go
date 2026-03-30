// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"crypto/sha256"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	gocidr "github.com/apparentlymart/go-cidr/cidr"
)

// skipKinds lists node kinds that should be skipped during matching.
var skipKinds = map[string]bool{
	"bridge":        true,
	"ext-container": true,
	"k8s-kind":      true,
}

// sortedLinkKey is used to sort point-to-point links deterministically.
type sortedLinkKey struct {
	minNode    string
	maxNode    string
	interfaceA string
	link       ClabLink
	eps        [2]Endpoint
}

// matchNode finds the first NodeConfig whose pattern matches nodeName.
// Returns the NodeConfig and true if found, or zero value and false otherwise.
func matchNode(nodeName string, configs []NodeConfig) (NodeConfig, bool) {
	for _, nc := range configs {
		re := regexp.MustCompile("^" + nc.Pattern + "$")
		if re.MatchString(nodeName) {
			return nc, true
		}
	}
	return NodeConfig{}, false
}

// allocateP2PIPs allocates point-to-point /31 IPv4 and /127 IPv6 subnets for
// each link between two non-bridge nodes.
func allocateP2PIPs(state *TopologyState, clab *ClabTopology, config *EnvironmentConfig) error {
	_, ipv4Base, err := net.ParseCIDR(config.IPRanges.PointToPoint.IPv4)
	if err != nil {
		return fmt.Errorf("parsing P2P IPv4 range: %w", err)
	}
	_, ipv6Base, err := net.ParseCIDR(config.IPRanges.PointToPoint.IPv6)
	if err != nil {
		return fmt.Errorf("parsing P2P IPv6 range: %w", err)
	}

	// Collect non-bridge links and sort them deterministically.
	var links []sortedLinkKey
	for _, link := range clab.Topology.Links {
		eps, parseErr := link.ParseEndpoints()
		if parseErr != nil {
			continue
		}
		nodeA := eps[0].Node
		nodeB := eps[1].Node

		// Skip if either node is a bridge or not in clab nodes.
		if nA, ok := clab.Topology.Nodes[nodeA]; ok && nA.Kind == "bridge" {
			continue
		}
		if nB, ok := clab.Topology.Nodes[nodeB]; ok && nB.Kind == "bridge" {
			continue
		}

		minN, maxN := nodeA, nodeB
		if nodeA > nodeB {
			minN, maxN = nodeB, nodeA
		}
		links = append(links, sortedLinkKey{
			minNode:    minN,
			maxNode:    maxN,
			interfaceA: eps[0].Interface,
			link:       link,
			eps:        eps,
		})
	}

	sort.Slice(links, func(i, j int) bool {
		if links[i].minNode != links[j].minNode {
			return links[i].minNode < links[j].minNode
		}
		if links[i].maxNode != links[j].maxNode {
			return links[i].maxNode < links[j].maxNode
		}
		return links[i].interfaceA < links[j].interfaceA
	})

	// Compute how many newBits we need for /31 from ipv4Base and /127 from ipv6Base.
	ipv4PrefixLen, _ := ipv4Base.Mask.Size()
	ipv4NewBits := 31 - ipv4PrefixLen

	ipv6PrefixLen, _ := ipv6Base.Mask.Size()
	ipv6NewBits := 127 - ipv6PrefixLen

	for i, lk := range links {
		// Allocate IPv4 /31 subnet.
		ipv4Subnet, err := gocidr.Subnet(ipv4Base, ipv4NewBits, i)
		if err != nil {
			return fmt.Errorf("allocating P2P IPv4 subnet %d: %w", i, err)
		}

		// Allocate IPv6 /127 subnet.
		ipv6Subnet, err := gocidr.Subnet(ipv6Base, ipv6NewBits, i)
		if err != nil {
			return fmt.Errorf("allocating P2P IPv6 subnet %d: %w", i, err)
		}

		eps := lk.eps
		nodeA := eps[0].Node
		ifaceA := eps[0].Interface
		nodeB := eps[1].Node
		ifaceB := eps[1].Interface

		// First IP in /31 goes to endpoint A, second to endpoint B.
		ipv4A, _ := gocidr.Host(ipv4Subnet, 0)
		ipv4B, _ := gocidr.Host(ipv4Subnet, 1)
		ipv6A, _ := gocidr.Host(ipv6Subnet, 0)
		ipv6B, _ := gocidr.Host(ipv6Subnet, 1)

		linkState := LinkState{
			NodeA:      nodeA,
			InterfaceA: ifaceA,
			NodeB:      nodeB,
			InterfaceB: ifaceB,
			IPv4Subnet: ipv4Subnet.String(),
			IPv6Subnet: ipv6Subnet.String(),
			Type:       "point-to-point",
		}
		state.Links = append(state.Links, linkState)

		// Populate interface states for nodes that are in state.Nodes.
		if ns, ok := state.Nodes[nodeA]; ok {
			if ns.Interfaces == nil {
				ns.Interfaces = make(map[string]*InterfaceState)
			}
			ns.Interfaces[ifaceA] = &InterfaceState{
				Name:          ifaceA,
				PeerNode:      nodeB,
				PeerInterface: ifaceB,
				IPv4:          fmt.Sprintf("%s/31", ipv4A.String()),
				IPv6:          fmt.Sprintf("%s/127", ipv6A.String()),
				LinkType:      "point-to-point",
			}
		}
		if ns, ok := state.Nodes[nodeB]; ok {
			if ns.Interfaces == nil {
				ns.Interfaces = make(map[string]*InterfaceState)
			}
			ns.Interfaces[ifaceB] = &InterfaceState{
				Name:          ifaceB,
				PeerNode:      nodeA,
				PeerInterface: ifaceA,
				IPv4:          fmt.Sprintf("%s/31", ipv4B.String()),
				IPv6:          fmt.Sprintf("%s/127", ipv6B.String()),
				LinkType:      "point-to-point",
			}
		}
	}

	return nil
}

// allocateBroadcastIPs allocates broadcast network subnets for bridge nodes.
func allocateBroadcastIPs(state *TopologyState, clab *ClabTopology, config *EnvironmentConfig) error {
	_, ipv4Base, err := net.ParseCIDR(config.IPRanges.Broadcast.IPv4)
	if err != nil {
		return fmt.Errorf("parsing broadcast IPv4 range: %w", err)
	}
	_, ipv6Base, err := net.ParseCIDR(config.IPRanges.Broadcast.IPv6)
	if err != nil {
		return fmt.Errorf("parsing broadcast IPv6 range: %w", err)
	}

	// Find bridge nodes, sorted by name.
	var bridgeNames []string
	for name, node := range clab.Topology.Nodes {
		if node.Kind == "bridge" {
			bridgeNames = append(bridgeNames, name)
		}
	}
	sort.Strings(bridgeNames)

	ipv4PrefixLen, _ := ipv4Base.Mask.Size()
	ipv4NewBits := 24 - ipv4PrefixLen

	ipv6PrefixLen, _ := ipv6Base.Mask.Size()
	ipv6NewBits := 64 - ipv6PrefixLen

	for bridgeIdx, bridgeName := range bridgeNames {
		// Find all links connected to this bridge.
		type memberInfo struct {
			nodeName  string
			ifaceName string
		}
		var members []memberInfo

		for _, link := range clab.Topology.Links {
			eps, parseErr := link.ParseEndpoints()
			if parseErr != nil {
				continue
			}
			if eps[0].Node == bridgeName {
				members = append(members, memberInfo{
					nodeName:  eps[1].Node,
					ifaceName: eps[1].Interface,
				})
			} else if eps[1].Node == bridgeName {
				members = append(members, memberInfo{
					nodeName:  eps[0].Node,
					ifaceName: eps[0].Interface,
				})
			}
		}

		// Sort members by node name for determinism.
		sort.Slice(members, func(i, j int) bool {
			return members[i].nodeName < members[j].nodeName
		})

		// Allocate /24 IPv4 and /64 IPv6 subnets.
		ipv4Subnet, err := gocidr.Subnet(ipv4Base, ipv4NewBits, bridgeIdx)
		if err != nil {
			return fmt.Errorf("allocating broadcast IPv4 subnet %d: %w", bridgeIdx, err)
		}
		ipv6Subnet, err := gocidr.Subnet(ipv6Base, ipv6NewBits, bridgeIdx)
		if err != nil {
			return fmt.Errorf("allocating broadcast IPv6 subnet %d: %w", bridgeIdx, err)
		}

		bcastNet := BroadcastNetwork{
			SwitchName: bridgeName,
			IPv4Subnet: ipv4Subnet.String(),
			IPv6Subnet: ipv6Subnet.String(),
		}

		for memberIdx, m := range members {
			ipv4Addr, err := gocidr.Host(ipv4Subnet, memberIdx+1)
			if err != nil {
				return fmt.Errorf("allocating broadcast IPv4 host %d: %w", memberIdx, err)
			}
			ipv6Addr, err := gocidr.Host(ipv6Subnet, memberIdx+1)
			if err != nil {
				return fmt.Errorf("allocating broadcast IPv6 host %d: %w", memberIdx, err)
			}

			bcastNet.Members = append(bcastNet.Members, BroadcastMember{
				NodeName:      m.nodeName,
				InterfaceName: m.ifaceName,
				IPv4:          fmt.Sprintf("%s/24", ipv4Addr.String()),
				IPv6:          fmt.Sprintf("%s/64", ipv6Addr.String()),
			})

			// Populate interface state for matched nodes.
			if ns, ok := state.Nodes[m.nodeName]; ok {
				if ns.Interfaces == nil {
					ns.Interfaces = make(map[string]*InterfaceState)
				}
				ns.Interfaces[m.ifaceName] = &InterfaceState{
					Name:          m.ifaceName,
					PeerNode:      bridgeName,
					PeerInterface: "",
					IPv4:          fmt.Sprintf("%s/24", ipv4Addr.String()),
					IPv6:          fmt.Sprintf("%s/64", ipv6Addr.String()),
					LinkType:      "broadcast",
				}
			}
		}

		state.BroadcastNetworks = append(state.BroadcastNetworks, bcastNet)
	}

	return nil
}

// allocateVTEPIPs allocates VTEP IPs for edge-leaf nodes.
func allocateVTEPIPs(state *TopologyState, config *EnvironmentConfig) error {
	if config.IPRanges.VTEP == "" {
		return nil
	}
	_, vtepBase, err := net.ParseCIDR(config.IPRanges.VTEP)
	if err != nil {
		return fmt.Errorf("parsing VTEP range: %w", err)
	}

	// Collect edge-leaf nodes sorted by name.
	var edgeLeafNames []string
	for name, ns := range state.Nodes {
		if ns.Role == "edge-leaf" {
			edgeLeafNames = append(edgeLeafNames, name)
		}
	}
	sort.Strings(edgeLeafNames)

	for i, name := range edgeLeafNames {
		ip, err := gocidr.Host(vtepBase, i+1)
		if err != nil {
			return fmt.Errorf("allocating VTEP IP for %s: %w", name, err)
		}
		state.Nodes[name].VTEPIP = ip.String()
	}

	return nil
}

// allocateRouterIDs allocates router IDs for all matched nodes.
func allocateRouterIDs(state *TopologyState, config *EnvironmentConfig) error {
	if config.IPRanges.RouterID == "" {
		return nil
	}
	_, ridBase, err := net.ParseCIDR(config.IPRanges.RouterID)
	if err != nil {
		return fmt.Errorf("parsing router ID range: %w", err)
	}

	var nodeNames []string
	for name := range state.Nodes {
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)

	for i, name := range nodeNames {
		ip, err := gocidr.Host(ridBase, i+1)
		if err != nil {
			return fmt.Errorf("allocating router ID for %s: %w", name, err)
		}
		state.Nodes[name].RouterID = ip.String()
	}

	return nil
}

// GenerateMAC generates a deterministic locally-administered MAC address
// from the hash of nodeName and vrfName.
func GenerateMAC(nodeName, vrfName string) string {
	h := sha256.Sum256([]byte(nodeName + vrfName))
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", h[0], h[1], h[2], h[3], h[4])
}

// resolveBGPPeers resolves BGP peer patterns to concrete peer addresses.
func resolveBGPPeers(state *TopologyState, config *EnvironmentConfig) error {
	// Build a map from node name to its NodeConfig.
	nodeConfigs := make(map[string]NodeConfig)
	for name := range state.Nodes {
		nc, ok := matchNode(name, config.Nodes)
		if ok {
			nodeConfigs[name] = nc
		}
	}

	for nodeName, ns := range state.Nodes {
		nc, ok := nodeConfigs[nodeName]
		if !ok {
			continue
		}

		bgpState := &BGPState{
			ASN: nc.BGP.ASN,
		}

		for _, peerCfg := range nc.BGP.Peers {
			peerRe := regexp.MustCompile("^" + peerCfg.Pattern + "$")

			// Find all matching peer nodes.
			var peerNames []string
			for peerName := range state.Nodes {
				if peerName == nodeName {
					continue
				}
				if peerRe.MatchString(peerName) {
					peerNames = append(peerNames, peerName)
				}
			}
			sort.Strings(peerNames)

			for _, peerName := range peerNames {
				peerNC, ok := nodeConfigs[peerName]
				if !ok {
					continue
				}

				// Find the link between nodeName and peerName,
				// and get the peer's IP on that link.
				var ipv4Addr, ipv6Addr string
				for _, iface := range state.Nodes[nodeName].Interfaces {
					if iface.PeerNode == peerName {
						// We need the peer's IP, which is on the peer's interface.
						// Strip prefix length (e.g., "10.0.0.1/31" -> "10.0.0.1")
						// because BGP neighbor commands use bare IPs.
						peerIface := state.Nodes[peerName].Interfaces[iface.PeerInterface]
						if peerIface != nil {
							ipv4Addr = stripPrefix(peerIface.IPv4)
							ipv6Addr = stripPrefix(peerIface.IPv6)
						}
						break
					}
				}

				bgpState.Peers = append(bgpState.Peers, BGPPeerState{
					NodeName:    peerName,
					ASN:         peerNC.BGP.ASN,
					IPv4Address: ipv4Addr,
					IPv6Address: ipv6Addr,
					EVPNEnabled: peerCfg.EVPNEnabled,
					BFDEnabled:  peerCfg.BFDEnabled,
				})
			}
		}

		ns.BGP = bgpState
	}

	return nil
}

// populateVRFs populates VRF state for edge-leaf nodes including MAC and bridge ID.
func populateVRFs(state *TopologyState, config *EnvironmentConfig) {
	for nodeName, ns := range state.Nodes {
		if ns.Role != "edge-leaf" {
			continue
		}
		nc, ok := matchNode(nodeName, config.Nodes)
		if !ok {
			continue
		}
		if len(nc.VRFs) == 0 {
			continue
		}

		ns.VRFs = make(map[string]*VRFState)

		// Sort VRF names for deterministic bridge ID assignment.
		var vrfNames []string
		for vrfName := range nc.VRFs {
			vrfNames = append(vrfNames, vrfName)
		}
		sort.Strings(vrfNames)

		for bridgeID, vrfName := range vrfNames {
			vrfCfg := nc.VRFs[vrfName]
			ns.VRFs[vrfName] = &VRFState{
				Name:                  vrfName,
				VNI:                   vrfCfg.VNI,
				Interfaces:            vrfCfg.Interfaces,
				RedistributeConnected: vrfCfg.RedistributeConnected,
				MACAddress:            GenerateMAC(nodeName, vrfName),
				BridgeID:              bridgeID + 1,
			}
		}
	}
}

// Allocate computes the complete topology state from a clab topology and
// environment configuration. It returns the topology state, a list of
// warnings, and an error if any allocation fails.
func Allocate(clab *ClabTopology, config *EnvironmentConfig) (*TopologyState, []string, error) {
	warnings, err := Validate(clab, config)
	if err != nil {
		return nil, warnings, fmt.Errorf("validation failed: %w", err)
	}

	state := &TopologyState{
		TopologyName: clab.Name,
		Nodes:        make(map[string]*NodeState),
	}

	// Match each clab node to its config pattern.
	for nodeName, node := range clab.Topology.Nodes {
		if skipKinds[node.Kind] {
			continue
		}
		nc, ok := matchNode(nodeName, config.Nodes)
		if !ok {
			continue
		}
		state.Nodes[nodeName] = &NodeState{
			Name:           nodeName,
			MatchedPattern: nc.Pattern,
			Role:           nc.Role,
			Interfaces:     make(map[string]*InterfaceState),
		}
	}

	// Allocate resources in order.
	if err := allocateP2PIPs(state, clab, config); err != nil {
		return nil, warnings, fmt.Errorf("P2P IP allocation: %w", err)
	}

	if err := allocateBroadcastIPs(state, clab, config); err != nil {
		return nil, warnings, fmt.Errorf("broadcast IP allocation: %w", err)
	}

	if err := allocateVTEPIPs(state, config); err != nil {
		return nil, warnings, fmt.Errorf("VTEP IP allocation: %w", err)
	}

	if err := allocateRouterIDs(state, config); err != nil {
		return nil, warnings, fmt.Errorf("router ID allocation: %w", err)
	}

	// Populate VRF state (includes MAC generation).
	populateVRFs(state, config)

	// Resolve BGP peers.
	if err := resolveBGPPeers(state, config); err != nil {
		return nil, warnings, fmt.Errorf("BGP peer resolution: %w", err)
	}

	// Compute input hash (placeholder using topology name).
	h := sha256.New()
	h.Write([]byte(clab.Name))
	for _, nc := range config.Nodes {
		h.Write([]byte(nc.Pattern))
	}
	state.InputHash = fmt.Sprintf("%x", h.Sum(nil))

	return state, warnings, nil
}

// stripPrefix removes the CIDR prefix length from an IP address string
// (e.g., "10.0.0.1/31" -> "10.0.0.1"). If no prefix is present, returns as-is.
func stripPrefix(addr string) string {
	if idx := strings.IndexByte(addr, '/'); idx >= 0 {
		return addr[:idx]
	}
	return addr
}
