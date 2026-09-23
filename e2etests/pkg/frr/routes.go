// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
)

// RouteProtocol identifies the routing protocol used to learn a route.
type RouteProtocol string

const (
	ISIS      RouteProtocol = "isis"
	Connected RouteProtocol = "connected"
)

// FrrRoutes maps prefix strings to their route entries, as returned by "show <af> route <protocol> json".
type FrrRoutes map[string][]RouteEntry

// RouteEntry represents a single route for a given prefix.
type RouteEntry struct {
	Prefix       string         `json:"prefix"`
	PrefixLen    int            `json:"prefixLen"`
	Protocol     string         `json:"protocol"`
	VRFId        int            `json:"vrfId"`
	VRFName      string         `json:"vrfName"`
	Selected     bool           `json:"selected"`
	DestSelected bool           `json:"destSelected"`
	Distance     int            `json:"distance"`
	Metric       int            `json:"metric"`
	Installed    bool           `json:"installed"`
	Table        int            `json:"table"`
	Uptime       string         `json:"uptime"`
	Nexthops     []RouteNexthop `json:"nexthops"`
}

// RouteNexthop represents a nexthop entry within a route.
type RouteNexthop struct {
	IP             string `json:"ip"`
	AFI            string `json:"afi"`
	InterfaceIndex int    `json:"interfaceIndex"`
	InterfaceName  string `json:"interfaceName"`
	Active         bool   `json:"active"`
	FIB            bool   `json:"fib"`
	Weight         int    `json:"weight"`
}

// GetRoutes runs "show <af> route <protocol> json" and parses the result.
func GetRoutes(exec executor.Executor, protocol RouteProtocol, family ipfamily.Family) (FrrRoutes, error) {
	if family == ipfamily.IPv4 {
		family = "ip"
	}
	cmd := fmt.Sprintf("show %s route %s json", family, protocol)
	res, err := exec.Exec("vtysh", "-c", cmd)
	if err != nil {
		return FrrRoutes{}, fmt.Errorf("failed to query `%s`: %w. Output: %s",
			cmd, err, res)
	}

	routeInfo, err := parseFrrRoutes([]byte(res))
	if err != nil {
		return FrrRoutes{}, fmt.Errorf("failed to parse output of `%s`: %w. Output: %s",
			cmd, err, res)
	}
	return routeInfo, nil
}

func parseFrrRoutes(data []byte) (FrrRoutes, error) {
	res := FrrRoutes{}
	if err := json.Unmarshal(data, &res); err != nil {
		return FrrRoutes{}, fmt.Errorf("error unmarshalling JSON: %w", err)
	}

	return res, nil
}
