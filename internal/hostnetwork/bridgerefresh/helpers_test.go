// SPDX-License-Identifier:Apache-2.0

//go:build runasroot
// +build runasroot

package bridgerefresh

import (
	"errors"
	"net"
	"os"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func createTestNS(testNs string) netns.NsHandle {
	GinkgoHelper()

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

func cleanTest(namespace string) {
	GinkgoHelper()

	err := netns.DeleteNamed(namespace)
	if !errors.Is(err, os.ErrNotExist) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func createTestBridge(nsPath, bridgeName string) {
	GinkgoHelper()

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

func addIPToBridge(nsPath, bridgeName, ipCIDR string) {
	GinkgoHelper()

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

func addStaleNeighbor(nsPath, bridgeName string, ip net.IP, mac net.HardwareAddr) {
	GinkgoHelper()

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

func addReachableNeighbor(nsPath, bridgeName string, ip net.IP, mac net.HardwareAddr) {
	GinkgoHelper()

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

func setBridgeMAC(nsPath, bridgeName string, mac net.HardwareAddr) {
	GinkgoHelper()

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
