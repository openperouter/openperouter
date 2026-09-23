// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

// ISISDatabase represents the parsed output of "show isis database detail json".
type ISISDatabase struct {
	Areas []ISISArea `json:"areas"`
}

// ISISArea represents an IS-IS area containing one or more levels.
type ISISArea struct {
	Levels []ISISLevel `json:"levels"`
}

// ISISLevel represents an IS-IS level (1 or 2) and its LSPs.
type ISISLevel struct {
	LSPs []ISISLSP `json:"lsps"`
}

// ISISLSP represents a Link State PDU with its reachability information.
type ISISLSP struct {
	IPv4       string           `json:"ipv4,omitempty"`
	ExtIPReach []ISISExtIPReach `json:"extIpReach,omitempty"`
	IPv6Reach  []ISISIPv6Reach  `json:"ipv6Reach,omitempty"`
}

// ISISExtIPReach represents an Extended IP Reachability (TLV 135) entry.
type ISISExtIPReach struct {
	MtID          string `json:"mtId"`
	IPReach       string `json:"ipReach"`
	IPReachMetric int    `json:"ipReachMetric"`
	Down          bool   `json:"down"`
}

// ISISIPv6Reach represents an IPv6 Reachability (TLV 236) entry.
type ISISIPv6Reach struct {
	MtID     string `json:"mtId"`
	Prefix   string `json:"prefix"`
	Metric   int    `json:"metric"`
	Down     bool   `json:"down"`
	External bool   `json:"external"`
}

// GetLSPs returns all LSPs across all areas and levels.
func (isis *ISISDatabase) GetLSPs() []ISISLSP {
	lsps := []ISISLSP{}
	for _, area := range isis.Areas {
		for _, level := range area.Levels {
			lsps = append(lsps, level.LSPs...)
		}
	}
	return lsps
}

// GetExtIPReach returns all Extended IP Reachability entries across all LSPs.
func (isis *ISISDatabase) GetExtIPReach() []ISISExtIPReach {
	lsps := []ISISExtIPReach{}
	for _, lsp := range isis.GetLSPs() {
		lsps = append(lsps, lsp.ExtIPReach...)
	}
	return lsps
}

// GetIPv6Reach returns all IPv6 Reachability entries across all LSPs.
func (isis *ISISDatabase) GetIPv6Reach() []ISISIPv6Reach {
	lsps := []ISISIPv6Reach{}
	for _, lsp := range isis.GetLSPs() {
		lsps = append(lsps, lsp.IPv6Reach...)
	}
	return lsps
}

// GetISISDatabaseDetail runs "show isis database detail json" and parses the result.
func GetISISDatabaseDetail(exec executor.Executor) (ISISDatabase, error) {
	cmd := "show isis database detail json"
	res, err := exec.Exec("vtysh", "-c", cmd)
	if err != nil {
		return ISISDatabase{}, fmt.Errorf("failed to query `%s`: %w. Output: %s",
			cmd, err, res)
	}

	isisDatabase, err := parseISISDatabase([]byte(res))
	if err != nil {
		return ISISDatabase{}, fmt.Errorf("failed to parse output of `%s`: %w. Output: %s",
			cmd, err, res)
	}
	return isisDatabase, nil
}

func parseISISDatabase(data []byte) (ISISDatabase, error) {
	res := ISISDatabase{}
	if err := json.Unmarshal(data, &res); err != nil {
		return ISISDatabase{}, fmt.Errorf("error unmarshalling JSON: %w", err)
	}

	return res, nil
}
