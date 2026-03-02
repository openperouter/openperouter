// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// IPFamily represents the IP address family.
type IPFamily int

const (
	IPv4 IPFamily = iota
	IPv6
)

// TopologyState holds the complete computed state of a topology.
type TopologyState struct {
	InputHash         string                `json:"inputHash"`
	TopologyName      string                `json:"topologyName"`
	Nodes             map[string]*NodeState `json:"nodes"`
	Links             []LinkState           `json:"links"`
	BroadcastNetworks []BroadcastNetwork    `json:"broadcastNetworks"`
}

// NodeState holds the state for a single node in the topology.
type NodeState struct {
	Name           string                    `json:"name"`
	MatchedPattern string                    `json:"matchedPattern"`
	Role           string                    `json:"role"`
	RouterID       string                    `json:"routerID"`
	VTEPIP         string                    `json:"vtepIP,omitempty"`
	Interfaces     map[string]*InterfaceState `json:"interfaces"`
	VRFs           map[string]*VRFState      `json:"vrfs,omitempty"`
	BGP            *BGPState                 `json:"bgp"`
}

// InterfaceState holds the state for a single interface.
type InterfaceState struct {
	Name          string `json:"name"`
	PeerNode      string `json:"peerNode"`
	PeerInterface string `json:"peerInterface"`
	IPv4          string `json:"ipv4"`
	IPv6          string `json:"ipv6"`
	LinkType      string `json:"linkType"`
}

// VRFState holds the state for a VRF configured on a node.
type VRFState struct {
	Name                  string   `json:"name"`
	VNI                   int      `json:"vni"`
	Interfaces            []string `json:"interfaces"`
	RedistributeConnected bool     `json:"redistributeConnected"`
	MACAddress            string   `json:"macAddress"`
	BridgeID              int      `json:"bridgeID"`
}

// LinkState holds the state for a link between two nodes.
type LinkState struct {
	NodeA      string `json:"nodeA"`
	InterfaceA string `json:"interfaceA"`
	NodeB      string `json:"nodeB"`
	InterfaceB string `json:"interfaceB"`
	IPv4Subnet string `json:"ipv4Subnet"`
	IPv6Subnet string `json:"ipv6Subnet"`
	Type       string `json:"type"`
}

// BroadcastNetwork represents a broadcast domain with multiple members.
type BroadcastNetwork struct {
	SwitchName string            `json:"switchName"`
	IPv4Subnet string            `json:"ipv4Subnet"`
	IPv6Subnet string            `json:"ipv6Subnet"`
	Members    []BroadcastMember `json:"members"`
}

// BroadcastMember represents a single member of a broadcast network.
type BroadcastMember struct {
	NodeName      string `json:"nodeName"`
	InterfaceName string `json:"interfaceName"`
	IPv4          string `json:"ipv4"`
	IPv6          string `json:"ipv6"`
}

// BGPState holds the BGP configuration state for a node.
type BGPState struct {
	ASN   uint32         `json:"asn"`
	Peers []BGPPeerState `json:"peers"`
}

// BGPPeerState holds the state for a single BGP peer.
type BGPPeerState struct {
	NodeName    string `json:"nodeName"`
	ASN         uint32 `json:"asn"`
	IPv4Address string `json:"ipv4Address"`
	IPv6Address string `json:"ipv6Address"`
	EVPNEnabled bool   `json:"evpnEnabled"`
	BFDEnabled  bool   `json:"bfdEnabled"`
}

// SaveState marshals the TopologyState to indented JSON and writes it to the given file path.
func (s *TopologyState) SaveState(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal topology state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file %s: %w", path, err)
	}

	return nil
}

