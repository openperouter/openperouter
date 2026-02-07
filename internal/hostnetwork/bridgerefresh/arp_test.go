// SPDX-License-Identifier:Apache-2.0

//go:build runasroot
// +build runasroot

package bridgerefresh

import (
	"fmt"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netns"
)

var _ = Describe("sendARPProbe", func() {
	const testNSName = "arptest"

	testNSPath := func() string {
		return fmt.Sprintf("/var/run/netns/%s", testNSName)
	}

	BeforeEach(func() {
		createTestNS(testNSName)
	})

	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should send ARP probe successfully", func() {
		createTestBridge(testNSPath(), "br-pe-400")
		addIPToBridge(testNSPath(), "br-pe-400", "192.168.1.1/24")
		bridgeMAC, _ := net.ParseMAC("02:00:00:00:04:00")
		setBridgeMAC(testNSPath(), "br-pe-400", bridgeMAC)

		gatewayIP := net.ParseIP("192.168.1.1").To4()
		targetIP := net.ParseIP("192.168.1.10")
		targetMAC, _ := net.ParseMAC("02:00:00:00:00:01")

		refresher := &BridgeRefresher{
			bridgeName: "br-pe-400",
			namespace:  testNSPath(),
			gatewayIPs: []net.IP{gatewayIP},
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		err = netnamespace.In(ns, func() error {
			return refresher.sendARPProbe(targetIP, targetMAC)
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should error when no gateway IPs configured", func() {
		createTestBridge(testNSPath(), "br-pe-401")

		targetIP := net.ParseIP("192.168.1.10")
		targetMAC, _ := net.ParseMAC("02:00:00:00:00:01")

		refresher := &BridgeRefresher{
			bridgeName: "br-pe-401",
			namespace:  testNSPath(),
			gatewayIPs: []net.IP{}, // Empty
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		err = netnamespace.In(ns, func() error {
			return refresher.sendARPProbe(targetIP, targetMAC)
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no gateway IPs configured"))
	})

	It("should error for non-existent bridge", func() {
		gatewayIP := net.ParseIP("192.168.1.1").To4()
		targetIP := net.ParseIP("192.168.1.10")
		targetMAC, _ := net.ParseMAC("02:00:00:00:00:01")

		refresher := &BridgeRefresher{
			bridgeName: "nonexistent-bridge",
			namespace:  testNSPath(),
			gatewayIPs: []net.IP{gatewayIP},
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		err = netnamespace.In(ns, func() error {
			return refresher.sendARPProbe(targetIP, targetMAC)
		})
		Expect(err).To(HaveOccurred())
	})

	It("should error for IPv6 target IP", func() {
		createTestBridge(testNSPath(), "br-pe-402")
		addIPToBridge(testNSPath(), "br-pe-402", "192.168.1.1/24")
		bridgeMAC, _ := net.ParseMAC("02:00:00:00:04:02")
		setBridgeMAC(testNSPath(), "br-pe-402", bridgeMAC)

		gatewayIP := net.ParseIP("192.168.1.1").To4()
		targetIP := net.ParseIP("2001:db8::10") // IPv6
		targetMAC, _ := net.ParseMAC("02:00:00:00:00:01")

		refresher := &BridgeRefresher{
			bridgeName: "br-pe-402",
			namespace:  testNSPath(),
			gatewayIPs: []net.IP{gatewayIP},
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		err = netnamespace.In(ns, func() error {
			return refresher.sendARPProbe(targetIP, targetMAC)
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not IPv4"))
	})
})
