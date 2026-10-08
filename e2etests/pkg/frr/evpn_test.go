// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

var data = []byte(`{
    "bgpTableVersion": 1,
    "bgpLocalRouterId": "192.168.1.1",
    "defaultLocPrf": 100,
    "localAS": 64520,
    "numPrefix": 3,
    "totalPrefix": 3,
    "192.168.20.1:2": {
        "[5]:[0]:[24]:[192.168.20.0]": {
            "prefix": "[5]:[0]:[24]:[192.168.20.0]",
            "prefixLen": 352,
            "paths": [
                {
                    "valid": true,
                    "bestpath": true,
                    "selectionReason": "First path received",
                    "pathFrom": "external",
                    "routeType": 5,
                    "ip": "192.168.20.0",
                    "nexthops": [
                        {
                            "ip": "192.168.20.1",
                            "hostname": "leafA",
                            "afi": "ipv4",
                            "used": true
                        }
                    ]
                }
            ]
        }
    },
    "192.169.10.0:2": {
        "[5]:[0]:[32]:[192.169.10.1]": {
            "prefix": "[5]:[0]:[32]:[192.169.10.1]",
            "prefixLen": 352,
            "paths": [
                {
                    "valid": true,
                    "bestpath": true,
                    "selectionReason": "First path received",
                    "pathFrom": "external",
                    "routeType": 5,
                    "ip": "192.169.10.1",
                    "nexthops": [
                        {
                            "ip": "192.169.10.2",
                            "hostname": "spine",
                            "afi": "ipv4",
                            "used": true
                        }
                    ]
                }
            ]
        }
    }
}`)

