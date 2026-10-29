// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package bridgerefresh

import (
	"context"
	"fmt"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var _ = Describe("VLAN gateway neighbor refresher", func() {
	const namespace = "vlanrefreshtest"
	var ns netns.NsHandle
	BeforeEach(func() { ns = createTestNS(namespace) })
	AfterEach(func() {
		StopAll()
		cleanTest(namespace)
		Expect(ns.Close()).To(Succeed())
	})

	It("finds both neighbor families and sends probes through the current API gateway endpoint", func() {
		params := hostnetwork.L2VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VNI: 65000, VRF: "gatewayvrf", TargetNS: fmt.Sprintf("/var/run/netns/%s", namespace),
				VTEPIP: "198.51.100.1/32", VXLanPort: new(int32(4789)),
			},
			L2GatewayIPs: []string{"192.0.2.1/24", "2001:db8::1/64"},
		}
		Expect(netnamespace.In(ns, func() error {
			vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: params.VRF}, Table: 1001}
			Expect(netlink.LinkAdd(vrf)).To(Succeed())
			Expect(netlink.LinkSetUp(vrf)).To(Succeed())
			underlay := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "underlay", MTU: 1500}}
			Expect(netlink.LinkAdd(underlay)).To(Succeed())
			Expect(netlink.LinkSetGroup(underlay, int(hostnetwork.UnderlayGroupID))).To(Succeed())
			Expect(netlink.LinkSetUp(underlay)).To(Succeed())
			Expect(hostnetwork.AssignIPToInterface(underlay, params.VTEPIP)).To(Succeed())
			return nil
		})).To(Succeed())
		Expect(hostnetwork.SetupL2VNI(context.Background(), &hostnetwork.OVSClient{}, params)).To(Succeed())
		refresher, err := New(params, StartOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(refresher.bridgeName).To(Equal("br-pe-65000"))
		testMAC, err := net.ParseMAC("02:00:00:00:00:41")
		Expect(err).NotTo(HaveOccurred())
		targets := []net.IP{net.ParseIP("192.0.2.10").To4(), net.ParseIP("2001:db8::10")}
		for _, target := range targets {
			addStaleNeighbor(params.TargetNS, refresher.bridgeName, target, testMAC)
		}
		Expect(netnamespace.In(ns, func() error {
			endpoint, err := netlink.LinkByName(refresher.bridgeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoint).To(BeAssignableToTypeOf(&netlink.Vlan{}))
			neighbors, err := refresher.listStaleNeighbors()
			Expect(err).NotTo(HaveOccurred())
			Expect(neighbors).To(HaveLen(2))
			for _, target := range targets {
				Expect(neighbors).To(ContainElement(And(
					HaveField("IP", target), HaveField("LinkIndex", endpoint.Attrs().Index),
				)))
			}
			Expect(refresher.sendPing(targets[0])).To(Succeed())
			return nil
		})).To(Succeed())
		Eventually(func() error {
			return netnamespace.In(ns, func() error { return refresher.sendPing(targets[1]) })
		}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
		Expect(StartForVNI(context.Background(), params)).To(Succeed())
		Expect(ActiveCount()).To(Equal(1))
		Expect(StartForVNI(context.Background(), params)).To(Succeed())
		Expect(ActiveCount()).To(Equal(1))
		StopForRemovedVNIs([]hostnetwork.L2VNIParams{params})
		Expect(ActiveCount()).To(Equal(1))
		Expect(StopForVNI(params.VNI)).To(Succeed())
		Expect(ActiveCount()).To(BeZero())
	})
})
