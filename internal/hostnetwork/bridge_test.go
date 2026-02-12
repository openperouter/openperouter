// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"encoding/binary"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
)

var _ = Describe("ensureBridgeFixedMacAddress", func() {
	const (
		bridgeName = "testbrmac"
		vni        = 100
	)

	// copied the function from `ensureBridgeFixedMacAddress` implementation
	expectedMAC := func(vni int) net.HardwareAddr {
		GinkgoHelper()
		mac := make([]byte, macSize)
		buf := new(bytes.Buffer)
		Expect(binary.Write(buf, binary.BigEndian, int32(vni+1))).To(Succeed())
		copy(mac, macHeader)
		copy(mac[2:], buf.Bytes())
		return mac
	}

	BeforeEach(func() {
		Expect(
			netlink.LinkAdd(
				&netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}},
			),
		).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		link, err := netlink.LinkByName(bridgeName)
		if err == nil { // bridge's gone; nothing to do
			Expect(netlink.LinkDel(link)).To(Succeed())
		}
	})

	It("should set the MAC address on a bridge that has a different MAC", func() {
		bridge, err := netlink.LinkByName(bridgeName)
		Expect(err).NotTo(HaveOccurred())

		Expect(ensureBridgeFixedMacAddress(bridge, vni)).To(Succeed())

		bridge, err = netlink.LinkByName(bridgeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(bridge.Attrs().HardwareAddr).To(Equal(expectedMAC(vni)))
	})

	It("should be idempotent and not emit netlink events when MAC is already correct", func() {
		bridge, err := netlink.LinkByName(bridgeName)
		Expect(err).NotTo(HaveOccurred())

		By("setting the MAC for the first time")
		Expect(ensureBridgeFixedMacAddress(bridge, vni)).To(Succeed())

		By("re-fetching the bridge to pick up the updated MAC")
		bridge, err = netlink.LinkByName(bridgeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(bridge.Attrs().HardwareAddr).To(Equal(expectedMAC(vni)))

		By("subscribing to netlink link updates")
		updates := make(chan netlink.LinkUpdate, 16)
		done := make(chan struct{})
		defer close(done)
		Expect(netlink.LinkSubscribe(updates, done)).To(Succeed())

		By("calling ensureBridgeFixedMacAddress again with the same VNI")
		Expect(ensureBridgeFixedMacAddress(bridge, vni)).To(Succeed())

		By("verifying no netlink link update was emitted for the bridge")
		Consistently(func() bool {
			select {
			case u := <-updates:
				if u.Attrs().Name == bridgeName {
					return false // got an unexpected event for our bridge
				}
				return true // event for a different link, keep checking
			default:
				return true
			}
		}).WithTimeout(500*time.Millisecond).WithPolling(50*time.Millisecond).Should(BeTrue(),
			"ensureBridgeFixedMacAddress should not emit netlink events when MAC is already correct")

		By("verifying the MAC is still correct")
		bridge, err = netlink.LinkByName(bridgeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(bridge.Attrs().HardwareAddr).To(Equal(expectedMAC(vni)))
	})
})
