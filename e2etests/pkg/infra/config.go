// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	kindLeafIP    = "192.168.11.2"
	kindLeaf2IP   = "192.168.12.2"
	kindLeafIPv6  = "2001:db8:11::2"
	kindLeaf2IPv6 = "2001:db8:12::2"
)

type nodeLinksConfig struct {
	Nodes map[string]nodeConfig `json:"nodes"`
}

type nodeConfig struct {
	IPForKindLeaf         string `json:"ipForKindLeaf"`
	IPForKindLeaf2        string `json:"ipForKindLeaf2"`
	IPv6ForKindLeaf       string `json:"ipv6ForKindLeaf"`
	IPv6ForKindLeaf2      string `json:"ipv6ForKindLeaf2"`
	IfaceForKindLeaf      string `json:"ifaceForKindLeaf"`
	IfaceForKindLeaf2     string `json:"ifaceForKindLeaf2"`
	LeafIfaceForKindLeaf  string `json:"leafIfaceForKindLeaf"`
	LeafIfaceForKindLeaf2 string `json:"leafIfaceForKindLeaf2"`
}

func RegisterLinks(path string) error {
	registerFabricLinks()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading node links config %s: %w", path, err)
	}
	var cfg nodeLinksConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing node links config %s: %w", path, err)
	}

	registerNodeLinks(cfg)
	return nil
}
