// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"reflect"
	"testing"
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
