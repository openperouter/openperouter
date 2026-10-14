// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"reflect"
	"testing"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
)

const nexthopsOutput = `[
{"vrf":"main","id":6,"origin":"zebra","iface":"tunnel-ep","type":"L3","info":"family=unspec flags=link"},
{"vrf":"main","id":0,"origin":"learn","iface":"u_toswitch1","type":"L3","info":"family=ipv4 addr=192.168.11.2 state=reachable mac=aa:c1:ab:0c:ef:c9 flags=neigh"},
{"vrf":"red","id":24,"origin":"zebra","iface":"red","type":"SRv6-local","info":"behavior=end.dt46 flavor=next-csid block_bits=32 csid_bits=16 out_vrf=5"},
{"vrf":"main","id":56,"origin":"zebra","iface":"u_toswitch1","type":"SRv6","info":"encap=h.encaps seglist=fd00:0:10:e001::"},
{"vrf":"main","id":57,"origin":"zebra","iface":"u_toswitch2","type":"SRv6","info":"encap=h.encaps seglist=fd00:0:10:e001::"},
{"vrf":"","id":55,"origin":"zebra","iface":"","type":"group","info":"id(56/1) id(57/1) "},
{"vrf":"main","id":61,"origin":"zebra","iface":"u_toswitch1","type":"SRv6","info":"encap=h.encaps.red seglist=fd00:0:10:e002::"}
]`

const routesOutput = `[
{"vrf":"red","family":"ipv4","destination":"192.169.20.0/24","origin":"bgp","next_hop":"type=group id=55 origin=zebra"},
{"vrf":"red","family":"ipv4","destination":"192.169.30.0/24","origin":"bgp","next_hop":"type=SRv6 id=61 iface=u_toswitch1 vrf=main origin=zebra encap=h.encaps.red seglist=fd00:0:10:e002::"},
{"vrf":"red","family":"ipv4","destination":"192.169.10.0/24","origin":"link","next_hop":"type=L3 id=6 iface=tunnel-ep vrf=main origin=zebra family=unspec flags=link"}
]`

func TestParseGroutRoute(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		expected *frr.IPRoute
	}{
		{
			name:   "ecmp route through a nexthop group",
			prefix: "192.169.20.0/24",
			expected: &frr.IPRoute{
				Destination: "192.169.20.0/24",
				Nexthops: []frr.IPNexthop{
					{Encap: frr.IPEncap{EncapType: "seg6", EncapMode: string(frr.HEncaps)}},
					{Encap: frr.IPEncap{EncapType: "seg6", EncapMode: string(frr.HEncaps)}},
				},
			},
		},
		{
			name:   "single srv6 nexthop",
			prefix: "192.169.30.0/24",
			expected: &frr.IPRoute{
				Destination: "192.169.30.0/24",
				Nexthops: []frr.IPNexthop{
					{Encap: frr.IPEncap{EncapType: "seg6", EncapMode: string(frr.HEncapsRed)}},
				},
			},
		},
		{
			name:     "route without srv6 encapsulation",
			prefix:   "192.169.10.0/24",
			expected: &frr.IPRoute{Destination: "192.169.10.0/24"},
		},
		{
			name:     "route not found",
			prefix:   "10.0.0.0/24",
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route, err := parseGroutRoute(routesOutput, nexthopsOutput, tc.prefix)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(route, tc.expected) {
				t.Fatalf("expected %+v, got %+v", tc.expected, route)
			}
		})
	}
}

func TestParseGroutRouteErrors(t *testing.T) {
	tests := []struct {
		name     string
		routes   string
		nexthops string
	}{
		{
			name:     "invalid routes json",
			routes:   "not json",
			nexthops: nexthopsOutput,
		},
		{
			name:     "invalid nexthops json",
			routes:   routesOutput,
			nexthops: "not json",
		},
		{
			name:     "route without nexthop id",
			routes:   `[{"destination":"192.169.20.0/24","next_hop":"type=group"}]`,
			nexthops: nexthopsOutput,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseGroutRoute(tc.routes, tc.nexthops, "192.169.20.0/24"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
