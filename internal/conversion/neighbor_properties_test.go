// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

func TestEBGPMultiHopForNeighbor(t *testing.T) {
	tests := []struct {
		name       string
		properties []v1alpha1.NeighborProperty
		wantSet    bool
		wantTTL    *int32
	}{
		{
			name:       "no properties",
			properties: nil,
			wantSet:    false,
			wantTTL:    nil,
		},
		{
			name:       "ebgpMultiHop without parameters",
			properties: []v1alpha1.NeighborProperty{{Type: v1alpha1.NeighborPropertyEBGPMultiHop}},
			wantSet:    true,
			wantTTL:    nil,
		},
		{
			name: "ebgpMultiHop with empty parameters",
			properties: []v1alpha1.NeighborProperty{{
				Type:         v1alpha1.NeighborPropertyEBGPMultiHop,
				EBGPMultiHop: &v1alpha1.EBGPMultiHopProperties{},
			}},
			wantSet: true,
			wantTTL: nil,
		},
		{
			name: "ebgpMultiHop with ttl",
			properties: []v1alpha1.NeighborProperty{{
				Type:         v1alpha1.NeighborPropertyEBGPMultiHop,
				EBGPMultiHop: &v1alpha1.EBGPMultiHopProperties{TTL: new(int32(5))},
			}},
			wantSet: true,
			wantTTL: new(int32(5)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSet, gotTTL := ebgpMultiHopForNeighbor(v1alpha1.Neighbor{Properties: tc.properties})
			if gotSet != tc.wantSet {
				t.Errorf("ebgpMultiHop set = %v, want %v", gotSet, tc.wantSet)
			}
			switch {
			case tc.wantTTL == nil && gotTTL != nil:
				t.Errorf("ttl = %d, want nil", *gotTTL)
			case tc.wantTTL != nil && gotTTL == nil:
				t.Errorf("ttl = nil, want %d", *tc.wantTTL)
			case tc.wantTTL != nil && *gotTTL != *tc.wantTTL:
				t.Errorf("ttl = %d, want %d", *gotTTL, *tc.wantTTL)
			}
		})
	}
}
