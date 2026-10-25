// SPDX-License-Identifier:Apache-2.0

package poc

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalog(t *testing.T) {
	catalog := New("example.driver", "example-pool")
	resources := catalog.Resources()
	slices := resources.Pools["example-pool"].Slices
	require.Len(t, slices, 1)
	require.Len(t, slices[0].Devices, 8)
	for index, device := range slices[0].Devices {
		wantName := fmt.Sprintf("vhu-%d", index)
		require.Equal(t, wantName, device.Name)
		gateway, err := catalog.GatewayCIDR(device.Name)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("192.169.%d.1/24", 20+index), gateway)
	}
	for _, device := range []string{"", "vhu-8", "vhu--1", "other-0"} {
		_, err := catalog.GatewayCIDR(device)
		require.Error(t, err)
	}
}
