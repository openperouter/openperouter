// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package bridgerefresh

import (
	"context"
	"fmt"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var _ = Describe("sendPing on a trunk-carried L2VNI", func() {
	const (
		testNSName  = "trunkbind"
		trunkID     = "tenant-a"
		trunkName   = "workloads"
		l2vni       = int32(10100)
		l2vniTag    = int32(100)
		gatewayCIDR = "192.169.10.1/24"
		peerIP      = "192.169.10.99"
	)

	testNSPath := func() string {
		return fmt.Sprintf("/var/run/netns/%s", testNSName)
	}

	var (
		ns  netns.NsHandle
		cfg *hostnetwork.VNIConfigurator
	)

	BeforeEach(func() {
		cleanTest(testNSName)
		ns = createTestNS(testNSName)
		createTunnelLoopback(ns)

		var err error
		cfg, err = hostnetwork.NewVNIConfigurator(ns, &hostnetwork.OVSClient{})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(ns.Close()).To(Succeed())
		cleanTest(testNSName)
	})

	It("binds to the br-pe endpoint that sits on the trunk bridge", func() {
		trunkParams := hostnetwork.L2TrunkParams{
			Name:      trunkName,
			Namespace: trunkID,
			TargetNS:  testNSPath(),
			VTEPIP:    "192.170.0.9/32",
			VXLanPort: 4789,
		}
		Expect(cfg.SetupL2Trunk(context.Background(), trunkParams)).To(Succeed())
		Expect(cfg.SetupL2TrunkVNI(context.Background(), hostnetwork.L2VNIParams{
			VNIParams: hostnetwork.VNIParams{
				TargetNS:  testNSPath(),
				VTEPIP:    "192.170.0.9/32",
				VNI:       l2vni,
				VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{gatewayCIDR},
			Trunk: &hostnetwork.L2VNITrunkParams{
				TrunkID: hostnetwork.TrunkID(trunkID, trunkName),
				Name:    trunkName,
				VLAN:    l2vniTag,
			},
		})).To(Succeed())

		refresher := &BridgeRefresher{
			bridgeName: hostnetwork.BridgeName(l2vni),
			namespace:  testNSPath(),
		}

		err := netnamespace.In(ns, func() error {
			return refresher.sendPing(net.ParseIP(peerIP))
		})
		Expect(err).NotTo(HaveOccurred())
	})
})

func createTunnelLoopback(ns netns.NsHandle) {
	GinkgoHelper()

	handle, err := netlink.NewHandleAt(ns)
	Expect(err).NotTo(HaveOccurred())
	defer handle.Close()

	tunnelEndpoint := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: hostnetwork.TunnelLoopback}}
	Expect(handle.LinkAdd(tunnelEndpoint)).To(Succeed())
	Expect(handle.LinkSetUp(tunnelEndpoint)).To(Succeed())
}
