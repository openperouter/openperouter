// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
)

type L3VPNData struct {
	VRFID         int    `json:"vrfId"`
	VRFName       string `json:"vrfName"`
	TableVersion  int    `json:"tableVersion"`
	RouterId      string `json:"routerId"`
	DefaultLocPrf int    `json:"defaultLocPrf"`
	LocalAS       int    `json:"localAS"`
	Routes        Route  `json:"routes"`
	TotalRoutes   int    `json:"totalRoutes"`
	TotalPaths    int    `json:"totalPaths"`
}

type Route struct {
	RouteDistinguishers RouteDistinguisherMap `json:"routeDistinguishers"`
}

type RouteDistinguisher string

type RouteDistinguisherMap map[RouteDistinguisher]BGPPathMap

type BGPPrefix string

type BGPPathMap map[BGPPrefix][]BGPPath

type BGPPath struct {
	ASPath            ASPath            `json:"aspath"`
	Origin            string            `json:"origin"`
	Metric            int               `json:"metric"`
	Valid             bool              `json:"valid"`
	Bestpath          BestPath          `json:"bestpath"`
	ExtendedCommunity ExtendedCommunity `json:"extendedCommunity"`
	Nexthops          []Nexthop         `json:"nexthops"`
	Peer              Peer              `json:"peer"`
}

type Peer struct {
	PeerID   string `json:"peerId"`
	RouterID string `json:"routerId"`
	Hostname string `json:"hostname"`
	Type     string `json:"type"`
}

type ASPath struct {
	String string `json:"string"`
}

type BestPath struct {
	Overall         bool   `json:"overall"`
	SelectionReason string `json:"selectionReason"`
}

func L3VPNInfo(exec executor.Executor, family ipfamily.Family) (L3VPNData, error) {
	res, err := exec.Exec("vtysh", "-c", fmt.Sprintf("show bgp %s vpn detail json", family))
	if err != nil {
		return L3VPNData{}, fmt.Errorf("failed to query `show bgp %s vpn detail json`: %w. Output: %s",
			family, err, res)
	}

	l3vpnInfo, err := parseBGPVPNtoL3VPN([]byte(res))
	if err != nil {
		return L3VPNData{}, fmt.Errorf("failed to parse output of `show bgp %s vpn detail json`: %w. Output: %s",
			family, err, res)
	}
	return l3vpnInfo, nil
}

func (l3 L3VPNData) ContainsBGPRouteForL3VPN(prefix string, routerID string, importRTs []v1alpha1.RouteTarget) bool {
	for _, pathMap := range l3.Routes.RouteDistinguishers {
		for bgpPrefix, bgpPaths := range pathMap {
			if string(bgpPrefix) != prefix {
				continue
			}
			for _, bgpPath := range bgpPaths {
				rt := v1alpha1.RouteTarget(strings.TrimPrefix(bgpPath.ExtendedCommunity.String, "RT:"))
				if !slices.Contains(importRTs, rt) {
					continue
				}
				if bgpPath.Peer.RouterID != routerID {
					continue
				}
				return true
			}
		}
	}
	return false
}

func parseBGPVPNtoL3VPN(data []byte) (L3VPNData, error) {
	res := L3VPNData{}
	if err := json.Unmarshal(data, &res); err != nil {
		return L3VPNData{}, fmt.Errorf("error unmarshalling JSON: %v", err)
	}

	return res, nil
}
