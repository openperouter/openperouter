// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindStaleL3VPNs(t *testing.T) {
	tests := []struct {
		name       string
		ifaces     []groutInterface
		configured map[int32]bool
		expected   []int32
	}{
		{
			name:       "no interfaces",
			configured: map[int32]bool{},
			expected:   nil,
		},
		{
			name: "all configured",
			ifaces: []groutInterface{
				{Name: "pe-s-1", Type: "port"},
				{Name: "pe-s-2", Type: "port"},
			},
			configured: map[int32]bool{1: true, 2: true},
			expected:   nil,
		},
		{
			name: "one stale",
			ifaces: []groutInterface{
				{Name: "pe-s-1", Type: "port"},
				{Name: "pe-s-2", Type: "port"},
			},
			configured: map[int32]bool{1: true},
			expected:   []int32{2},
		},
		{
			name: "non l3vpn interfaces ignored",
			ifaces: []groutInterface{
				{Name: "pe-100", Type: "port"},
				{Name: "u_toswitch1", Type: "port"},
				{Name: "vni100", Type: "vxlan"},
				{Name: "pe-s-3", Type: "vrf"},
				{Name: "pe-s-3x", Type: "port"},
				{Name: "pe-s-3", Type: "port"},
			},
			configured: map[int32]bool{},
			expected:   []int32{3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, findStaleL3VPNs(tt.ifaces, tt.configured))
		})
	}
}
