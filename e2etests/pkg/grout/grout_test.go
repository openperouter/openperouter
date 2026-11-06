// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
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

func TestGetRoute(t *testing.T) {
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
			route, err := GetRoute(routeExecutor(t, routesOutput, nexthopsOutput), "red", tc.prefix)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(route, tc.expected) {
				t.Fatalf("expected %+v, got %+v", tc.expected, route)
			}
		})
	}
}

func TestGetRouteErrors(t *testing.T) {
	tests := []struct {
		name     string
		routes   string
		nexthops string
		wantErr  string
	}{
		{
			name:     "invalid routes json",
			routes:   "not json",
			nexthops: nexthopsOutput,
			wantErr:  "failed to parse grout route output: invalid character 'o' in literal null (expecting 'u')",
		},
		{
			name:     "invalid nexthops json",
			routes:   routesOutput,
			nexthops: "not json",
			wantErr:  "failed to parse grout nexthop output: invalid character 'o' in literal null (expecting 'u')",
		},
		{
			name:     "route without nexthop id",
			routes:   `[{"destination":"192.169.20.0/24","next_hop":"type=group"}]`,
			nexthops: nexthopsOutput,
			wantErr:  `invalid nexthop id "" for route 192.169.20.0/24: strconv.Atoi: parsing "": invalid syntax`,
		},
		{
			name:     "missing route nexthop",
			routes:   routesOutput,
			nexthops: `[]`,
			wantErr:  "failed to resolve nexthop for route 192.169.20.0/24: nexthop 55 not found",
		},
		{
			name:     "missing ecmp member after valid member",
			routes:   routesOutput,
			nexthops: strings.Replace(nexthopsOutput, "id(57/1)", "id(58/1)", 1),
			wantErr:  "failed to resolve nexthop for route 192.169.20.0/24: member nexthop 58 not found in group 55",
		},
		{
			name:     "invalid ecmp member id after valid member",
			routes:   routesOutput,
			nexthops: strings.Replace(nexthopsOutput, "id(57/1)", "id(invalid/1)", 1),
			wantErr: "failed to resolve nexthop for route 192.169.20.0/24: " +
				`invalid member "id(invalid/1)" in nexthop group 55`,
		},
		{
			name:     "malformed ecmp member after valid member",
			routes:   routesOutput,
			nexthops: strings.Replace(nexthopsOutput, "id(57/1)", "invalid", 1),
			wantErr: "failed to resolve nexthop for route 192.169.20.0/24: " +
				`invalid member "invalid" in nexthop group 55`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			route, err := GetRoute(routeExecutor(t, tc.routes, tc.nexthops), "red", "192.169.20.0/24")
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}
			if route != nil {
				t.Fatalf("expected no route on error, got %+v", route)
			}
		})
	}
}

func TestGroutPortForName(t *testing.T) {
	execErr := errors.New("command failed")
	tests := []struct {
		name     string
		output   string
		execErr  error
		expected GroutPort
		wantErr  string
	}{
		{
			name:     "port parameters",
			output:   `{"devargs":"0000:00:01.0","n_rxq":2,"rxq_size":512}`,
			expected: GroutPort{Devargs: "0000:00:01.0", RXQueues: 2, QSize: 512},
		},
		{
			name:    "command error",
			output:  "port not found",
			execErr: execErr,
			wantErr: "failed to show grout port u_test: command failed. Output: port not found",
		},
		{
			name:    "invalid json",
			output:  "{",
			wantErr: "failed to parse grout port u_test: unexpected end of JSON input. Output: {",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exec := routerExecFunc(func(cmd string, args ...string) (string, error) {
				if cmd != "grcli" || !reflect.DeepEqual(args, []string{"--json", "interface", "show", "name", "u_test"}) {
					t.Fatalf("unexpected command: %s %v", cmd, args)
				}
				return tc.output, tc.execErr
			})
			port, err := GroutPortForName(exec, "u_test")
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr) {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}
			if tc.execErr != nil && !errors.Is(err, tc.execErr) {
				t.Fatalf("expected wrapped error %v, got %v", tc.execErr, err)
			}
			if port != tc.expected {
				t.Fatalf("expected %+v, got %+v", tc.expected, port)
			}
		})
	}
}

func routeExecutor(t *testing.T, routes, nexthops string) routerExecFunc {
	t.Helper()
	return func(cmd string, args ...string) (string, error) {
		if cmd != "grcli" {
			return "", fmt.Errorf("unexpected command %q", cmd)
		}
		switch {
		case reflect.DeepEqual(args, []string{"--json", "route", "show", "vrf", "red"}):
			return routes, nil
		case reflect.DeepEqual(args, []string{"--json", "nexthop", "show"}):
			return nexthops, nil
		default:
			return "", fmt.Errorf("unexpected arguments %v", args)
		}
	}
}

type routerExecFunc func(cmd string, args ...string) (string, error)

func (r routerExecFunc) Exec(cmd string, args ...string) (string, error) {
	return r(cmd, args...)
}

func (r routerExecFunc) Name() string {
	return "test-router"
}

func (r routerExecFunc) NodeName() string {
	return "test-node"
}
