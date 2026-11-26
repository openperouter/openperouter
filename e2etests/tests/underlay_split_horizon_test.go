// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"encoding/json"
	"testing"

	"github.com/openperouter/openperouter/e2etests/pkg/frr"
)

func TestEVPNContainsRouteWithdrawal(t *testing.T) {
	route := fabricRouteIdentity{rd: "64512:4022", ethTag: 4022, prefix: "198.18.22.0/24"}
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "received", payload: `{"valid":true,"routeType":5,"ethTag":4022,"ipLen":24,"ip":"198.18.22.0"}`, want: true},
		{name: "withdrawn", payload: `{"removed":true,"routeType":5,"ethTag":4022,"ipLen":24,"ip":"198.18.22.0"}`, want: false},
		{name: "advertised", payload: `{"routeType":5,"ethTag":4022,"ipLen":24,"ip":"198.18.22.0"}`, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var path frr.Path
			if err := json.Unmarshal([]byte(tc.payload), &path); err != nil {
				t.Fatal(err)
			}
			data := frr.EVPNData{Entries: []frr.RdEntry{{
				RD:       route.rd,
				Prefixes: map[string]frr.Prefix{"route": {Paths: []frr.Path{path}}},
			}}}
			if got := evpnContainsRoute(data, route); got != tc.want {
				t.Fatalf("evpnContainsRoute() = %v, want %v", got, tc.want)
			}
		})
	}
}
