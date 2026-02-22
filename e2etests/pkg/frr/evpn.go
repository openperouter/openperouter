// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

func EVPNInfo(exec executor.Executor) (EVPNData, error) {
	res, err := exec.Exec("vtysh", "-c", "show bgp l2vpn evpn json")
	if err != nil {
		return EVPNData{}, fmt.Errorf("failed to query l2vpn evpn: %w. Output: %s", err, res)
	}

	evpnInfo, err := parseL2VPNEVPN([]byte(res))
	if err != nil {
		return EVPNData{}, errors.Join(err, fmt.Errorf("Failed to parse l2vpn evpn: %w", err))
	}
	return evpnInfo, nil
}

type EVPNData struct {
	BgpTableVersion  int       `json:"bgpTableVersion"`
	BgpLocalRouterId string    `json:"bgpLocalRouterId"`
	DefaultLocPrf    int       `json:"defaultLocPrf"`
	LocalAS          int       `json:"localAS"`
	Entries          []RdEntry `json:"-"` // handled manually
	NumPrefix        int       `json:"numPrefix"`
	TotalPrefix      int       `json:"totalPrefix"`
}

// ContainsType5RouteForVNI tells if the given prefix is received as type 5 route
// with the given vtep as next hop.
func (e *EVPNData) ContainsType5RouteForVNI(prefix string, vtep string, vni int) bool {
	for _, path := range e.allPaths() {
		routePrefix := fmt.Sprintf("%s/%d", path.IP, path.IPLen)
		if routePrefix == prefix && pathHasVTEPAndVNI(path, vtep, vni) {
			return true
		}
	}
	return false
}

// ContainsType2MACIPRouteForVNI checks if a Type 2 MAC+IP route exists for the given VNI.
// Type 2 routes have RouteType == 2 and include both MAC and IP information.
// The ip parameter should be the bare IP address (e.g., "192.168.1.10").
func (e *EVPNData) ContainsType2MACIPRouteForVNI(ip string, vtep string, vni int) bool {
	isType2MACIPRoute := func(p Path) bool {
		return p.RouteType == 2 && p.IPLen > 0
	}
	for _, path := range e.matchingPaths(isType2MACIPRoute) {
		if path.IP == ip && pathHasVTEPAndVNI(path, vtep, vni) {
			return true
		}
	}
	return false
}

