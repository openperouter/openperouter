// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

// Validate checks the consistency of a ClabTopology and an EnvironmentConfig.
// It returns a list of non-fatal warnings and an error if any hard constraint
// is violated.
func Validate(clab *ClabTopology, config *EnvironmentConfig) (warnings []string, err error) {
	// 1. Compile all patterns (node patterns and peer patterns).
	nodeRegexps := make([]*regexp.Regexp, len(config.Nodes))
	for i, nc := range config.Nodes {
		re, compileErr := regexp.Compile("^" + nc.Pattern + "$")
		if compileErr != nil {
			return nil, fmt.Errorf("invalid node pattern %q: %w", nc.Pattern, compileErr)
		}
		nodeRegexps[i] = re
	}
	for _, nc := range config.Nodes {
		for _, peer := range nc.BGP.Peers {
			if _, compileErr := regexp.Compile("^" + peer.Pattern + "$"); compileErr != nil {
				return nil, fmt.Errorf("invalid peer pattern %q: %w", peer.Pattern, compileErr)
			}
		}
	}

	// Build set of interfaces per clab node from links.
	nodeInterfaces := buildNodeInterfaces(clab)

	// Determine which kinds to skip for matching purposes.
	skipKinds := map[string]bool{
		"bridge":        true,
		"ext-container": true,
	}

	// 2. Overlapping pattern detection (FR-022) and
	// 3. Unmatched node warnings (FR-020).
	patternMatchCount := make([]int, len(config.Nodes))

	for nodeName, node := range clab.Topology.Nodes {
		if node.Kind == "bridge" {
			continue
		}

		var matchingPatterns []string
		for i, re := range nodeRegexps {
			if re.MatchString(nodeName) {
				matchingPatterns = append(matchingPatterns, config.Nodes[i].Pattern)
				patternMatchCount[i]++
			}
		}

		if len(matchingPatterns) > 1 {
			sort.Strings(matchingPatterns)
			return warnings, fmt.Errorf("node %q matches multiple patterns: %s",
				nodeName, strings.Join(matchingPatterns, ", "))
		}

		// Warn for unmatched nodes, but skip bridge, ext-container kinds.
		if len(matchingPatterns) == 0 && !skipKinds[node.Kind] {
			warnings = append(warnings, fmt.Sprintf("node %q does not match any config pattern", nodeName))
		}
	}

	// 4. Unmatched pattern warnings.
	for i, nc := range config.Nodes {
		if patternMatchCount[i] == 0 {
			warnings = append(warnings, fmt.Sprintf("config pattern %q does not match any clab node", nc.Pattern))
		}
	}

	// 5. Interface existence check (FR-021).
	for _, nc := range config.Nodes {
		re := regexp.MustCompile("^" + nc.Pattern + "$")
		for nodeName := range clab.Topology.Nodes {
			if !re.MatchString(nodeName) {
				continue
			}
			ifaces := nodeInterfaces[nodeName]
			for vrfName, vrf := range nc.VRFs {
				for _, iface := range vrf.Interfaces {
					if !ifaces[iface] {
						return warnings, fmt.Errorf("interface %q (VRF %q) not found on node %q",
							iface, vrfName, nodeName)
					}
				}
			}
		}
	}

	// 6. VNI uniqueness.
	vniSeen := map[int]string{} // VNI -> "pattern/vrfName"
	for _, nc := range config.Nodes {
		for vrfName, vrf := range nc.VRFs {
			if vrf.VNI == 0 {
				continue
			}
			key := fmt.Sprintf("%s/%s", nc.Pattern, vrfName)
			if prev, ok := vniSeen[vrf.VNI]; ok {
				return warnings, fmt.Errorf("duplicate VNI %d in %s and %s", vrf.VNI, prev, key)
			}
			vniSeen[vrf.VNI] = key
		}
	}

	// 7. Role constraints: transit nodes must not have VRFs.
	for _, nc := range config.Nodes {
		if nc.Role == "transit" && len(nc.VRFs) > 0 {
			return warnings, fmt.Errorf("transit node pattern %q must not have VRFs defined", nc.Pattern)
		}
	}

	// 8. IP range validation.
	cidrs := []struct {
		name  string
		value string
	}{
		{"pointToPoint.ipv4", config.IPRanges.PointToPoint.IPv4},
		{"pointToPoint.ipv6", config.IPRanges.PointToPoint.IPv6},
		{"broadcast.ipv4", config.IPRanges.Broadcast.IPv4},
		{"broadcast.ipv6", config.IPRanges.Broadcast.IPv6},
		{"vtep", config.IPRanges.VTEP},
		{"routerID", config.IPRanges.RouterID},
	}
	for _, c := range cidrs {
		if c.value == "" {
			continue
		}
		if _, _, parseErr := net.ParseCIDR(c.value); parseErr != nil {
			return warnings, fmt.Errorf("invalid CIDR %q in ipRanges.%s: %w", c.value, c.name, parseErr)
		}
	}

	sort.Strings(warnings)
	return warnings, nil
}

// buildNodeInterfaces returns a map from node name to the set of interface
// names that appear as link endpoints for that node.
func buildNodeInterfaces(clab *ClabTopology) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	for _, link := range clab.Topology.Links {
		eps, err := link.ParseEndpoints()
		if err != nil {
			continue
		}
		for _, ep := range eps {
			if result[ep.Node] == nil {
				result[ep.Node] = make(map[string]bool)
			}
			result[ep.Node][ep.Interface] = true
		}
	}
	return result
}
