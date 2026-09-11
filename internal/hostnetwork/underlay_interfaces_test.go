// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnderlayInterfacesToRemove(t *testing.T) {
	tap := UnderlayInterface{InterfaceName: "eth0", Kind: UnderlayInterfaceNetDev}
	dpdk := UnderlayInterface{
		InterfaceName:     "eth0",
		Kind:              UnderlayInterfaceNetDev,
		AcceleratedConfig: &AcceleratedConfigParams{},
	}
	other := UnderlayInterface{InterfaceName: "eth1", Kind: UnderlayInterfaceNetDev}

	t.Run("keeps matching tap interface", func(t *testing.T) {
		assert.Empty(t, UnderlayInterfacesToRemove([]UnderlayInterface{tap}, []UnderlayInterface{tap}))
	})

	t.Run("removes interface that is no longer requested", func(t *testing.T) {
		got := UnderlayInterfacesToRemove([]UnderlayInterface{tap, other}, []UnderlayInterface{tap})
		assert.Equal(t, []UnderlayInterface{other}, got)
	})

	t.Run("removes tap interface when requested as dpdk", func(t *testing.T) {
		got := UnderlayInterfacesToRemove([]UnderlayInterface{tap}, []UnderlayInterface{dpdk})
		assert.Equal(t, []UnderlayInterface{tap}, got)
	})

	t.Run("removes dpdk interface when requested as tap", func(t *testing.T) {
		got := UnderlayInterfacesToRemove([]UnderlayInterface{dpdk}, []UnderlayInterface{tap})
		assert.Equal(t, []UnderlayInterface{dpdk}, got)
	})

	t.Run("keeps matching dpdk interface", func(t *testing.T) {
		assert.Empty(t, UnderlayInterfacesToRemove([]UnderlayInterface{dpdk}, []UnderlayInterface{dpdk}))
	})
}
