// SPDX-License-Identifier:Apache-2.0

package frr

import "testing"

func TestContainsType5RouteWithRouterMAC(t *testing.T) {
	const mac = "02:00:00:00:00:01"
	matching := Path{
		Valid: true, RouteType: 5, IP: "192.0.2.1", IPLen: 32,
		Nexthops:          []Nexthop{{IP: "100.65.0.1"}},
		ExtendedCommunity: ExtendedCommunity{String: "RT:64514:100 ET:8 Rmac:" + mac},
	}
	tests := []struct {
		name   string
		change func(*Path)
		want   bool
	}{
		{name: "matching path", want: true},
		{name: "invalid path", change: func(p *Path) { p.Valid = false }},
		{name: "type 2", change: func(p *Path) { p.RouteType = 2 }},
		{name: "different prefix", change: func(p *Path) { p.IP = "192.0.2.2" }},
		{name: "different prefix length", change: func(p *Path) { p.IPLen = 24 }},
		{name: "different VTEP", change: func(p *Path) { p.Nexthops = []Nexthop{{IP: "100.65.0.2"}} }},
		{name: "different VNI", change: func(p *Path) {
			p.ExtendedCommunity.String = "RT:64514:200 ET:8 Rmac:" + mac
		}},
		{name: "different MAC", change: func(p *Path) {
			p.ExtendedCommunity.String = "RT:64514:100 ET:8 Rmac:02:00:00:00:00:02"
		}},
		{name: "missing MAC", change: func(p *Path) { p.ExtendedCommunity.String = "RT:64514:100 ET:8" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := matching
			if tt.change != nil {
				tt.change(&path)
			}
			// An unrelated path carrying the expected MAC must not satisfy the check.
			other := matching
			other.IP = "198.51.100.1"
			data := EVPNData{Entries: []RdEntry{{Prefixes: map[string]Prefix{
				"route": {Paths: []Path{path, other}},
			}}}}
			got := data.ContainsType5RouteWithRouterMAC("192.0.2.1/32", "100.65.0.1", "64514:100", mac)
			if got != tt.want {
				t.Errorf("ContainsType5RouteWithRouterMAC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContainsType5RouteWithRouterMACNormalization(t *testing.T) {
	data := EVPNData{Entries: []RdEntry{{Prefixes: map[string]Prefix{
		"route": {Paths: []Path{{
			Valid: true, RouteType: 5, IP: "192.0.2.1", IPLen: 32,
			Nexthops:          []Nexthop{{IP: "100.65.0.1"}},
			ExtendedCommunity: ExtendedCommunity{String: "RT:64514:100 ET:8 Rmac:02:ab:cd:ef:00:01"},
		}}},
	}}}}
	tests := []struct {
		name string
		mac  string
		want bool
	}{
		{name: "canonical", mac: "02:ab:cd:ef:00:01", want: true},
		{name: "uppercase", mac: "02:AB:CD:EF:00:01", want: true},
		{name: "mixed case", mac: "02:aB:Cd:eF:00:01", want: true},
		{name: "hyphenated", mac: "02-AB-CD-EF-00-01", want: true},
		{name: "different MAC", mac: "02:AB:CD:EF:00:02"},
		{name: "invalid", mac: "not-a-mac"},
		{name: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := data.ContainsType5RouteWithRouterMAC("192.0.2.1/32", "100.65.0.1", "64514:100", tt.mac)
			if got != tt.want {
				t.Errorf("ContainsType5RouteWithRouterMAC(%q) = %v, want %v", tt.mac, got, tt.want)
			}
		})
	}
}

func TestContainsType5RouteWithRouterMACRequiresSamePath(t *testing.T) {
	matchingPeer := Path{
		Valid: true, RouteType: 5, IP: "192.0.2.1", IPLen: 32,
		Nexthops:          []Nexthop{{IP: "100.65.0.1"}},
		ExtendedCommunity: ExtendedCommunity{String: "RT:64514:100 ET:8 Rmac:02:ab:cd:ef:00:02"},
	}
	tests := []struct {
		name      string
		vtep      string
		community string
	}{
		{
			name:      "MAC on a path from another VTEP",
			vtep:      "100.65.0.2",
			community: "RT:64514:100 ET:8 Rmac:02:ab:cd:ef:00:01",
		},
		{
			name:      "MAC on a path with another route target",
			vtep:      "100.65.0.1",
			community: "RT:64514:200 ET:8 Rmac:02:ab:cd:ef:00:01",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchingMAC := Path{
				Valid: true, RouteType: 5, IP: "192.0.2.1", IPLen: 32,
				Nexthops:          []Nexthop{{IP: tt.vtep}},
				ExtendedCommunity: ExtendedCommunity{String: tt.community},
			}
			data := EVPNData{Entries: []RdEntry{{Prefixes: map[string]Prefix{
				"[5]:[0]:[32]:[192.0.2.1]": {Paths: []Path{matchingPeer, matchingMAC}},
			}}}}
			if data.ContainsType5RouteWithRouterMAC("192.0.2.1/32", "100.65.0.1", "64514:100", "02:AB:CD:EF:00:01") {
				t.Fatal("matched VTEP and route target from one path with the router MAC from another")
			}
		})
	}
}
