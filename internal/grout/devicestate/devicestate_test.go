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
	t.Parallel()
	store := Store{Dir: filepath.Join(t.TempDir(), "grout-state")}

	state := State{
		InterfaceName:  "enp3s0f0v0",
		PCIAddress:     "0000:03:02.0",
		OriginalDriver: "ice",
		Addresses:      []string{"192.168.1.10/24", "fd00::10/64"},
		AddrGenMode:    "0",
		AcceptRA:       "1",
	}

	require.NoError(t, store.Save(state))

	loaded, err := store.Load("enp3s0f0v0")
	require.NoError(t, err)
	assert.Equal(t, state, loaded)

	require.NoError(t, store.Delete("enp3s0f0v0"))
	_, err = os.Stat(store.filePath("enp3s0f0v0"))
	assert.True(t, os.IsNotExist(err), "state file should be removed")

	_, err = store.Load("enp3s0f0v0")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestOverwrite(t *testing.T) {
	t.Parallel()
	store := Store{Dir: filepath.Join(t.TempDir(), "grout-state")}

	require.NoError(t, store.Save(State{
		InterfaceName: "toswitch1",
		Addresses:     []string{"10.0.0.1/24"},
	}))
	require.NoError(t, store.Save(State{
		InterfaceName: "toswitch1",
		Addresses:     []string{"192.168.1.1/24", "fd00::1/64"},
	}))

	loaded, err := store.Load("toswitch1")
	require.NoError(t, err)
	assert.Equal(t, []string{"192.168.1.1/24", "fd00::1/64"}, loaded.Addresses)
}

func TestDeleteNonExistent(t *testing.T) {
	t.Parallel()
	store := Store{Dir: filepath.Join(t.TempDir(), "grout-state")}

	assert.NoError(t, store.Delete("does_not_exist"))
	assert.ErrorIs(t, store.Delete(""), errMissingDeviceID)
	assert.ErrorIs(t, store.Save(State{}), errMissingDeviceID)
	_, err := store.Load("")
	assert.ErrorIs(t, err, errMissingDeviceID)
}

func TestList(t *testing.T) {
	t.Parallel()
	store := Store{Dir: filepath.Join(t.TempDir(), "grout-state")}

	states, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, states)

	tap := State{InterfaceName: "toswitch1", Addresses: []string{"10.0.0.1/24"}}
	pci := State{InterfaceName: "ens1f0", PCIAddress: "0000:03:02.0", OriginalDriver: "ice"}
	require.NoError(t, store.Save(tap))
	require.NoError(t, store.Save(pci))

	states, err = store.List()
	require.NoError(t, err)
	assert.ElementsMatch(t, []State{tap, pci}, states)
}
