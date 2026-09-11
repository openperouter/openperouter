// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/openperouter/openperouter/internal/dockertest"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/networklayerprotocol"
)

var tests = map[string]struct {
	failValidate bool
	failReload   bool
}{
	"/tmp/shouldPass": {
		failValidate: false,
		failReload:   false,
	},
	"/tmp/failValidate": {
		failValidate: true,
		failReload:   false,
	},
	"/tmp/failReload": {
		failValidate: false,
		failReload:   true,
	},
}

func TestReload(t *testing.T) {
	execCommand = fakeExecCommand
	defer func() { execCommand = exec.Command }()

	for tc, params := range tests {
		t.Run(fmt.Sprintf("reload %s", tc), func(t *testing.T) {
			err := Update(tc)
			if (params.failReload || params.failValidate) && err == nil {
				t.Fatalf("expecting failure, got no error")
			}
			if params.failReload && !strings.Contains(err.Error(), "reload") {
				t.Fatalf("expecting reload error, got %v", err)
			}
			if params.failValidate && !strings.Contains(err.Error(), "test") {
				t.Fatalf("expecting test error, got %v", err)
			}
			if !params.failReload && !params.failValidate && err != nil {
				t.Fatalf("expecting no error, got %v", err)
			}
		})
	}
}

// This is not a real test. It's used in case fakeExecCommand is used in place of exec.Command.
// In that case the command execution is redirected to this function.
func TestFakeReloadHelper(t *testing.T) {
	if os.Getenv("WANT_FAKE_PYTHON") != "true" {
		return
	}

	args := os.Args

	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	// vtysh calls are invoked as "-c <command>". The reload tests never have
	// ISIS in the running config, so return an empty running config and let the
	// stale-ISIS teardown short-circuit.
	if len(args) > 0 && args[0] == "-c" {
		os.Exit(0)
	}

	if len(args) != 5 {
		fmt.Printf("expecting 5 args, got %v", args)
		os.Exit(1)
	}

	if !reflect.DeepEqual(args[0], reloaderPath) {
		fmt.Println("expected to be called with -c reloader args", args)
		os.Exit(1)
	}
	action, _ := strings.CutPrefix(args[1], "--")
	path := args[4]

	params, ok := tests[path]
	if !ok {
		fmt.Println("received untestable path", path)
		os.Exit(1)
	}
	if params.failReload && action == reload {
		fmt.Println("reload failed")
		os.Exit(1)
	}
	if params.failValidate && action == test {
		fmt.Println("test failed")
		os.Exit(1)
	}

	os.Exit(0)
}

