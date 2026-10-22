// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"strings"
	"testing"

	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
)

func TestRegisterNodeLinksRequiresLeaf1IP(t *testing.T) {
	err := registerNodeLinks(nodeLinksConfig{
		Nodes: map[string]nodeConfig{
			"worker": {},
		},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "ipForKindLeaf is required") {
		t.Fatalf("got %v, want ipForKindLeaf required error", err)
	}
}

func TestRegisterNodeLinksDualLeafRequiresLeaf2IP(t *testing.T) {
	err := registerNodeLinks(nodeLinksConfig{
		Nodes: map[string]nodeConfig{
			"worker": {IPForKindLeaf: "192.168.11.4"},
		},
	}, true)
	if err == nil || !strings.Contains(err.Error(), "ipForKindLeaf2 is required") {
		t.Fatalf("got %v, want ipForKindLeaf2 required error", err)
	}
}

func TestRegisterNodeLinksSingleLeafOmitsLeaf2(t *testing.T) {
	// Reset link tables so HasLink checks are meaningful for this node.
	linksForFamily = map[ipfamily.Family]map[link]linkAddresses{}

	err := registerNodeLinks(nodeLinksConfig{
		Nodes: map[string]nodeConfig{
			"worker": {
				IPForKindLeaf:  "192.168.11.4",
				IPForKindLeaf2: "192.168.12.4", // ignored in single-leaf mode
			},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !HasKindLeafLink("worker") {
		t.Fatal("expected leaf1 link")
	}
	if HasKindLeaf2Link("worker") {
		t.Fatal("did not expect leaf2 link in single-leaf mode")
	}
}

func TestRegisterNodeLinksDualLeafOK(t *testing.T) {
	linksForFamily = map[ipfamily.Family]map[link]linkAddresses{}

	err := registerNodeLinks(nodeLinksConfig{
		Nodes: map[string]nodeConfig{
			"worker": {
				IPForKindLeaf:         "192.168.11.4",
				IPForKindLeaf2:        "192.168.12.4",
				IPv6ForKindLeaf:       "2001:db8:11::4",
				IPv6ForKindLeaf2:      "2001:db8:12::4",
				IfaceForKindLeaf:      "toleafkind1",
				IfaceForKindLeaf2:     "toleafkind2",
				LeafIfaceForKindLeaf:  "tokindworker",
				LeafIfaceForKindLeaf2: "tokindworker",
			},
		},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !HasKindLeafLink("worker") || !HasKindLeaf2Link("worker") {
		t.Fatal("expected leaf1 and leaf2 links")
	}
}
