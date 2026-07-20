// SPDX-License-Identifier:Apache-2.0

package dhcp

import (
	"encoding/json"
	"fmt"
)

// IPAMType returns the IPAM type string from a CNI conf or conflist JSON.
// It returns "" when no IPAM block is found.
func IPAMType(config []byte) (string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(config, &raw); err != nil {
		return "", fmt.Errorf("failed to parse CNI config: %w", err)
	}

	if _, hasPlugins := raw["plugins"]; hasPlugins {
		return ipamTypeFromConflist(raw)
	}
	return ipamTypeFromSingleConf(config)
}

func ipamTypeFromConflist(raw map[string]json.RawMessage) (string, error) {
	var plugins []json.RawMessage
	if err := json.Unmarshal(raw["plugins"], &plugins); err != nil {
		return "", fmt.Errorf("failed to parse conflist plugins: %w", err)
	}

	for _, plugin := range plugins {
		var p struct {
			IPAM struct {
				Type string `json:"type"`
			} `json:"ipam"`
		}
		if err := json.Unmarshal(plugin, &p); err != nil {
			continue
		}
		if p.IPAM.Type != "" {
			return p.IPAM.Type, nil
		}
	}
	return "", nil
}

func ipamTypeFromSingleConf(config []byte) (string, error) {
	var c struct {
		IPAM struct {
			Type string `json:"type"`
		} `json:"ipam"`
	}
	if err := json.Unmarshal(config, &c); err != nil {
		return "", fmt.Errorf("failed to parse CNI config: %w", err)
	}
	return c.IPAM.Type, nil
}