// TestUpdate tests the Update() function by running a full configuration write and reload against an openperouter
// FRR docker container. This test allows us to unit test transitions from and to specific FRR configurations and thus
// to catch any issues with the frr-reload.py script before these issues haunt us in the E2E lanes. The added benefit
// of testing the frrconfig.Update() function is that we can take corrective action inside Update() to address any bugs
// caused by the frr-reload.py script.
func TestUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FRR container integration test")
	}

	tcs := []struct {
		name   string
		before frr.Config
		after  frr.Config
	}{
		{
			name:   "TestBasic",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 64512,
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				BFDProfiles: []frr.BFDProfile{
					{
						Name:            "foo",
						ReceiveInterval: new(int32(43)),
					},
				},
			},
		},
		{
			name:   "L3VNIWithoutLocalNeighborAndAdvertise",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					Neighbors: []frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
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
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
				Passthrough: &frr.PassthroughConfig{
					LocalNeighborV4: &frr.NeighborConfig{
						ASN:         mustNewPeerASNFromNumber(64513),
						Addr:        "192.168.1.3",
						ID:          "192.168.1.3",
						ConnectTime: new(int64(5)),
					},
					LocalNeighborV6: &frr.NeighborConfig{
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
				L3VNIs: []frr.L3VNIConfig{},
			},
		},
		{
			name:   "RawConfig",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64513),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
				RawConfig: []frr.RawFRRSnippet{
					{Priority: new(int32(5)), Config: "ip prefix-list raw-low seq 10 permit 10.0.0.0/8"},
					{Priority: new(int32(20)), Config: "ip prefix-list raw-high seq 10 permit 10.1.0.0/16"},
				},
			},
		},
		{
			name:   "TunnelEndpointConfig",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.10.1/24",
						IPv6CIDR: "2001:db8:192:168::1/64",
					},
				},
				L3VNIs: []frr.L3VNIConfig{
					{
						VRF:      "red",
						ASN:      64512,
						VNI:      100,
						RouterID: "10.0.0.1",
						LocalNeighbor: &frr.NeighborConfig{
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
			name: "SRv6 configuration (test segment-routing standalone, incomplete config)",
			before: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
			},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
					SegmentRouting: &frr.UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1::1",
						Locator: frr.SRV6Locator{
							Name:     "Main",
							Prefix:   "2001:db8::/48",
							BlockLen: 32,
							NodeLen:  16,
							Behavior: "usid",
							Format:   "usid-f3216",
						},
						EncapBehavior: frr.HEncapsRed,
					},
				},
			},
		},
		{
			name:   "TestL2VNIWithRouteTargets",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "100.64.0.1/32",
					},
					Neighbors: []frr.NeighborConfig{
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
				L2VNIs: []frr.L2VNIConfig{
					{
						VNI:       100,
						ExportRTs: []string{"65000:100", "192.0.2.1:100"},
						ImportRTs: []string{"65001:100"},
					},
				},
			},
		},
		{
			name: "ISIS standalone configuration",
			before: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
				},
			},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
					ISIS: &frr.UnderlayISIS{
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Name:  "ISIS",
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: true, IPv6: false},
							{Name: "eth1", IPv4: false, IPv6: true},
							{Name: "eth2", IPv4: true, IPv6: true},
						},
					},
				},
			},
		},
		{
			name:   "SegmentRoutingWithL2VNI",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN: 65000,
					ISIS: &frr.UnderlayISIS{
						Name:  "ISIS",
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: true, IPv6: true},
						},
					},
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
					TunnelEndpoint: &frr.TunnelEndpoint{
						IPv4CIDR: "192.168.123.0/32",
						IPv6CIDR: "2001:db8:1234:5678::/128",
					},
					SegmentRouting: &frr.UnderlaySegmentRouting{
						SourceAddress: "2001:db8:1234:5678::",
						Locator: frr.SRV6Locator{
							Name:     "MAIN",
							Prefix:   "fd00:0:32::/48",
							BlockLen: 32,
							NodeLen:  16,
							Behavior: "usid",
							Format:   "usid-f3216",
						},
						EncapBehavior: frr.HEncapsRed,
					},
				},
				VPNs: []frr.L3VPNConfig{
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						LocalNeighbor: &frr.NeighborConfig{
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
						LocalNeighbor: &frr.NeighborConfig{
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
		},
		{
			name:   "ISISAdvertisePassiveOnly",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
						{
							ASN:                   mustNewPeerASNFromNumber(64512),
							Addr:                  "192.168.1.2",
							ID:                    "192.168.1.2",
							NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast}},
						},
					},
					ISIS: &frr.UnderlayISIS{
						Net:                  frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Name:                 "ISIS",
						Level:                1,
						AdvertisePassiveOnly: true,
						Interfaces: []frr.ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: true, IPv6: false},
							{Name: "eth1", IPv4: false, IPv6: true},
							{Name: "eth2", IPv4: true, IPv6: true},
						},
					},
				},
			},
		},
		{
			name:   "SegmentRouting full configuration",
			before: frr.Config{},
			after: frr.Config{
				Underlay: frr.UnderlayConfig{
					MyASN:    64512,
					RouterID: "10.0.0.1",
					Neighbors: []frr.NeighborConfig{
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
					ISIS: &frr.UnderlayISIS{
						Net:   frr.MustParseISISNet("49.0001.0002.0003.0004.00"),
						Name:  "ISIS",
						Level: 1,
						Interfaces: []frr.ISISInterface{
							{Name: "lo", IPv6: true, IsPassive: true},
							{Name: "eth0", IPv4: false, IPv6: true},
						},
					},
					SegmentRouting: &frr.UnderlaySegmentRouting{
						SourceAddress: "fc00::2:172:31:1:32",
						Locator: frr.SRV6Locator{
							Name:     "MAIN",
							Prefix:   "fd00:0:32::/48",
							BlockLen: 32,
							NodeLen:  16,
							Behavior: "usid",
							Format:   "usid-f3216",
						},
						EncapBehavior: frr.HEncaps,
					},
				},
				VPNs: []frr.L3VPNConfig{
					{
						ASN:             65000,
						ToAdvertiseIPv4: []string{"192.168.2.2/32"},
						ToAdvertiseIPv6: []string{},
						LocalNeighbor: &frr.NeighborConfig{
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
						LocalNeighbor: &frr.NeighborConfig{
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
		},
	}

	dir := t.TempDir()
	configFile := filepath.Join(dir, "frr.conf")

	updaterFn := func(_ context.Context, config string) error {
		if err := os.WriteFile(configFile, []byte(config), 0600); err != nil {
			return fmt.Errorf("failed to write the config to %s", configFile)
		}
		return nil
	}

	reloadFn := func(t *testing.T, config frr.Config) {
		if err := frr.ApplyConfig(context.TODO(), &config, updaterFn); err != nil {
			t.Fatalf("Failed to apply config: %s", err)
		}
		err := update(configFile, dockertest.FrrReload, dockertest.RunVtysh)
		if err != nil {
			t.Fatalf("Failed to update FRR with config: %s", err)
		}
	}

	dockertest.TestWithDockerT(t, func(t *testing.T) {
		for _, tc := range tcs {
			t.Run(fmt.Sprintf("add %s", tc.name), func(t *testing.T) {
				reloadFn(t, tc.before)
				reloadFn(t, tc.after)
			})
			t.Run(fmt.Sprintf("remove %s", tc.name), func(t *testing.T) {
				reloadFn(t, tc.after)
				reloadFn(t, tc.before)
			})
		}
	})
}

// helper function that redirects the execution to a mock process implemented by
// TestHelperProcess
func fakeExecCommand(name string, args ...string) *exec.Cmd {
	//nolint:prealloc
	cs := []string{"-test.run=TestFakeReloadHelper", "--"}
	cs = append(cs, args...)
	env := []string{
		"WANT_FAKE_PYTHON=true",
	}

	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(env, os.Environ()...)
	return cmd
}

func mustNewPeerASNFromNumber(number int64) frr.PeerASN {
	if number == 0 {
		panic("number must be > 0")
	}
	asn, err := frr.NewPeerASN(&number, nil)
	if err != nil {
		panic(err)
	}
	return asn
}

func mustNewPeerASNFromType(t string) frr.PeerASN {
	asn, err := frr.NewPeerASN(nil, &t)
	if err != nil {
		panic(err)
	}
	return asn
}
