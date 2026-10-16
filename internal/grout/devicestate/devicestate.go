// SPDX-License-Identifier:Apache-2.0

package devicestate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var ErrNotFound = errors.New("device state not found")

var errMissingDeviceID = errors.New("device state deviceID is required")

// Store holds per-device state files in Dir.
type Store struct {
	Dir string
}

// State records the original state of a network device before it is
// handed to grout, so the device can be restored on teardown, or if
// the setup process is interrupted in the middle.
//
// InterfaceName is the primary deviceID and the state file is named
// "<interfaceName>.json". PCIAddress is stored in the file so a DPDK
// port can be mapped back to its original kernel interface.
type State struct {
	InterfaceName  string   `json:"interfaceName"`
	PCIAddress     string   `json:"pciAddress,omitempty"`
	OriginalDriver string   `json:"originalDriver,omitempty"`
	Addresses      []string `json:"addresses"`
}

func (s Store) Save(state State) error {
	if state.InterfaceName == "" {
		return errMissingDeviceID
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("failed to create device state directory: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal device state: %w", err)
	}
	path := s.filePath(state.InterfaceName)
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("failed to write device state to %s: %w", path, err)
	}
	return nil
}

func (s Store) Load(deviceID string) (State, error) {
	if deviceID == "" {
		return State{}, errMissingDeviceID
	}
	return loadState(s.filePath(deviceID))
}

// List returns all the saved device states.
func (s Store) List() ([]State, error) {
	paths, err := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list device state files in %s: %w", s.Dir, err)
	}
	states := make([]State, 0, len(paths))
	for _, path := range paths {
		state, err := loadState(path)
		if err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

func (s Store) Delete(deviceID string) error {
	if deviceID == "" {
		return errMissingDeviceID
	}
	path := s.filePath(deviceID)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to delete device state file %s: %w", path, err)
	}
	return nil
}

func (s Store) filePath(deviceID string) string {
	return filepath.Join(s.Dir, deviceID+".json")
}

// writeFileAtomic writes to a temporary file and renames it over path, so
// an interrupted write never leaves a truncated state file behind. The
// temporary name does not end in ".json" so List never picks it up.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func loadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, ErrNotFound
	}
	if err != nil {
		return State{}, fmt.Errorf("failed to read device state from %s: %w", path, err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("failed to unmarshal device state from %s: %w", path, err)
	}
	return state, nil
}
