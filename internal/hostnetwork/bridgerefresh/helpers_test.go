// SPDX-License-Identifier:Apache-2.0

//go:build runasroot
// +build runasroot

package bridgerefresh

import (
	"errors"
	"net"
	"os"
	"runtime"

	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// createTestNS creates an isolated network namespace for testing.
func createTestNS(testNs string) netns.NsHandle {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	currentNs, err := netns.Get()
	Expect(err).NotTo(HaveOccurred())

	newNs, err := netns.NewNamed(testNs)
	Expect(err).NotTo(HaveOccurred())

	err = netns.Set(currentNs)
	Expect(err).NotTo(HaveOccurred())
	return newNs
}

// cleanTest deletes the namespace and any test links.
func cleanTest(namespace string) {
	err := netns.DeleteNamed(namespace)
	if !errors.Is(err, os.ErrNotExist) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// createTestBridge creates a bridge in the specified namespace.
func createTestBridge(nsPath, bridgeName string) {
	ns, err := netns.GetFromPath(nsPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = ns.Close() }()

	err = netnamespace.In(ns, func() error {
		bridge := &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{Name: bridgeName},
		}
		if err := netlink.LinkAdd(bridge); err != nil {
			return err
		}
		return netlink.LinkSetUp(bridge)
	})
	Expect(err).NotTo(HaveOccurred())
}

// addIPToBridge assigns an IP address to a bridge in the namespace.
func addIPToBridge(nsPath, bridgeName, ipCIDR string) {
	ns, err := netns.GetFromPath(nsPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = ns.Close() }()

	err = netnamespace.In(ns, func() error {
		bridge, err := netlink.LinkByName(bridgeName)
		if err != nil {
			return err
		}

		addr, err := netlink.ParseAddr(ipCIDR)
		if err != nil {
			return err
		}
		return netlink.AddrAdd(bridge, addr)
	})
	Expect(err).NotTo(HaveOccurred())
}

// addStaleNeighbor adds a STALE neighbor entry to the bridge.
func addStaleNeighbor(nsPath, bridgeName string, ip net.IP, mac net.HardwareAddr) {
	ns, err := netns.GetFromPath(nsPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = ns.Close() }()

	err = netnamespace.In(ns, func() error {
		bridge, err := netlink.LinkByName(bridgeName)
		if err != nil {
			return err
		}

		neigh := &netlink.Neigh{
			LinkIndex:    bridge.Attrs().Index,
			State:        netlink.NUD_STALE,
			IP:           ip,
			HardwareAddr: mac,
		}
		return netlink.NeighAdd(neigh)
	})
	Expect(err).NotTo(HaveOccurred())
}

// addReachableNeighbor adds a REACHABLE neighbor entry to the bridge.
func addReachableNeighbor(nsPath, bridgeName string, ip net.IP, mac net.HardwareAddr) {
	ns, err := netns.GetFromPath(nsPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = ns.Close() }()

	err = netnamespace.In(ns, func() error {
		bridge, err := netlink.LinkByName(bridgeName)
		if err != nil {
			return err
		}

		neigh := &netlink.Neigh{
			LinkIndex:    bridge.Attrs().Index,
			State:        netlink.NUD_REACHABLE,
			IP:           ip,
			HardwareAddr: mac,
		}
		return netlink.NeighAdd(neigh)
	})
	Expect(err).NotTo(HaveOccurred())
}

// setBridgeMAC sets the hardware address of a bridge in the namespace.
func setBridgeMAC(nsPath, bridgeName string, mac net.HardwareAddr) {
	ns, err := netns.GetFromPath(nsPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = ns.Close() }()

	err = netnamespace.In(ns, func() error {
		bridge, err := netlink.LinkByName(bridgeName)
		if err != nil {
			return err
		}
		return netlink.LinkSetHardwareAddr(bridge, mac)
	})
	Expect(err).NotTo(HaveOccurred())
}

// getNeighborsInNS returns all neighbors on the bridge in the namespace.
func getNeighborsInNS(nsPath, bridgeName string) []netlink.Neigh {
	var neighbors []netlink.Neigh

	ns, err := netns.GetFromPath(nsPath)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = ns.Close() }()

	err = netnamespace.In(ns, func() error {
		bridge, err := netlink.LinkByName(bridgeName)
		if err != nil {
			return err
		}

		neighbors, err = netlink.NeighList(bridge.Attrs().Index, netlink.FAMILY_ALL)
		return err
	})
	Expect(err).NotTo(HaveOccurred())
	return neighbors
}
