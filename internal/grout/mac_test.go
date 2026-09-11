// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandomMAC(t *testing.T) {
	seen := map[string]struct{}{}
	for range 100 {
		mac, err := randomMAC()
		require.NoError(t, err)
		require.Len(t, mac, 6)
		assert.Zero(t, mac[0]&0x01, "MAC must be unicast")
		assert.NotZero(t, mac[0]&0x02, "MAC must be locally administered")

		_, duplicate := seen[mac.String()]
		assert.False(t, duplicate, "generated duplicate MAC %s", mac)
		seen[mac.String()] = struct{}{}
	}
}
