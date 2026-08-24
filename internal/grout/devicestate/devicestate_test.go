// SPDX-License-Identifier:Apache-2.0

package devicestate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadDelete(t *testing.T) {
	origDir := Dir
	Dir = filepath.Join(t.TempDir(), "grout-state")
	t.Cleanup(func() { Dir = origDir })

	state := Entry{
		InterfaceName:  "enp3s0f0v0",
		PCIAddress:     "0000:03:02.0",
		OriginalDriver: "ice",
		Addresses:      []string{"192.168.1.10/24", "fd00::10/64"},
		MTU:            9000,
	}

	require.NoError(t, Save("enp3s0f0v0", state))

	loaded, err := Load("enp3s0f0v0")
	require.NoError(t, err)
	assert.Equal(t, state, *loaded)
	assert.Equal(t, int32(9000), loaded.MTU)

	require.NoError(t, Delete("enp3s0f0v0"))
	_, err = os.Stat(filePath("enp3s0f0v0"))
	assert.True(t, os.IsNotExist(err), "state file should be removed")

	_, err = Load("enp3s0f0v0")
	require.ErrorIs(t, err, ErrDeviceStateNotFound)
}

func TestOverwrite(t *testing.T) {
	origDir := Dir
	Dir = filepath.Join(t.TempDir(), "grout-state")
	t.Cleanup(func() { Dir = origDir })

	require.NoError(t, Save("toswitch1", Entry{
		InterfaceName: "toswitch1",
		Addresses:     []string{"10.0.0.1/24"},
	}))
	require.NoError(t, Save("toswitch1", Entry{
		InterfaceName: "toswitch1",
		Addresses:     []string{"192.168.1.1/24", "fd00::1/64"},
	}))

	loaded, err := Load("toswitch1")
	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.1.1/24", "fd00::1/64"}, loaded.Addresses)
}

func TestDeleteNonExistent(t *testing.T) {
	origDir := Dir
	Dir = filepath.Join(t.TempDir(), "grout-state")
	t.Cleanup(func() { Dir = origDir })

	assert.NoError(t, Delete("does_not_exist"))
	assert.Error(t, Delete(""))
	assert.Error(t, Save("", Entry{}))
}

func TestLoadByPCI(t *testing.T) {
	origDir := Dir
	Dir = filepath.Join(t.TempDir(), "grout-state")
	t.Cleanup(func() { Dir = origDir })

	require.NoError(t, Save("ens1f0", Entry{
		PCIAddress:     "0000:01:00.0",
		InterfaceName:  "ens1f0",
		OriginalDriver: "iavf",
		Addresses:      []string{"10.0.0.1/24"},
		MTU:            9000,
	}))
	require.NoError(t, Save("ens2f0", Entry{
		PCIAddress:     "0000:02:00.0",
		InterfaceName:  "ens2f0",
		OriginalDriver: "mlx5_core",
	}))
	require.NoError(t, Save("toswitch1", Entry{
		InterfaceName: "toswitch1",
		Addresses:     []string{"192.168.11.3/24"},
	}))

	t.Run("finds matching PCI address", func(t *testing.T) {
		loaded, err := LoadByPCI("0000:02:00.0")
		require.NoError(t, err)
		assert.Equal(t, "ens2f0", loaded.InterfaceName)
		assert.Equal(t, "mlx5_core", loaded.OriginalDriver)
	})

	t.Run("preserves MTU", func(t *testing.T) {
		loaded, err := LoadByPCI("0000:01:00.0")
		require.NoError(t, err)
		assert.Equal(t, int32(9000), loaded.MTU)
	})

	t.Run("errors when PCI address is unknown", func(t *testing.T) {
		_, err := LoadByPCI("0000:ff:00.0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no device state for PCI address")
	})

	t.Run("errors when PCI address is empty", func(t *testing.T) {
		_, err := LoadByPCI("")
		assert.Error(t, err)
	})
}
