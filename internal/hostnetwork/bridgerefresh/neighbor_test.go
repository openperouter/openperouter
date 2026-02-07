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
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var _ = Describe("listStaleNeighbors", func() {
	const testNSName = "neighbortest"

	testNSPath := func() string {
		return fmt.Sprintf("/var/run/netns/%s", testNSName)
	}

	BeforeEach(func() {
		createTestNS(testNSName)
	})

	AfterEach(func() {
		cleanTest(testNSName)
	})

	It("should return empty list when no stale neighbors", func() {
		createTestBridge(testNSPath(), "br-pe-300")
		addIPToBridge(testNSPath(), "br-pe-300", "192.168.1.1/24")

		refresher := &BridgeRefresher{
			bridgeName: "br-pe-300",
			namespace:  testNSPath(),
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		var neighbors []netlink.Neigh
		err = netnamespace.In(ns, func() error {
			var err error
			neighbors, err = refresher.listStaleNeighbors()
			return err
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(neighbors).To(BeEmpty())
	})

	It("should return stale neighbors with MAC and IP", func() {
		createTestBridge(testNSPath(), "br-pe-301")
		addIPToBridge(testNSPath(), "br-pe-301", "192.168.1.1/24")

		testIP := net.ParseIP("192.168.1.10")
		testMAC, _ := net.ParseMAC("02:00:00:00:00:01")
		addStaleNeighbor(testNSPath(), "br-pe-301", testIP, testMAC)

		refresher := &BridgeRefresher{
			bridgeName: "br-pe-301",
			namespace:  testNSPath(),
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		var neighbors []netlink.Neigh
		err = netnamespace.In(ns, func() error {
			var err error
			neighbors, err = refresher.listStaleNeighbors()
			return err
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(neighbors).To(HaveLen(1))
		Expect(neighbors[0].IP.String()).To(Equal(testIP.String()))
		Expect(neighbors[0].HardwareAddr.String()).To(Equal(testMAC.String()))
	})

	It("should return multiple stale neighbors", func() {
		createTestBridge(testNSPath(), "br-pe-302")
		addIPToBridge(testNSPath(), "br-pe-302", "192.168.1.1/24")

		testIP1 := net.ParseIP("192.168.1.10")
		testMAC1, _ := net.ParseMAC("02:00:00:00:00:01")
		addStaleNeighbor(testNSPath(), "br-pe-302", testIP1, testMAC1)

		testIP2 := net.ParseIP("192.168.1.20")
		testMAC2, _ := net.ParseMAC("02:00:00:00:00:02")
		addStaleNeighbor(testNSPath(), "br-pe-302", testIP2, testMAC2)

		refresher := &BridgeRefresher{
			bridgeName: "br-pe-302",
			namespace:  testNSPath(),
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		var neighbors []netlink.Neigh
		err = netnamespace.In(ns, func() error {
			var err error
			neighbors, err = refresher.listStaleNeighbors()
			return err
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(neighbors).To(HaveLen(2))
	})

	It("should skip non-STALE neighbors", func() {
		createTestBridge(testNSPath(), "br-pe-303")
		addIPToBridge(testNSPath(), "br-pe-303", "192.168.1.1/24")

		// Add REACHABLE neighbor (should be skipped)
		testIP := net.ParseIP("192.168.1.10")
		testMAC, _ := net.ParseMAC("02:00:00:00:00:01")
		addReachableNeighbor(testNSPath(), "br-pe-303", testIP, testMAC)

		refresher := &BridgeRefresher{
			bridgeName: "br-pe-303",
			namespace:  testNSPath(),
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		var neighbors []netlink.Neigh
		err = netnamespace.In(ns, func() error {
			var err error
			neighbors, err = refresher.listStaleNeighbors()
			return err
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(neighbors).To(BeEmpty())
	})

	It("should error for non-existent bridge", func() {
		refresher := &BridgeRefresher{
			bridgeName: "nonexistent-bridge",
			namespace:  testNSPath(),
		}

		ns, err := netns.GetFromPath(testNSPath())
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = ns.Close() }()

		err = netnamespace.In(ns, func() error {
			_, err := refresher.listStaleNeighbors()
			return err
		})
		Expect(err).To(HaveOccurred())
	})
})
