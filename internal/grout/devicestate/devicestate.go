// SPDX-License-Identifier:Apache-2.0

package devicestate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Dir is the directory that holds per-device state files. It can be
// overridden in tests.
var Dir = "/var/run/openperouter/grout"
var ErrDeviceStateNotFound = errors.New("device state not found")

// Entry records the original state of a network device before it is
// handed to grout, so the device can be restored on teardown.
//
// InterfaceName is the primary key and the state file is named
// "<interfaceName>.json". PCIAddress is stored in the file so a DPDK
// port can be mapped back to its original kernel interface.
type Entry struct {
	InterfaceName  string   `json:"interfaceName"`
	PCIAddress     string   `json:"pciAddress,omitempty"`
	OriginalDriver string   `json:"originalDriver,omitempty"`
	Addresses      []string `json:"addresses"`
	MTU            int32    `json:"mtu,omitempty"`
}

func filePath(key string) string {
	return filepath.Join(Dir, key+".json")
}

func Save(key string, state Entry) error {
	if key == "" {
		return fmt.Errorf("device state key is required")
	}
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return fmt.Errorf("failed to create device state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal device state: %w", err)
	}
	path := filePath(key)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write device state to %s: %w", path, err)
	}
	return nil
}

func Load(key string) (*Entry, error) {
	path := filePath(key)
	return loadFile(path)
}

// LoadByPCI finds the saved device state whose PCIAddress matches.
// State files are keyed by interface name, so this scans the state directory.
func LoadByPCI(pciAddress string) (*Entry, error) {
	if pciAddress == "" {
		return nil, fmt.Errorf("pci address is required")
	}
	entries, err := os.ReadDir(Dir)
	if err != nil {
		return nil, fmt.Errorf("failed to list device state directory %s: %w", Dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		state, err := loadFile(filepath.Join(Dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if state.PCIAddress == pciAddress {
			return state, nil
		}
	}
	return nil, fmt.Errorf("no device state for PCI address %s", pciAddress)
}

func loadFile(path string) (*Entry, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrDeviceStateNotFound
		}
		return nil, fmt.Errorf("failed to stat device state file %s: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read device state from %s: %w", path, err)
	}
	var state Entry
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal device state from %s: %w", path, err)
	}
	return &state, nil
}

func Delete(key string) error {
	if key == "" {
		return fmt.Errorf("device state key is required")
	}
	path := filePath(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete device state file %s: %w", path, err)
	}
	return nil
}
