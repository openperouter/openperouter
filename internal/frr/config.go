// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"text/template"

	"github.com/openperouter/openperouter/internal/networklayerprotocol"
)

var (
	//go:embed templates/* templates/*
	templates embed.FS
)

type RawFRRSnippet struct {
	Priority *int32
	Config   string
}

type Config struct {
	Loglevel    string
	Hostname    string
	Underlay    UnderlayConfig
	VNIs        []L3VNIConfig
	VPNs        []L3VPNConfig
	Passthrough *PassthroughConfig
	BFDProfiles []BFDProfile
	RawConfig   []RawFRRSnippet
}

type GracefulRestart struct {
	RestartTime   int64
	StalePathTime int64
}

type UnderlayConfig struct {
	MyASN           int64
	RouterID        string
	Neighbors       []NeighborConfig
	TunnelEndpoint  *TunnelEndpoint
	GracefulRestart *GracefulRestart
	ISIS            *UnderlayISIS
	SegmentRouting  *UnderlaySegmentRouting
	RouteReflector  *RouteReflector
	// ListenLimit caps the number of dynamic sessions accepted via bgp
	// listen range. When zero, DefaultListenLimit is rendered.
	ListenLimit uint16
}

// DefaultListenLimit raises the FRR default dynamic neighbors cap (100) to
// the maximum, needed to properly support listen ranges since we don't know
// the amount of neighbors from the get go.
const DefaultListenLimit = 65535

// RouteReflector holds the BGP route reflector parameters of the local
// router (RFC 4456).
type RouteReflector struct {
	ClusterID string
}

// HasListenRange reports whether at least one underlay neighbor accepts
// dynamic sessions via bgp listen range.
func (u UnderlayConfig) HasListenRange() bool {
	for _, n := range u.Neighbors {
		if n.ListenRange != "" {
			return true
		}
	}
	return false
}

// BGPListenLimit returns the dynamic neighbors cap to render, falling back
// to DefaultListenLimit when the limit is not set.
func (u UnderlayConfig) BGPListenLimit() uint16 {
	if u.ListenLimit == 0 {
		return DefaultListenLimit
	}
	return u.ListenLimit
}

type TunnelEndpoint struct {
	IPv4CIDR string
	IPv6CIDR string
}

type UnderlayISIS struct {
	Name                 string
	Net                  ISISNet
	Level                int32
	AdvertisePassiveOnly bool
	Interfaces           []ISISInterface
}

type UnderlaySegmentRouting struct {
	SourceAddress string
	Locator       SRV6Locator
	EncapBehavior string
}

type SRV6Locator struct {
	Name     string
	Prefix   string
	BlockLen int
	NodeLen  int
	Behavior string
	Format   string
}

type PassthroughConfig struct {
	LocalNeighborV4 *NeighborConfig
	LocalNeighborV6 *NeighborConfig
	ToAdvertiseIPv4 []string
	ToAdvertiseIPv6 []string
}

type L3VNIConfig struct {
	ASN             int64
	ToAdvertiseIPv4 []string
	ToAdvertiseIPv6 []string
	LocalNeighbor   *NeighborConfig
	VRF             string
	VNI             int32
	RouterID        string
	ExportRTs       []string
	ImportRTs       []string
}

type L3VPNConfig struct {
	ASN                int64
	ToAdvertiseIPv4    []string
	ToAdvertiseIPv6    []string
	LocalNeighbor      *NeighborConfig
	VRF                string
	RouterID           string
	ExportRTs          []string
	ImportRTs          []string
	RouteDistinguisher string
}

type BFDProfile struct {
	Name             string
	ReceiveInterval  *int32
	TransmitInterval *int32
	DetectMultiplier *int32
	EchoInterval     *int32
	EchoMode         bool
	PassiveMode      bool
	MinimumTTL       *int32
}

type NeighborConfig struct {
	Name                  string
	ASN                   PeerASN
	Addr                  string
	Interface             string
	ID                    string
	Port                  *int32
	HoldTime              *int64
	KeepaliveTime         *int64
	ConnectTime           *int64
	Password              string
	BFDEnabled            bool
	BFDProfile            string
	EBGPMultiHop          bool
	NetworkLayerProtocols []networklayerprotocol.NLP
	// ListenRange turns the neighbor into a peer-group accepting dynamic
	// sessions from the given CIDR via bgp listen range. ID holds the
	// peer-group name.
	ListenRange string
	// Allow bgp to negotiate the extended-nexthop capability with its peer. If you are peering over a v6 LL address
	// then this capability is turned on automatically.
	// If you are peering over a v6 Global Address then turning on this command will allow BGP to install v4 routes
	// with v6 nexthops if you do not have v4 configured on interfaces.
	ExtendedNexthop bool
	UpdateSource    string
}