// Summary returns a human-readable summary of the topology state.
func (s *TopologyState) Summary() string {
	var b strings.Builder

	// Topology overview
	b.WriteString("=== Topology Summary ===\n")
	b.WriteString(fmt.Sprintf("Name:               %s\n", s.TopologyName))
	b.WriteString(fmt.Sprintf("Nodes:              %d\n", len(s.Nodes)))
	b.WriteString(fmt.Sprintf("Links:              %d\n", len(s.Links)))
	b.WriteString(fmt.Sprintf("Broadcast Networks: %d\n", len(s.BroadcastNetworks)))

	// Sort node names for deterministic output
	nodeNames := make([]string, 0, len(s.Nodes))
	for name := range s.Nodes {
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)

	totalIPs := 0

	// Per-node details
	b.WriteString("\n=== Node Details ===\n")
	for _, name := range nodeNames {
		node := s.Nodes[name]
		b.WriteString(fmt.Sprintf("\n--- %s ---\n", node.Name))
		b.WriteString(fmt.Sprintf("  Role:      %s\n", node.Role))
		b.WriteString(fmt.Sprintf("  Router ID: %s\n", node.RouterID))
		if node.VTEPIP != "" {
			b.WriteString(fmt.Sprintf("  VTEP IP:   %s\n", node.VTEPIP))
		}

		// Interfaces sorted by name
		if len(node.Interfaces) > 0 {
			ifaceNames := make([]string, 0, len(node.Interfaces))
			for ifName := range node.Interfaces {
				ifaceNames = append(ifaceNames, ifName)
			}
			sort.Strings(ifaceNames)

			b.WriteString("  Interfaces:\n")
			for _, ifName := range ifaceNames {
				iface := node.Interfaces[ifName]
				b.WriteString(fmt.Sprintf("    %s -> %s:%s\n", iface.Name, iface.PeerNode, iface.PeerInterface))
				if iface.IPv4 != "" {
					b.WriteString(fmt.Sprintf("      IPv4: %s\n", iface.IPv4))
					totalIPs++
				}
				if iface.IPv6 != "" {
					b.WriteString(fmt.Sprintf("      IPv6: %s\n", iface.IPv6))
					totalIPs++
				}
			}
		}

		// VRFs sorted by name
		if len(node.VRFs) > 0 {
			vrfNames := make([]string, 0, len(node.VRFs))
			for vrfName := range node.VRFs {
				vrfNames = append(vrfNames, vrfName)
			}
			sort.Strings(vrfNames)

			b.WriteString("  VRFs:\n")
			for _, vrfName := range vrfNames {
				vrf := node.VRFs[vrfName]
				b.WriteString(fmt.Sprintf("    %s (VNI: %d)\n", vrf.Name, vrf.VNI))
			}
		}

		// BGP peers
		if node.BGP != nil && len(node.BGP.Peers) > 0 {
			b.WriteString(fmt.Sprintf("  BGP (ASN: %d):\n", node.BGP.ASN))
			for _, peer := range node.BGP.Peers {
				b.WriteString(fmt.Sprintf("    Peer: %s (ASN: %d)", peer.NodeName, peer.ASN))
				if peer.IPv4Address != "" {
					b.WriteString(fmt.Sprintf(" IPv4: %s", peer.IPv4Address))
				}
				if peer.IPv6Address != "" {
					b.WriteString(fmt.Sprintf(" IPv6: %s", peer.IPv6Address))
				}
				b.WriteString("\n")
			}
		}
	}

	// Resource allocation summary
	b.WriteString("\n=== Resource Allocation ===\n")
	b.WriteString(fmt.Sprintf("Total IPs allocated: %d\n", totalIPs))

	return b.String()
}

// LoadState reads a JSON file from the given path and unmarshals it into a TopologyState.
func LoadState(path string) (*TopologyState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file %s: %w", path, err)
	}

	var state TopologyState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal topology state: %w", err)
	}

	return &state, nil
}

// GetNodeVTEP returns the VTEP IP for the given node.
// It returns an error if the node is not found or does not have a VTEP IP (i.e., is not an edge/leaf node).
func (s *TopologyState) GetNodeVTEP(nodeName string) (string, error) {
	node, ok := s.Nodes[nodeName]
	if !ok {
		return "", fmt.Errorf("node %q not found", nodeName)
	}
	if node.VTEPIP == "" {
		return "", fmt.Errorf("node %q does not have a VTEP IP", nodeName)
	}
	return node.VTEPIP, nil
}

// GetLinkIP returns the IP address of nodeName's interface facing peerName, for the given IP family.
// It returns an error if the node or peer interface is not found, or if the requested IP family is not configured.
func (s *TopologyState) GetLinkIP(nodeName, peerName string, family IPFamily) (string, error) {
	node, ok := s.Nodes[nodeName]
	if !ok {
		return "", fmt.Errorf("node %q not found", nodeName)
	}
	for _, iface := range node.Interfaces {
		if iface.PeerNode == peerName {
			switch family {
			case IPv4:
				if iface.IPv4 == "" {
					return "", fmt.Errorf("no IPv4 address on %s interface facing %s", nodeName, peerName)
				}
				return iface.IPv4, nil
			case IPv6:
				if iface.IPv6 == "" {
					return "", fmt.Errorf("no IPv6 address on %s interface facing %s", nodeName, peerName)
				}
				return iface.IPv6, nil
			default:
				return "", fmt.Errorf("unknown IP family %d", family)
			}
		}
	}
	return "", fmt.Errorf("no interface on %s facing peer %s", nodeName, peerName)
}

// FindIPOwner searches all node interfaces for an IP address containing the given IP string.
// It returns the node name and interface name that owns the IP, or an error if not found.
func (s *TopologyState) FindIPOwner(ip string) (nodeName, iface string, err error) {
	for nName, node := range s.Nodes {
		for ifName, ifState := range node.Interfaces {
			if strings.Contains(ifState.IPv4, ip) || strings.Contains(ifState.IPv6, ip) {
				return nName, ifName, nil
			}
		}
	}
	return "", "", fmt.Errorf("no interface found with IP containing %q", ip)
}

// GetNodesByPattern returns a sorted list of node names matching the given regex pattern.
func (s *TopologyState) GetNodesByPattern(pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}
	var matches []string
	for name := range s.Nodes {
		if re.MatchString(name) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches, nil
}
