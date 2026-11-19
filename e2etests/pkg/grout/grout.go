// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
)

type GroutPort struct {
	Devargs  string `json:"devargs"`
	RXQueues int32  `json:"n_rxq"`
	QSize    int32  `json:"rxq_size"`
}

type groutRoute struct {
	Destination string `json:"destination"`
	NextHop     string `json:"next_hop"`
}

type groutNexthop struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Info string `json:"info"`
}

// GetRoute is the grout counterpart of frr.GetKernelRoute: it reads the
// route for prefix from the grout FIB of vrf, and translates the SRv6
// encapsulation to the kernel representation.
func GetRoute(exec openperouter.RouterExecutor, vrf string, prefix string) (*frr.IPRoute, error) {
	routesOutput, err := exec.Exec("grcli", "--json", "route", "show", "vrf", vrf)
	if err != nil {
		return nil, fmt.Errorf("failed to show grout routes in vrf %s: %w. Output: %s", vrf, err, routesOutput)
	}

	// The route only references its nexthop: the SRv6 encapsulation is a property
	// of the nexthop (or of the members of a nexthop group, for ECMP routes), and
	// it is shown only by "nexthop show". VRF routes reference nexthops living in
	// the default VRF or in no VRF at all, so nexthops are not filtered by vrf.
	nexthopsOutput, err := exec.Exec("grcli", "--json", "nexthop", "show")
	if err != nil {
		return nil, fmt.Errorf("failed to show grout nexthops: %w. Output: %s", err, nexthopsOutput)
	}

	var routes []groutRoute
	if err := json.Unmarshal([]byte(routesOutput), &routes); err != nil {
		return nil, fmt.Errorf("failed to parse grout route output: %w", err)
	}
	var nexthops []groutNexthop
	if err := json.Unmarshal([]byte(nexthopsOutput), &nexthops); err != nil {
		return nil, fmt.Errorf("failed to parse grout nexthop output: %w", err)
	}

	return groutRouteForPrefix(routes, nexthops, prefix)
}

func GroutPortForName(router openperouter.RouterExecutor, portName string) (GroutPort, error) {
	out, err := router.Exec("grcli", "--json", "interface", "show", "name", portName)
	if err != nil {
		return GroutPort{}, fmt.Errorf("failed to show grout port %s: %w. Output: %s", portName, err, out)
	}
	var port GroutPort
	if err := json.Unmarshal([]byte(out), &port); err != nil {
		return GroutPort{}, fmt.Errorf("failed to parse grout port %s: %w. Output: %s", portName, err, out)
	}
	return port, nil
}

func groutRouteForPrefix(routes []groutRoute, nexthops []groutNexthop, prefix string) (*frr.IPRoute, error) {
	route := routeForPrefix(routes, prefix)
	if route == nil {
		return nil, nil
	}

	rawID := parseNextHopFields(route.NextHop)["id"]
	id, err := strconv.Atoi(rawID)
	if err != nil {
		return nil, fmt.Errorf("invalid nexthop id %q for route %s: %w", rawID, prefix, err)
	}

	resolved, err := nextHopMembersForID(nexthopsByID(nexthops), id)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve nexthop for route %s: %w", prefix, err)
	}

	result := &frr.IPRoute{Destination: route.Destination}
	for _, nh := range resolved {
		if nh.Type != "SRv6" {
			continue
		}
		result.Nexthops = append(result.Nexthops, frr.IPNexthop{
			Encap: frr.IPEncap{
				EncapType: "seg6",
				EncapMode: groutEncapToFRR(parseNextHopFields(nh.Info)["encap"]),
			},
		})
	}
	return result, nil
}

func routeForPrefix(routes []groutRoute, prefix string) *groutRoute {
	for _, route := range routes {
		if route.Destination == prefix {
			return &route
		}
	}
	return nil
}

func nexthopsByID(nexthops []groutNexthop) map[int]groutNexthop {
	res := map[int]groutNexthop{}
	for _, nh := range nexthops {
		// Nexthops learned from neighbors all share id 0 and are never referenced by routes.
		if nh.ID == 0 {
			continue
		}
		res[nh.ID] = nh
	}
	return res
}

// nextHopMembersForID returns the members of a nexthop group, or the nexthop itself
// when it is not a group.
// sample group info: "id(56/1) id(57/1) "
func nextHopMembersForID(nexthops map[int]groutNexthop, id int) ([]groutNexthop, error) {
	nh, ok := nexthops[id]
	if !ok {
		return nil, fmt.Errorf("nexthop %d not found", id)
	}
	if nh.Type != "group" {
		return []groutNexthop{nh}, nil
	}

	var members []groutNexthop
	for member := range strings.FieldsSeq(nh.Info) {
		memberID, ok := nextHopIDFromGroupMember(member)
		if !ok {
			return nil, fmt.Errorf("invalid member %q in nexthop group %d", member, id)
		}
		m, ok := nexthops[memberID]
		if !ok {
			return nil, fmt.Errorf("member nexthop %d not found in group %d", memberID, id)
		}
		members = append(members, m)
	}
	return members, nil
}

// nextHopIDFromGroupMember extracts the nexthop id from a "id(<id>/<weight>)" group member.
func nextHopIDFromGroupMember(member string) (int, bool) {
	inner, ok := strings.CutPrefix(member, "id(")
	if !ok {
		return 0, false
	}
	rawID, _, _ := strings.Cut(inner, "/")
	id, err := strconv.Atoi(rawID)
	if err != nil {
		return 0, false
	}
	return id, true
}

// parseNextHopFields parses the key=value pairs grout prints to describe a nexthop.
// sample output:
// "type=SRv6-local id=18 iface=u_toswitch2 vrf=main origin=zebra behavior=end.x endx_addr=fe80::1 flavor=next-csid"
func parseNextHopFields(nextHop string) map[string]string {
	fields := map[string]string{}
	for part := range strings.FieldsSeq(nextHop) {
		if key, value, ok := strings.Cut(part, "="); ok {
			fields[key] = value
		}
	}
	return fields
}

var groutEncapModes = map[string]frr.EncapMode{
	"h.encaps":     frr.HEncaps,
	"h.encaps.red": frr.HEncapsRed,
}

func groutEncapToFRR(groutMode string) string {
	if mode, ok := groutEncapModes[groutMode]; ok {
		return string(mode)
	}
	return groutMode
}
