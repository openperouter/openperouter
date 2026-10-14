// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/onsi/gomega"
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

	return parseGroutRoute(routesOutput, nexthopsOutput, prefix)
}

func ShowGroutPort(g gomega.Gomega, router openperouter.RouterExecutor, portName string) GroutPort {
	var port GroutPort
	out, err := router.Exec("grcli", "--json", "interface", "show", "name", portName)
	g.Expect(err).To(gomega.Succeed(), out)
	g.Expect(json.Unmarshal([]byte(out), &port)).To(gomega.Succeed(), out)
	return port
}

func parseGroutRoute(routesOutput, nexthopsOutput, prefix string) (*frr.IPRoute, error) {
	route, err := findRoute(routesOutput, prefix)
	if err != nil {
		return nil, err
	}
	if route == nil {
		return nil, nil
	}

	nexthops, err := parseNexthopsByID(nexthopsOutput)
	if err != nil {
		return nil, err
	}

	rawID := parseNextHopFields(route.NextHop)["id"]
	id, err := strconv.Atoi(rawID)
	if err != nil {
		return nil, fmt.Errorf("invalid nexthop id %q for route %s: %w", rawID, prefix, err)
	}

	result := &frr.IPRoute{Destination: route.Destination}
	for _, nh := range resolveNexthop(nexthops, id) {
		if nh.Type != "SRv6" {
			continue
		}
		result.Nexthops = append(result.Nexthops, frr.IPNexthop{
			Encap: frr.IPEncap{
				EncapType: "seg6",
				EncapMode: groutEncapToKernel(parseNextHopFields(nh.Info)["encap"]),
			},
		})
	}
	return result, nil
}

func findRoute(output string, prefix string) (*groutRoute, error) {
	var routes []groutRoute
	if err := json.Unmarshal([]byte(output), &routes); err != nil {
		return nil, fmt.Errorf("failed to parse grout route output: %w", err)
	}
	for _, route := range routes {
		if route.Destination == prefix {
			return &route, nil
		}
	}
	return nil, nil
}

func parseNexthopsByID(output string) (map[int]groutNexthop, error) {
	var nexthops []groutNexthop
	if err := json.Unmarshal([]byte(output), &nexthops); err != nil {
		return nil, fmt.Errorf("failed to parse grout nexthop output: %w", err)
	}
	res := map[int]groutNexthop{}
	for _, nh := range nexthops {
		// Nexthops learned from neighbors all share id 0 and are never referenced by routes.
		if nh.ID == 0 {
			continue
		}
		res[nh.ID] = nh
	}
	return res, nil
}

// resolveNexthop returns the members of a nexthop group, or the nexthop itself
// when it is not a group.
// sample group info: "id(56/1) id(57/1) "
func resolveNexthop(nexthops map[int]groutNexthop, id int) []groutNexthop {
	nh, ok := nexthops[id]
	if !ok {
		return nil
	}
	if nh.Type != "group" {
		return []groutNexthop{nh}
	}

	var members []groutNexthop
	for member := range strings.FieldsSeq(nh.Info) {
		memberID, ok := parseGroupMemberID(member)
		if !ok {
			continue
		}
		if m, ok := nexthops[memberID]; ok {
			members = append(members, m)
		}
	}
	return members
}

// parseGroupMemberID extracts the nexthop id from a "id(<id>/<weight>)" group member.
func parseGroupMemberID(member string) (int, bool) {
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

func groutEncapToKernel(groutMode string) string {
	if mode, ok := groutEncapModes[groutMode]; ok {
		return string(mode)
	}
	return groutMode
}
