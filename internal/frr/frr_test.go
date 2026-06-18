// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openperouter/openperouter/internal/networklayerprotocol"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	testData        = "testdata/"
	isisProcessName = "ISIS"
	locatorName     = "MAIN"
)

var update = flag.Bool("update", false, "update .golden files")

func TestBasic(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestBasicWithASNRT(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestBasicWithIPRT(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestExternal(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64512,
			TunnelEndpoint: &TunnelEndpoint{
				IPv4CIDR: "100.64.0.1/32",
			},
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:  mustNewPeerASNFromType("external"),
					Addr: "192.168.1.2",
					ID:   "192.168.1.2",
					NetworkLayerProtocols: []networklayerprotocol.NLP{
						{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
						{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
					},
				},
			},
		},
		VNIs: []L3VNIConfig{
			{
				VRF:      "red",
				ASN:      64512,
				VNI:      100,
				RouterID: "10.0.0.1",
				LocalNeighbor: &NeighborConfig{
					ASN:  mustNewPeerASNFromType("external"),
					Addr: "192.168.1.2",
					ID:   "192.168.1.2",
				},
				ToAdvertiseIPv4: []string{
					"192.169.10.2/24",
				},
			},
		},
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestInternal(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64512,
			TunnelEndpoint: &TunnelEndpoint{
				IPv4CIDR: "100.64.0.1/32",
			},
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:  mustNewPeerASNFromType("internal"),
					Addr: "192.168.1.2",
					ID:   "192.168.1.2",
					NetworkLayerProtocols: []networklayerprotocol.NLP{
						{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
						{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
					},
				},
			},
		},
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestDualStack(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestDualStackWithRT(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestIPv6Only(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestBGPUnnumbered(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestIPv6OnlyWithRT(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestEmpty(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestNoVNIs(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestBFDEnabled(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestBFDProfile(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestL3VNIWithoutLocalNeighborAndAdvertise(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
			{
				RouterID: "10.0.0.1",
				VRF:      "red",
				VNI:      100,
				ASN:      64512,
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestL3VNIWithLocalNeighborAndRedistributeConnected(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestPassthroughNoEVPN(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestPassthroughExternal(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
				ASN:         mustNewPeerASNFromType("external"),
				Addr:        "192.168.1.3",
				ID:          "192.168.1.3",
				ConnectTime: new(int64(5)),
			},
			ToAdvertiseIPv4: []string{
				"192.169.20.0/24",
				"192.169.21.0/24",
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestPassthroughV4(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestPassthroughDual(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestRawConfig(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

// TestTunnelEndpointConfig tests that both IPv4 and IPv6 networks are advertised via FRR.
func TestTunnelEndpointConfig(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
		VNIs: []L3VNIConfig{
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestISIS(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestISISAdvertisePassiveOnly(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSegmentRouting(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestSegmentRoutingWithL2VNI(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
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
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func testCompareFiles(t *testing.T, configFile, goldenFile string) {
	var lastError error

	// Try comparing files multiple times because tests can generate more than one configuration
	err := wait.PollUntilContextTimeout(context.Background(), 10*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		lastError = nil
		cmd := exec.Command("diff", configFile, goldenFile)
		output, err := cmd.Output()

		if err != nil {
			lastError = fmt.Errorf("command %s returned error: %s\n%s", cmd.String(), err, output)
			return false, nil
		}

		return true, nil
	})

	// err can only be a ErrWaitTimeout, as the check function always return nil errors.
	// So lastError is always set
	if err != nil {
		t.Fatalf("failed to compare configfiles %s, %s using poll interval\nlast error: %v", configFile, goldenFile, lastError)
	}
}

func testUpdateGoldenFile(t *testing.T, configFile, goldenFile string) {
	t.Log("update golden file")

	// No other conditions can be checked, so sleeping is our best option.
	time.Sleep(100 * time.Millisecond) //nolint:forbidigo // debouncer has no observable condition to poll

	cmd := exec.Command("cp", "-a", configFile, goldenFile)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("command %s returned %s and error: %s", cmd.String(), output, err)
	}
}

func testGenerateFileNames(t *testing.T) (string, string) {
	return filepath.Join(testData, filepath.FromSlash(t.Name())), filepath.Join(testData, filepath.FromSlash(t.Name())+".golden")
}

func testSetup(t *testing.T) string {
	configFile, _ := testGenerateFileNames(t)
	_ = os.Remove(configFile) // removing leftovers from previous runs
	return configFile
}

func testCheckConfigFile(t *testing.T) {
	configFile, goldenFile := testGenerateFileNames(t)

	if *update {
		testUpdateGoldenFile(t, configFile, goldenFile)
	}

	testCompareFiles(t, configFile, goldenFile)

	if !strings.Contains(configFile, "Invalid") {
		err := testFileIsValid(configFile)
		if err != nil {
			t.Fatalf("Failed to verify the file %q", err)
		}
	}
}

func TestGracefulRestart(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64512,
			TunnelEndpoint: &TunnelEndpoint{
				IPv4CIDR: "100.64.0.1/32",
			},
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:                   mustNewPeerASNFromNumber(64512),
					Addr:                  "192.168.1.2",
					ID:                    "192.168.1.2",
					NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
				},
			},
			GracefulRestart: &GracefulRestart{
				RestartTime:   120,
				StalePathTime: 360,
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestGracefulRestartCustomTimers(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64512,
			TunnelEndpoint: &TunnelEndpoint{
				IPv4CIDR: "100.64.0.1/32",
			},
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:                   mustNewPeerASNFromNumber(64512),
					Addr:                  "192.168.1.2",
					ID:                    "192.168.1.2",
					NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
				},
			},
			GracefulRestart: &GracefulRestart{
				RestartTime:   90,
				StalePathTime: 180,
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestRouteReflector(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	evpn := networklayerprotocol.NLP{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN}
	ipv4 := networklayerprotocol.NLP{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}
	evpnWithRRClient := networklayerprotocol.NLP{AFI: evpn.AFI, SAFI: evpn.SAFI,
		Properties: networklayerprotocol.NLPProperties{RouteReflectorClient: true}}

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64514,
			TunnelEndpoint: &TunnelEndpoint{
				IPv4CIDR: "100.65.0.0/32",
			},
			RouterID: "10.0.0.1",
			RouteReflector: &RouteReflector{
				ClusterID: "192.0.2.1",
			},
			Neighbors: []NeighborConfig{
				{
					ASN:                   mustNewPeerASNFromNumber(64512),
					Addr:                  "192.168.10.2",
					ID:                    "192.168.10.2",
					NetworkLayerProtocols: []networklayerprotocol.NLP{ipv4, evpn},
				},
				{
					ASN:                   mustNewPeerASNFromType("internal"),
					ListenRange:           "192.168.10.0/24",
					ID:                    "192.168.10.0/24",
					NetworkLayerProtocols: []networklayerprotocol.NLP{evpnWithRRClient},
				},
				{
					ASN:                   mustNewPeerASNFromType("internal"),
					ListenRange:           "fd00:10::/64",
					ID:                    "fd00:10::/64",
					ExtendedNexthop:       true,
					NetworkLayerProtocols: []networklayerprotocol.NLP{evpnWithRRClient},
				},
				{
					ASN:                   mustNewPeerASNFromType("internal"),
					Addr:                  "192.168.10.4",
					ID:                    "192.168.10.4",
					NetworkLayerProtocols: []networklayerprotocol.NLP{ipv4, evpn},
				},
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestRouteReflectorOnly(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	evpn := networklayerprotocol.NLP{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN}
	evpnWithRRClient := networklayerprotocol.NLP{AFI: evpn.AFI, SAFI: evpn.SAFI,
		Properties: networklayerprotocol.NLPProperties{RouteReflectorClient: true}}

	config := Config{
		Underlay: UnderlayConfig{
			MyASN:    64514,
			RouterID: "10.0.0.1",
			RouteReflector: &RouteReflector{
				ClusterID: "192.0.2.1",
			},
			Neighbors: []NeighborConfig{
				{
					ASN:                   mustNewPeerASNFromType("internal"),
					ListenRange:           "192.168.11.0/24",
					ID:                    "192.168.11.0/24",
					NetworkLayerProtocols: []networklayerprotocol.NLP{evpnWithRRClient},
				},
				{
					ASN:                   mustNewPeerASNFromType("internal"),
					Addr:                  "192.168.10.4",
					ID:                    "192.168.10.4",
					NetworkLayerProtocols: []networklayerprotocol.NLP{evpn},
				},
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestRouteReflectorL3VPN(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	ipv4vpnWithRRClient := networklayerprotocol.NLP{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN,
		Properties: networklayerprotocol.NLPProperties{RouteReflectorClient: true}}
	ipv6vpnWithRRClient := networklayerprotocol.NLP{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN,
		Properties: networklayerprotocol.NLPProperties{RouteReflectorClient: true}}

	config := Config{
		Underlay: UnderlayConfig{
			MyASN:    64514,
			RouterID: "10.0.0.1",
			RouteReflector: &RouteReflector{
				ClusterID: "192.0.2.1",
			},
			Neighbors: []NeighborConfig{
				{
					ASN:                   mustNewPeerASNFromType("internal"),
					Addr:                  "fc00::2:172:31:1:12",
					ID:                    "fc00::2:172:31:1:12",
					NetworkLayerProtocols: []networklayerprotocol.NLP{ipv4vpnWithRRClient, ipv6vpnWithRRClient},
					ExtendedNexthop:       true,
					UpdateSource:          "fc00::2:172:31:1:32",
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
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestListenRangeEBGP(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	evpn := networklayerprotocol.NLP{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN}
	ipv4 := networklayerprotocol.NLP{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64514,
			TunnelEndpoint: &TunnelEndpoint{
				IPv4CIDR: "100.65.0.0/32",
			},
			RouterID: "10.0.0.1",
			Neighbors: []NeighborConfig{
				{
					ASN:                   mustNewPeerASNFromNumber(64512),
					ListenRange:           "192.168.11.0/24",
					ID:                    "192.168.11.0/24",
					NetworkLayerProtocols: []networklayerprotocol.NLP{ipv4, evpn},
				},
				{
					ASN:                   mustNewPeerASNFromNumber(64513),
					ListenRange:           "192.168.12.0/24",
					ID:                    "192.168.12.0/24",
					NetworkLayerProtocols: []networklayerprotocol.NLP{ipv4, evpn},
				},
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestListenRangeCustomLimit(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	ipv4 := networklayerprotocol.NLP{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64514,
			TunnelEndpoint: &TunnelEndpoint{
				IPv4CIDR: "100.65.0.0/32",
			},
			RouterID:    "10.0.0.1",
			ListenLimit: 512,
			Neighbors: []NeighborConfig{
				{
					ASN:                   mustNewPeerASNFromNumber(64512),
					ListenRange:           "192.168.11.0/24",
					ID:                    "192.168.11.0/24",
					NetworkLayerProtocols: []networklayerprotocol.NLP{ipv4},
				},
			},
		},
	}
	if err := ApplyConfig(context.Background(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func testUpdater(configFile string) func(context.Context, string) error {
	return func(_ context.Context, config string) error {
		err := os.WriteFile(configFile, []byte(config), 0600)
		if err != nil {
			return fmt.Errorf("failed to write the config to %s", configFile)
		}
		return nil
	}
}

func mustNewPeerASNFromNumber(number int64) PeerASN {
	if number == 0 {
		panic("number must be > 0")
	}
	asn, err := NewPeerASN(&number, nil)
	if err != nil {
		panic(err)
	}
	return asn
}

func mustNewPeerASNFromType(t string) PeerASN {
	asn, err := NewPeerASN(nil, &t)
	if err != nil {
		panic(err)
	}
	return asn
}
