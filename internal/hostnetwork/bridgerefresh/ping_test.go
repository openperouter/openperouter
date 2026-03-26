// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package bridgerefresh

import (
	"fmt"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netns"
)

var _ = Describe("sendPing", func() {
	const testNSName = "pingtest"

	testNSPath := func() string {
		return fmt.Sprintf("/var/run/netns/%s", testNSName)
	}

	BeforeEach(func() {
		createTestNS(testNSName)
	})

	AfterEach(func() {
		cleanTest(testNSName)
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
			return refresher.sendPing(net.ParseIP("192.168.1.10"))
		})
		Expect(err).To(HaveOccurred())
	})
})