// ActivateFor tells whether the neighbor activates the given address family.
func (n NeighborConfig) ActivateFor(afi networklayerprotocol.AFI, safi networklayerprotocol.SAFI) bool {
	return networklayerprotocol.HasNLP(n.NetworkLayerProtocols, networklayerprotocol.NLP{AFI: afi, SAFI: safi})
}

// IsRouteReflectorClientFor tells whether the neighbor is a route reflector
// client in the given address family.
func (n NeighborConfig) IsRouteReflectorClientFor(afi networklayerprotocol.AFI, safi networklayerprotocol.SAFI) bool {
	nlp := networklayerprotocol.FindNLP(
		n.NetworkLayerProtocols,
		networklayerprotocol.NLP{
			AFI:  afi,
			SAFI: safi,
		},
	)

	if nlp == nil {
		return false
	}

	return nlp.Properties.RouteReflectorClient
}

// shouldRenderUnderlayEVPN tells whether the underlay needs the l2vpn evpn
// address family block. Data-plane underlays render it whenever a tunnel
// endpoint exists; route-reflector-only nodes render it when a neighbor
// activates the l2vpn evpn family without a tunnel endpoint.
func shouldRenderUnderlayEVPN(underlay UnderlayConfig) bool {
	if underlay.TunnelEndpoint != nil {
		return true
	}
	for _, n := range underlay.Neighbors {
		if n.ActivateFor(networklayerprotocol.L2VPN, networklayerprotocol.EVPN) {
			return true
		}
	}
	return false
}

// templateConfig uses the template library to template
// 'globalConfigTemplate' using 'data'.
func templateConfig(data any) (string, error) {
	counterMap := map[string]int{}
	t, err := template.New("frr.tmpl").Funcs(
		template.FuncMap{
			"counter": func(counterName string) int {
				counter := counterMap[counterName]
				counter++
				counterMap[counterName] = counter
				return counter
			},
			"dict": func(values ...any) (map[string]any, error) {
				if len(values)%2 != 0 {
					return nil, errors.New("invalid dict call, expecting even number of args")
				}
				dict := make(map[string]any, len(values)/2)
				for i := 0; i < len(values); i += 2 {
					key, ok := values[i].(string)
					if !ok {
						return nil, fmt.Errorf("dict keys must be strings, got %v %T", values[i], values[i])
					}
					dict[key] = values[i+1]
				}
				return dict, nil
			},
			"mustDisableConnectedCheck": func(nlps []networklayerprotocol.NLP, myASN int64, peerASN PeerASN,
				eBGPMultiHop bool) bool {
				// Return true only if neighbor establishes an IPv6 eBGP session.
				return networklayerprotocol.HasUnicastFamily(nlps, networklayerprotocol.IPv6) &&
					!eBGPMultiHop && peerASN.IsExternalTo(myASN)
			},
			"isEBGP": func(myASN int64, peerASN PeerASN) bool {
				return peerASN.IsExternalTo(myASN)
			},
			"join": func(s []string) string {
				return strings.Join(s, " ")
			},
			"renderUnderlayEVPN": shouldRenderUnderlayEVPN,
		}).ParseFS(templates, "templates/*")
	if err != nil {
		return "", err
	}

	var b bytes.Buffer
	err = t.Execute(&b, data)
	return b.String(), err
}

// generateAndReloadConfigFile takes a 'struct Config' and, using a template,
// generates and writes a valid FRR configuration file. If this completes
// successfully it will also force FRR to reload that configuration file.
func generateAndReloadConfigFile(ctx context.Context, config *Config, updater ConfigUpdater) error {
	slog.InfoContext(ctx, "frr generate config", "event", "start")
	defer slog.InfoContext(ctx, "frr generate config", "event", "stop")

	slog.DebugContext(ctx, "frr generate config", "config", *config)

	configString, err := templateConfig(config)
	if err != nil {
		slog.Error("failed to generate config from template", "error", err, "cause", "template", "config", config)
		return err
	}
	slog.DebugContext(ctx, "frr generaetd configuration", "config", configString)
	err = updater(ctx, configString)
	if err != nil {
		slog.Error("failed to write frr config", "error", err, "cause", "updater", "config", config)
		return err
	}
	return nil
}
