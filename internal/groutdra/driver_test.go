// SPDX-License-Identifier:Apache-2.0

package groutdra

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayCIDRForDevice(t *testing.T) {
	for device, want := range map[string]string{
		"vhu-0": "192.169.20.1/24",
		"vhu-1": "192.169.21.1/24",
		"vhu-7": "192.169.27.1/24",
	} {
		got, err := gatewayCIDRForDevice(device)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	for _, device := range []string{"", "vhu-8", "vhu--1", "other-0"} {
		_, err := gatewayCIDRForDevice(device)
		require.Error(t, err)
	}
}