func TestParseL2VPNEVPN1(t *testing.T) {
	expectedData := EVPNData{
		BgpTableVersion:  1,
		BgpLocalRouterId: "192.168.1.1",
		DefaultLocPrf:    100,
		LocalAS:          64520,
		NumPrefix:        3,
		TotalPrefix:      3,
		Entries: []RdEntry{
			{
				RD: "192.168.20.1:2",
				Prefixes: map[string]Prefix{
					"[5]:[0]:[24]:[192.168.20.0]": {
						Prefix:    "[5]:[0]:[24]:[192.168.20.0]",
						PrefixLen: 352,
						Paths: []Path{
							{
								Valid:           true,
								Bestpath:        true,
								SelectionReason: "First path received",
								PathFrom:        "external",
								RouteType:       5,
								IP:              "192.168.20.0",
								Nexthops: []Nexthop{
									{
										IP:       "192.168.20.1",
										Hostname: "leafA",
										Afi:      "ipv4",
										Used:     true,
									},
								},
							},
						},
					},
				},
			},
			{
				RD: "192.169.10.0:2",
				Prefixes: map[string]Prefix{
					"[5]:[0]:[32]:[192.169.10.1]": {
						Prefix:    "[5]:[0]:[32]:[192.169.10.1]",
						PrefixLen: 352,
						Paths: []Path{
							{
								Valid:           true,
								Bestpath:        true,
								SelectionReason: "First path received",
								PathFrom:        "external",
								RouteType:       5,
								IP:              "192.169.10.1",
								Nexthops: []Nexthop{
									{
										IP:       "192.169.10.2",
										Hostname: "spine",
										Afi:      "ipv4",
										Used:     true,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	parsedData, err := parseL2VPNEVPN(data)
	if err != nil {
		t.Fatalf("Parsing returned an error: %v", err)
	}

	if !reflect.DeepEqual(parsedData, expectedData) {
		parsedJSON, _ := json.MarshalIndent(parsedData, "", "  ")
		expectedJSON, _ := json.MarshalIndent(expectedData, "", "  ")
		t.Errorf("Parsed data does not match expected data.\nParsed:\n%s\nExpected:\n%s", parsedJSON, expectedJSON)
	}
}

func TestPathHasAllRouteTargets(t *testing.T) {
	path := Path{
		ExtendedCommunity: ExtendedCommunity{
			String: "RT:65000:10 RT:65000:2 ET:8",
		},
	}

	tests := []struct {
		name         string
		routeTargets []v1alpha1.RouteTarget
		want         bool
	}{
		{
			name:         "exact route target match",
			routeTargets: []v1alpha1.RouteTarget{"65000:10", "65000:2"},
			want:         true,
		},
		{
			name:         "route target prefix does not match",
			routeTargets: []v1alpha1.RouteTarget{"65000:1"},
			want:         false,
		},
		{
			name:         "one matching route target is insufficient",
			routeTargets: []v1alpha1.RouteTarget{"65000:10", "65000:1"},
			want:         false,
		},
		{
			name: "empty route targets match",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathHasAllRouteTargets(path, tt.routeTargets); got != tt.want {
				t.Errorf("pathHasAllRouteTargets() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathHasRouteTarget(t *testing.T) {
	path := Path{
		ExtendedCommunity: ExtendedCommunity{
			String: "RT:65000:10 RT:65000:2 ET:8",
		},
	}

	if pathHasRouteTarget(path, []v1alpha1.RouteTarget{"65000:1"}) {
		t.Fatal("pathHasRouteTarget() matched a route target prefix")
	}
	if !pathHasRouteTarget(path, []v1alpha1.RouteTarget{"65000:2"}) {
		t.Fatal("pathHasRouteTarget() did not match an exact route target")
	}
}

func TestContainsRouteWithRD(t *testing.T) {
	for _, routeType := range []int{2, 5} {
		identity := "192.0.2.1"
		if routeType == 5 {
			identity += "/32"
		}
		path := Path{RouteType: routeType, IP: "192.0.2.1", IPLen: 32,
			Nexthops:          []Nexthop{{IP: "198.51.100.1"}},
			ExtendedCommunity: ExtendedCommunity{String: "RT:64514:300 RT:64514:301 ET:8"}}
		info := EVPNData{Entries: []RdEntry{{RD: "198.51.100.1:42",
			Prefixes: map[string]Prefix{"route": {Paths: []Path{path}}}}}}
		tests := []struct {
			name, rd, identity, nextHop string
			routeType                   int
			rts                         []v1alpha1.RouteTarget
			want                        bool
		}{
			{"matching", "198.51.100.1:42", identity, "198.51.100.1", routeType, []v1alpha1.RouteTarget{"64514:300", "64514:301"}, true},
			{"wrong RD", "198.51.100.1:43", identity, "198.51.100.1", routeType, nil, false},
			{"wrong identity", "198.51.100.1:42", "192.0.2.2", "198.51.100.1", routeType, nil, false},
			{"wrong next hop", "198.51.100.1:42", identity, "198.51.100.2", routeType, nil, false},
			{"wrong type", "198.51.100.1:42", identity, "198.51.100.1", 3, nil, false},
			{"missing RT", "198.51.100.1:42", identity, "198.51.100.1", routeType, []v1alpha1.RouteTarget{"64514:300", "64514:302"}, false},
			{"unexpected extra RT", "198.51.100.1:42", identity, "198.51.100.1", routeType, []v1alpha1.RouteTarget{"64514:300"}, false},
		}
		for _, tc := range tests {
			t.Run(fmt.Sprintf("type%d/%s", routeType, tc.name), func(t *testing.T) {
				if got := info.ContainsRouteWithRD(tc.rd, tc.routeType, tc.identity, tc.nextHop, tc.rts); got != tc.want {
					t.Fatalf("ContainsRouteWithRD() = %v, want %v", got, tc.want)
				}
			})
		}
	}
}

func TestContainsRouteWithRDDoesNotCombinePaths(t *testing.T) {
	path := Path{RouteType: 2, IP: "192.0.2.1", IPLen: 32,
		Nexthops:          []Nexthop{{IP: "198.51.100.1"}},
		ExtendedCommunity: ExtendedCommunity{String: "RT:64514:300"}}
	otherPath := path
	otherPath.Nexthops = []Nexthop{{IP: "198.51.100.2"}}
	otherPath.ExtendedCommunity.String = "RT:64514:301"
	otherRDPath := path
	otherRDPath.ExtendedCommunity.String = "RT:64514:300 RT:64514:301"
	info := EVPNData{Entries: []RdEntry{
		{RD: "198.51.100.1:42", Prefixes: map[string]Prefix{"route": {Paths: []Path{path, otherPath}}}},
		{RD: "198.51.100.1:43", Prefixes: map[string]Prefix{"route": {Paths: []Path{otherRDPath}}}},
	}}
	if info.ContainsRouteWithRD("198.51.100.1:42", 2, "192.0.2.1", "198.51.100.1",
		[]v1alpha1.RouteTarget{"64514:300", "64514:301"}) {
		t.Fatal("matched route targets from different paths or another RD")
	}
	path.IPLen = 0
	info.Entries[0].Prefixes["route"] = Prefix{Paths: []Path{path}}
	if info.ContainsRouteWithRD("198.51.100.1:42", 2, "192.0.2.1", "198.51.100.1", []v1alpha1.RouteTarget{"64514:300"}) {
		t.Fatal("matched a MAC-only route as a MAC/IP route")
	}
}