// allPaths returns a flat slice of paths from all entries and prefixes.
func (e *EVPNData) allPaths() []Path {
	var paths []Path
	for _, entry := range e.Entries {
		for _, prefixEntry := range entry.Prefixes {
			for _, path := range prefixEntry.Paths {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

// matchingPaths returns a flat slice of paths from all entries and prefixes that match the predicate.
// If predicate is nil, all paths are returned.
func (e *EVPNData) matchingPaths(predicate func(Path) bool) []Path {
	var paths []Path
	for _, entry := range e.Entries {
		for _, prefixEntry := range entry.Prefixes {
			for _, path := range prefixEntry.Paths {
				if predicate == nil || predicate(path) {
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

// pathHasVTEPAndVNI checks if a path has the given VTEP as a nexthop and the given VNI in its extended community.
func pathHasVTEPAndVNI(path Path, vtep string, vni int) bool {
	pathVNIs, err := vnisFromExtendedCommunity(path.ExtendedCommunity.String)
	if err != nil {
		return false
	}
	for _, n := range path.Nexthops {
		if n.IP == vtep && containsVNI(pathVNIs, vni) {
			return true
		}
	}
	return false
}

func (e *EVPNData) ContainsType5RouteForRT(prefix string, vtep string, rt []string) bool {
	for _, entry := range e.Entries {
		for _, prefixEntry := range entry.Prefixes {
			for _, path := range prefixEntry.Paths {
				routePrefix := fmt.Sprintf("%s/%d", path.IP, path.IPLen)
				if routePrefix == prefix {
					for _, n := range path.Nexthops {
						if n.IP == vtep &&
							routeTargetFromExtendedCommunity(path.ExtendedCommunity.String, rt) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

type RdEntry struct {
	RD       string            `json:"rd"`
	Prefixes map[string]Prefix `json:"-"` // handled manually
}

type ExtendedCommunity struct {
	String string `json:"string"`
}

type Nexthop struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Afi      string `json:"afi"`
	Used     bool   `json:"used"`
}

type Path struct {
	Valid             bool              `json:"valid"`
	Bestpath          bool              `json:"bestpath"`
	SelectionReason   string            `json:"selectionReason"`
	PathFrom          string            `json:"pathFrom"`
	RouteType         int               `json:"routeType"`
	EthTag            int               `json:"ethTag"`
	IPLen             int               `json:"ipLen"`
	IP                string            `json:"ip"`
	Metric            int               `json:"metric"`
	Weight            int               `json:"weight"`
	PeerId            string            `json:"peerId"`
	Path              string            `json:"path"`
	Origin            string            `json:"origin"`
	ExtendedCommunity ExtendedCommunity `json:"extendedCommunity"`
	Nexthops          []Nexthop         `json:"nexthops"`
}

type Prefix struct {
	Prefix    string `json:"prefix"`
	PrefixLen int    `json:"prefixLen"`
	Paths     []Path `json:"paths"`
}

func parseL2VPNEVPN(data []byte) (EVPNData, error) {
	res := EVPNData{
		Entries: make([]RdEntry, 0),
	}

	if err := json.Unmarshal(data, &res); err != nil {
		return EVPNData{}, fmt.Errorf("error unmarshalling JSON: %v", err)
	}

	var dynamicData map[string]json.RawMessage
	if err := json.Unmarshal(data, &dynamicData); err != nil {
		return EVPNData{}, fmt.Errorf("error unmarshalling JSON: %v", err)
	}

	for k, v := range dynamicData {
		if strings.Contains(k, ":") { // Route Distinguisher
			entry := RdEntry{
				RD:       k,
				Prefixes: make(map[string]Prefix),
			}

			var rd map[string]json.RawMessage
			if err := json.Unmarshal(v, &rd); err != nil {
				return EVPNData{}, fmt.Errorf("error unmarshalling JSON: %v", err)
			}

			for k, v := range rd {
				if strings.Contains(k, ":") { // Route
					var prefix Prefix
					if err := json.Unmarshal(v, &prefix); err != nil {
						return EVPNData{}, fmt.Errorf("error unmarshalling JSON: %v", err)
					}
					entry.Prefixes[k] = prefix
				}
			}

			res.Entries = append(res.Entries, entry)
		}
	}

	return res, nil
}

func vnisFromExtendedCommunity(extendedCommunity string) ([]int, error) {
	// extended community can look like:
	// "RT:64514:200 ET:8 Rmac:22:2e:e4:41:7f:5c"
	// or with multiple RTs:
	// "RT:64514:100 RT:64514:110 ET:8 Rmac:f6:5f:31:5a:33:a2"

	if extendedCommunity == "" {
		return nil, fmt.Errorf("empty extended community string")
	}

	parts := strings.Split(extendedCommunity, " ")
	if len(parts) == 0 {
		return nil, fmt.Errorf("no parts found in extended community: %s", extendedCommunity)
	}

	var vnis []int
	for _, part := range parts {
		if !strings.HasPrefix(part, "RT:") {
			continue
		}
		rtValues := strings.Split(part, ":")
		if len(rtValues) < 3 {
			continue
		}
		vni, err := strconv.Atoi(rtValues[2])
		if err != nil {
			continue
		}
		vnis = append(vnis, vni)
	}

	if len(vnis) == 0 {
		return nil, fmt.Errorf("no VNIs found in extended community: %s", extendedCommunity)
	}
	return vnis, nil
}

func containsVNI(vnis []int, vni int) bool {
	for _, v := range vnis {
		if v == vni {
			return true
		}
	}
	return false
}

func routeTargetFromExtendedCommunity(extendedCommunity string, routeTargets []string) bool {
	parts := strings.Split(extendedCommunity, " ")
	for part := range parts {
		for _, routeTarget := range routeTargets {
			routeTargetValue := "RT:" + routeTarget
			if parts[part] == routeTargetValue {
				return true
			}
		}
	}

	return false
}
