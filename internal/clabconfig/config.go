// SPDX-License-Identifier:Apache-2.0

package clabconfig

import (
	"os"

	"gopkg.in/yaml.v3"
)

type EnvironmentConfig struct {
	IPRanges IPRanges     `yaml:"ipRanges"`
	Nodes    []NodeConfig `yaml:"nodes"`
}

type IPRanges struct {
	PointToPoint PointToPointRanges `yaml:"pointToPoint"`
	Broadcast    BroadcastRanges    `yaml:"broadcast"`
	VTEP         string             `yaml:"vtep"`
	RouterID     string             `yaml:"routerID"`
}

type PointToPointRanges struct {
	IPv4 string `yaml:"ipv4"`
	IPv6 string `yaml:"ipv6"`
}

type BroadcastRanges struct {
	IPv4 string `yaml:"ipv4"`
	IPv6 string `yaml:"ipv6"`
}

type NodeConfig struct {
	Pattern     string               `yaml:"pattern"`
	Role        string               `yaml:"role"`
	EVPNEnabled bool                 `yaml:"evpnEnabled"`
	VRFs        map[string]VRFConfig `yaml:"vrfs,omitempty"`
	BGP         BGPConfig            `yaml:"bgp"`
}

type VRFConfig struct {
	RedistributeConnected bool     `yaml:"redistributeConnected"`
	Interfaces            []string `yaml:"interfaces"`
	VNI                   int      `yaml:"vni"`
}

type BGPConfig struct {
	ASN   uint32       `yaml:"asn"`
	Peers []PeerConfig `yaml:"peers"`
}

type PeerConfig struct {
	Pattern     string `yaml:"pattern"`
	EVPNEnabled bool   `yaml:"evpnEnabled"`
	BFDEnabled  bool   `yaml:"bfdEnabled"`
}

// LoadConfig reads and parses a YAML environment configuration file from the given path.
func LoadConfig(path string) (*EnvironmentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config EnvironmentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
