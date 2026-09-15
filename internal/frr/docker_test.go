// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"errors"

	"github.com/openperouter/openperouter/internal/networklayerprotocol"
)

func TestDockerFRRFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FRR integration")
	}

	badFile := filepath.Join(testData, "TestDockerTestfails.golden")
	err := frrReload(badFile, "test")
	if !errors.As(err, &invalidFileErr{}) {
		t.Fatalf("Validity check of invalid file passed")
	}
}

func TestReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FRR integration")
	}

	tcs := []struct {
		name   string
		before Config
		after  Config
	}{
		{
			name:   "TestBasic",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
					},
				},
			},
		},
		{
			name:   "TestBasicWithASNRT",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
						ExportRTs: []string{"65000:1000"},
						ImportRTs: []string{"65000:1000"},
					},
				},
			},
		},
		{
			name:   "TestBasicWithIPRT",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
						ExportRTs: []string{"10.0.0.1:1000"},
						ImportRTs: []string{"10.0.0.1:1000"},
					},
				},
			},
		},
		{
			name:   "TestExternal",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromType("External"),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromType("External"),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
					},
				},
			},
		},
		{
			name:   "TestInternal",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromType("Internal"),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64512),
							Addr: "192.168.1.3",
							ID:   "192.168.1.3",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
					},
				},
			},
		},
		{
			name:   "TestDualStack",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8::2/64",
						},
					},
				},
			},
		},
		{
			name:   "TestDualStackWithRT",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8::2/64",
						},
						ExportRTs: []string{"65000:1000", "10.0.0.1:1000"},
						ImportRTs: []string{"65000:1000", "10.0.0.1:1000"},
					},
				},
			},
		},
		{
			name:   "TestIPv6Only",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							ExtendedNexthop: true,
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8::2/64",
						},
					},
				},
			},
		},
		{
			name:   "TestBGPUnnumbered",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:       mustNewPeerASNFromNumber(64512),
							Interface: "eth1",
							ID:        "eth1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							ExtendedNexthop: true,
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64512),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8::2/64",
						},
					},
				},
			},
		},
		{
			name:   "BGPUnnumberedEBGPIPv6",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:       mustNewPeerASNFromNumber(64513),
							Interface: "eth1",
							ID:        "eth1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							ExtendedNexthop: true,
						},
					},
				},
			},
		},
		{
			name:   "IPv6OnlyWithRT",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						ToAdvertiseIPv6: []string{
							"2001:db8::2/64",
						},
						ExportRTs: []string{"65000:1000"},
						ImportRTs: []string{"65000:1000"},
					},
				},
			},
		},
		{
			name:   "NoL3VNIs",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
						},
					},
				},
			},
		},
		{
			name:   "BFDEnabled",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
							BFDEnabled: true,
						},
					},
				},
			},
		},
		{
			name:   "BFDProfile",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
							},
							BFDEnabled: true,
							BFDProfile: "foo",
						},
					},
				},
				BFDProfiles: []BFDProfile{
					{
						Name:            "foo",
						ReceiveInterval: new(int32(43)),
					},
				},
			},
		},
		{
			name:   "L3VNIWithoutLocalNeighborAndAdvertise",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						RouterID: "10.0.0.1",
						VRF:      "red",
						VNI:      100,
						ASN:      64512,
					},
				},
			},
		},
		{
			name:   "L3VNIWithLocalNeighborAndRedistributeConnected",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
					},
				},
			},
		},
		{
			name:   "PassthroughNoEVPN",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				Passthrough: &PassthroughConfig{
					LocalNeighborV4: &NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(64513),
						Addr:        "192.168.1.3",
						ID:          "192.168.1.3",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{
						"192.169.20.0/24",
						"192.169.21.0/24",
					},
				},
			},
		},
		{
			name:   "PassthroughExternal",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				Passthrough: &PassthroughConfig{
					LocalNeighborV4: &NeighborConfig{
						ASN:         mustNewPeerASNFromType("External"),
						Addr:        "192.168.1.3",
						ID:          "192.168.1.3",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{
						"192.169.20.0/24",
						"192.169.21.0/24",
					},
				},
			},
		},
		{
			name:   "PassthroughV4",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				Passthrough: &PassthroughConfig{
					LocalNeighborV4: &NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(64513),
						Addr:        "192.168.1.3",
						ID:          "192.168.1.3",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{
						"192.169.20.0/24",
						"192.169.21.0/24",
					},
				},
			},
		},
		{
			name:   "PassthroughDual",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{ // Override to only IPv4 (auto-detection would be dualstack).
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
							},
						},
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "2001:db8::1",
							ID:   "2001:db8::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
							},
						},
					},
				},
				Passthrough: &PassthroughConfig{
					LocalNeighborV4: &NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(64513),
						Addr:        "192.168.1.3",
						ID:          "192.168.1.3",
						ConnectTime: new(int64(5)),
					},
					LocalNeighborV6: &NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(64513),
						Addr:        "2001:db8:20::2",
						ID:          "2001:db8:20::2",
						ConnectTime: new(int64(5)),
					},
					ToAdvertiseIPv4: []string{
						"192.169.20.0/24",
						"192.169.21.0/24",
					},
					ToAdvertiseIPv6: []string{
						"2001:db8:20::/64",
						"2001:db8:21::/64",
					},
				},
				L3VNIs: []L3VNIConfig{},
			},
		},
		{
			name:   "RawConfig",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				RawConfig: []RawFRRSnippet{
					{Priority: new(int32(5)), Config: "ip prefix-list raw-low seq 10 permit 10.0.0.0/8"},
					{Priority: new(int32(20)), Config: "ip prefix-list raw-high seq 10 permit 10.1.0.0/16"},
				},
			},
		},
		{
			name:   "TunnelEndpointConfig",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "192.168.10.1/24",
						IPv6CIDR: "2001:db8:192:168::1/64",
					},
				},
				L3VNIs: []L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
						},
						ToAdvertiseIPv4: []string{
							"192.169.10.2/24",
						},
					},
				},
			},
		},
		/* Disabled due to https://github.com/openperouter/openperouter/issues/645
		{
			name:   "ISISAdvertisePassiveOnly",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
					ISIS: &UnderlayISIS{
						Net:                  MustParseISISNet("49.0001.0002.0003.0004.00"),
						Name:                 isisProcessName,
						Level:                1,
						AdvertisePassiveOnly: true,
						Interfaces: []ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: true, IPv6: false},
							{Name: "eth1", IPv4: false, IPv6: true},
							{Name: "eth2", IPv4: true, IPv6: true},
						},
					},
				},
			},
		},*/
		/* Disabled due to https://github.com/openperouter/openperouter/issues/645
		{
			name:   "SegmentRouting full configuration",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "fc00::2:172:31:1:12",
							ID:   "fc00::2:172:31:1:12",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN},
							},
							ExtendedNexthop: true,
							UpdateSource:    "fc00::2:172:31:1:32",
						},
					},
					ISIS: &UnderlayISIS{
						Net:   MustParseISISNet("49.0001.0002.0003.0004.00"),
						Name:  isisProcessName,
						Level: 1,
						Interfaces: []ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: false, IPv6: true},
						},
					},
					SegmentRouting: &UnderlaySegmentRouting{
						SourceAddress: "fc00::2:172:31:1:32",
						Locator: SRV6Locator{
							Name:     locatorName,
							Prefix:   "fd00:0:32::/48",
							BlockLen: 32,
							NodeLen:  16,
							Behavior: "usid",
							Format:   "usid-f3216",
						},
						EncapBehavior: HEncaps,
					},
				},
				VPNs: []L3VPNConfig{
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.2.2",
							ID:   "192.168.2.2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100 65000:101"},
						ImportRTs:          []string{"65001:102 65001:103"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						VRF:                "vrf2",
						ExportRTs:          []string{"65002:100 65002:101"},
						ImportRTs:          []string{"65003:102 65003:103"},
						RouteDistinguisher: "10.0.0.1:101",
						RouterID:           "10.0.0.1",
					},
				},
			},
		},*/
		/* Disabled due to https://github.com/openperouter/openperouter/issues/645
		{
			name:   "SegmentRoutingWithL2VNI",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN: 65000,
					ISIS: &UnderlayISIS{
						Name:  isisProcessName,
						Net:   MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 1,
						Interfaces: []ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: true, IPv6: true},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							Name: "65001@192.168.122.1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.122.1",
							ID:   "192.168.122.1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
							EBGPMultiHop:    false,
							ExtendedNexthop: false,
						},
						{
							Name: "65001@2001:db8:192:168:1::1",
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8:192:168:1::1",
							ID:   "2001:db8:192:168:1::1",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.Unicast},
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
								{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN},
								{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN},
							},
							EBGPMultiHop:    false,
							ExtendedNexthop: true,
							UpdateSource:    "2001:db8:1234:5678::",
						},
					},
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "192.168.123.0/32",
						IPv6CIDR: "2001:db8:1234:5678::/128",
					},
					SegmentRouting: &UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1234:5678::",
						Locator: SRV6Locator{
							Name:     locatorName,
							Prefix:   "fd00:0:32::/48",
							BlockLen: 32,
							NodeLen:  16,
							Behavior: "usid",
							Format:   "usid-f3216",
						},
						EncapBehavior: HEncapsRed,
					},
				},
				VPNs: []L3VPNConfig{
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "192.168.2.2",
							ID:   "192.168.2.2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100 11110:100"},
						ImportRTs:          []string{"65001:100 11111:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{},
						ToAdvertiseIPv6: []string{"2001:db8::2/128"},
						LocalNeighbor: &NeighborConfig{
							ASN:  mustNewPeerASNFromNumber(65001),
							Addr: "2001:db8::2",
							ID:   "2001:db8::2",
						},
						VRF:                "vrf1",
						ExportRTs:          []string{"65000:100 11110:100"},
						ImportRTs:          []string{"65001:100 11111:100"},
						RouteDistinguisher: "10.0.0.1:100",
						RouterID:           "10.0.0.1",
					},
				},
			},
		},*/
		{
			name: "SRv6 configuration (test segment-routing standalone, incomplete config)",
			before: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
			},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
					SegmentRouting: &UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1::1",
						Locator: SRV6Locator{
							Name:     "Main",
							Prefix:   "2001:db8::/48",
							BlockLen: 32,
							NodeLen:  16,
							Behavior: "usid",
							Format:   "usid-f3216",
						},
						EncapBehavior: HEncapsRed,
					},
				},
			},
		},
		/* Disabled due to https://github.com/openperouter/openperouter/issues/645
		{
			name: "ISIS standalone configuration",
			before: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
			},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
					ISIS: &UnderlayISIS{
						Net:   MustParseISISNet("49.0001.0002.0003.0004.00"),
						Name:  isisProcessName,
						Level: 1,
						Interfaces: []ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: true, IPv6: false},
							{Name: "eth1", IPv4: false, IPv6: true},
							{Name: "eth2", IPv4: true, IPv6: true},
						},
					},
				},
			},
		},*/
		{
			name:   "TestL2VNIWithRouteTargets",
			before: Config{},
			after: Config{
				Underlay: UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					TunnelEndpoint: &TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					Neighbors: []NeighborConfig{
						{
							ASN:  mustNewPeerASNFromNumber(64513),
							Addr: "192.168.1.2",
							ID:   "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{
								{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
							},
						},
					},
				},
				L2VNIs: []L2VNIConfig{
					{
						VNI:       100,
						ExportRTs: []string{"65000:100", "192.0.2.1:100"},
						ImportRTs: []string{"65001:100"},
					},
				},
			},
		},
	}

	dir := t.TempDir()
	configFile := filepath.Join(dir, "frr.conf")
	updater := testUpdater(configFile)

	reload := func(t *testing.T, config Config, configFile string, updater func(context.Context, string) error) {
		if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
			t.Fatalf("Failed to apply config: %s", err)
		}
		err := frrReload(configFile, "test")
		if err != nil {
			t.Fatalf("Failed to test reload FRR with config: %s", err)
		}
		err = frrReload(configFile, "reload")
		if err != nil {
			t.Fatalf("Failed to reload FRR with config: %s", err)
		}
	}

	for _, tc := range tcs {
		t.Run(fmt.Sprintf("add %s", tc.name), func(t *testing.T) {
			reload(t, tc.before, configFile, updater)
			reload(t, tc.after, configFile, updater)
		})
		t.Run(fmt.Sprintf("remove %s", tc.name), func(t *testing.T) {
			reload(t, tc.after, configFile, updater)
			reload(t, tc.before, configFile, updater)
		})
	}
}
