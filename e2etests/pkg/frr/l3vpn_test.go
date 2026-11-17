// SPDX-License-Identifier:Apache-2.0

package frr

import "testing"

func TestContainsPrefixWithRouteTarget(t *testing.T) {
	data := L3VPNData{
		Routes: Route{
			RouteDistinguishers: RouteDistinguisherMap{
				"65001:102": {
					"198.18.102.0/24": []BGPPath{{
						ExtendedCommunity: ExtendedCommunity{
							String: "ET:8 RT:65001:102 RT:65001:103 Rmac:02:00:00:00:00:01",
						},
					}},
				},
			},
		},
	}

	if !ContainsPrefixWithRouteTarget(data, "198.18.102.0/24", "65001:102") {
		t.Fatal("expected route target token to match")
	}
	if ContainsPrefixWithRouteTarget(data, "198.18.102.0/24", "65001:104") {
		t.Fatal("did not expect unrelated route target to match")
	}
	if ContainsPrefixWithRouteTarget(data, "198.18.103.0/24", "65001:102") {
		t.Fatal("did not expect unrelated prefix to match")
	}
}
