// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

// NeighborInfo returns informations for the given neighbor in the given
// executor.
func NeighborInfo(neighborName string, exec executor.Executor) (*FRRNeighbor, error) {
	res, err := exec.Exec("vtysh", "-c", fmt.Sprintf("show bgp neighbor %s json", neighborName))

	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("Failed to query neighbour %s", neighborName))
	}
	neighbor, err := parseNeighbour(res)
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("Failed to parse neighbour %s", neighborName))
	}
	return neighbor, nil
}

type BGPRoutes map[string][]string

func (r BGPRoutes) HaveRoute(prefix, expectedNexthop string) bool {
	nextHops, ok := r[prefix]
	if !ok {
		return false
	}
	for _, n := range nextHops {
		if n == expectedNexthop {
			return true
		}
	}
	return false
}

func BGPRoutesFor(exec executor.Executor) (BGPRoutes, BGPRoutes, error) {
	ipv4Res, err := exec.Exec("vtysh", "-c", "show bgp ipv4 json")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to show bgp ipv4: %w - %s", err, ipv4Res)
	}
	ipv4Routes, err := parseRoutes(ipv4Res)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse IPv4 routes: %w", err)
	}

	ipv6Res, err := exec.Exec("vtysh", "-c", "show bgp ipv6 json")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to show bgp ipv6: %w - %s", err, ipv6Res)
	}
	ipv6Routes, err := parseRoutes(ipv6Res)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse IPv6 routes: %w", err)
	}

	return ipv4Routes, ipv6Routes, nil
}

const EstablishedState = "Established"

type FRRNeighbor struct {
	BGPNeighborAddr              string      `json:"bgpNeighborAddr"`
	RemoteAs                     int         `json:"remoteAs"`
	LocalAs                      int         `json:"localAs"`
	RemoteRouterID               string      `json:"remoteRouterId"`
	BgpVersion                   int         `json:"bgpVersion"`
	BgpState                     string      `json:"bgpState"`
	PortForeign                  int         `json:"portForeign"`
	PeerBFDInfo                  PeerBFDInfo `json:"peerBfdInfo"`
	VRFName                      string      `json:"vrf"`
	ConfiguredHoldTimeMSecs      int         `json:"bgpTimerConfiguredHoldTimeMsecs"`
	ConfiguredKeepAliveTimeMSecs int         `json:"bgpTimerConfiguredKeepAliveIntervalMsecs"`
	ConnectRetryTimer            int         `json:"connectRetryTimer"`
	AddressFamilyInfo            map[string]struct {
		SentPrefixCounter int `json:"sentPrefixCounter"`
	} `json:"addressFamilyInfo"`
	ConnectionsDropped int `json:"connectionsDropped"`
}

type PeerBFDInfo struct {
	Type             string `json:"type"`
	DetectMultiplier int    `json:"detectMultiplier"`
	RxMinInterval    int    `json:"rxMinInterval"`
	TxMinInterval    int    `json:"txMinInterval"`
	Status           string `json:"status"`
	LastUpdate       string `json:"lastUpdate"`
}

type IPInfo struct {
	Routes map[string][]FRRRoute `json:"routes"`
}

type FRRRoute struct {
	Stale     bool   `json:"stale"`
	BestPath  bool   `json:"bestpath"`
	Valid     bool   `json:"valid"`
	PeerID    string `json:"peerId"`
	LocalPref uint32 `json:"locPrf"`
	Origin    string `json:"origin"`
	PathFrom  string `json:"pathFrom"`
	Nexthops  []struct {
		IP    string `json:"ip"`
		Scope string `json:"scope"`
	} `json:"nexthops"`
}

type BFDPeer struct {
	Multihop                  bool   `json:"multihop"`
	Peer                      string `json:"peer"`
	Local                     string `json:"local"`
	Vrf                       string `json:"vrf"`
	Interface                 string `json:"interface"`
	ID                        int    `json:"id"`
	RemoteID                  int64  `json:"remote-id"`
	PassiveMode               bool   `json:"passive-mode"`
	Status                    string `json:"status"`
	Uptime                    int    `json:"uptime"`
	Diagnostic                string `json:"diagnostic"`
	RemoteDiagnostic          string `json:"remote-diagnostic"`
	ReceiveInterval           int    `json:"receive-interval"`
	TransmitInterval          int    `json:"transmit-interval"`
	EchoReceiveInterval       int    `json:"echo-receive-interval"`
	EchoTransmitInterval      int    `json:"echo-transmit-interval"`
	DetectMultiplier          int    `json:"detect-multiplier"`
	RemoteReceiveInterval     int    `json:"remote-receive-interval"`
	RemoteTransmitInterval    int    `json:"remote-transmit-interval"`
	RemoteEchoInterval        int    `json:"remote-echo-interval"`
	RemoteEchoReceiveInterval int    `json:"remote-echo-receive-interval"`
	RemoteDetectMultiplier    int    `json:"remote-detect-multiplier"`
}

type NoNeighborError struct{}

func (n NoNeighborError) Error() string {
	return "no such neighbor"
}

// parseNeighbour takes the result of a show bgp neighbor x.y.w.z
// and parses the informations related to the neighbour.
func parseNeighbour(vtyshRes string) (*FRRNeighbor, error) {
	var rawNeighborReply map[string]json.RawMessage
	if err := json.Unmarshal([]byte(vtyshRes), &rawNeighborReply); err != nil {
		return nil, fmt.Errorf("error unmarshalling raw JSON: %v", err)
	}
	if _, ok := rawNeighborReply["bgpNoSuchNeighbor"]; ok {
		return nil, NoNeighborError{}
	}

	res := map[string]FRRNeighbor{}
	err := json.Unmarshal([]byte(vtyshRes), &res)
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("failed to parse vtysh response: %s", vtyshRes))
	}
	if len(res) > 1 {
		return nil, errors.New("more than one peer were returned")
	}
	if len(res) == 0 {
		return nil, errors.New("no peers were returned")
	}
	for _, n := range res {
		return &n, nil
	}
	return nil, nil
}

func parseRoutes(vtyshRes string) (BGPRoutes, error) {
	res := BGPRoutes{}
	parsed := IPInfo{}
	err := json.Unmarshal([]byte(vtyshRes), &parsed)
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("failed to parse vtysh response: %s", vtyshRes))
	}

	for p, routes := range parsed.Routes {
		for _, r := range routes {
			if !r.BestPath || !r.Valid {
				continue
			}

			if res[p] == nil {
				res[p] = []string{}
			}
			for _, nextHop := range r.Nexthops {
				res[p] = append(res[p], nextHop.IP)
			}
		}
	}
	return res, nil
}
