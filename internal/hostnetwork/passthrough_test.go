// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const testPassthroughNSName = "passthroughtestns"

func testPassthroughNSPath() string {
	return fmt.Sprintf("/var/run/netns/%s", testPassthroughNSName)
}

var _ = Describe("Passthrough configuration", func() {
	var testNS netns.NsHandle

	BeforeEach(func() {
		cleanTest(testPassthroughNSName)
		testNS = createTestNS(testPassthroughNSName)
		setupLoopback(testNS)
	})
	AfterEach(func() {
		cleanTest(testPassthroughNSName)
	})

	It("should work with IPv4 only passthrough", func() {
		params := PassthroughParams{
			TargetNS: testPassthroughNSPath(),
			HostVeth: Veth{
				HostIPv4: "192.168.10.1/32",
				NSIPv4:   "192.168.10.0/32",
			},
		}

		err := SetupPassthrough(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validatePassthrough(g, params, testNS)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with IPv6 only passthrough", func() {
		params := PassthroughParams{
			TargetNS: testPassthroughNSPath(),
			HostVeth: Veth{
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		err := SetupPassthrough(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validatePassthrough(g, params, testNS)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work with dual-stack passthrough", func() {
		params := PassthroughParams{
			TargetNS: testPassthroughNSPath(),
			HostVeth: Veth{
				HostIPv4: "192.168.10.1/32",
				NSIPv4:   "192.168.10.0/32",
				HostIPv6: "2001:db8::1/128",
				NSIPv6:   "2001:db8::/128",
			},
		}

		err := SetupPassthrough(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validatePassthrough(g, params, testNS)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should remove passthrough interfaces correctly", func() {
		params := PassthroughParams{
			TargetNS: testPassthroughNSPath(),
			HostVeth: Veth{
				HostIPv4: "192.168.10.1/32",
				NSIPv4:   "192.168.10.0/32",
			},
		}

		err := SetupPassthrough(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validatePassthrough(g, params, testNS)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		err = RemovePassthrough(testPassthroughNSPath())
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validatePassthroughRemoved(g, testNS)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should be idempotent", func() {
		params := PassthroughParams{
			TargetNS: testPassthroughNSPath(),
			HostVeth: Veth{
				HostIPv4: "192.168.10.1/32",
				NSIPv4:   "192.168.10.0/32",
			},
		}

		err := SetupPassthrough(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		err = SetupPassthrough(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validatePassthrough(g, params, testNS)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should handle removal of non-existent passthrough gracefully", func() {
		err := RemovePassthrough(testPassthroughNSPath())
		Expect(err).NotTo(HaveOccurred())
	})
})

func validatePassthrough(g Gomega, params PassthroughParams, testNS netns.NsHandle) {
	vethHasIPs(g, PassthroughNames.HostSide, params.HostVeth.HostIPv4, params.HostVeth.HostIPv6)

	_ = netnamespace.In(testNS, func() error {
		validatePassthroughInNamespace(g, params)
		return nil
	})
}

func validatePassthroughInNamespace(g Gomega, params PassthroughParams) {
	vethHasIPs(g, PassthroughNames.NamespaceSide, params.HostVeth.NSIPv4, params.HostVeth.NSIPv6)
}

func vethHasIPs(g Gomega, linkName, ipv4, ipv6 string) {
	link, err := netlink.LinkByName(linkName)
	g.Expect(err).NotTo(HaveOccurred(), "passthrough link not found %q", linkName)

	g.Expect(link.Attrs().OperState).To(BeEquivalentTo(netlink.OperUp))

	if ipv4 != "" {
		hasIP, err := interfaceHasIP(link, ipv4)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "passthrough does not have IPv4", ipv4)
	}

	if ipv6 != "" {
		hasIP, err := interfaceHasIP(link, ipv6)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(hasIP).To(BeTrue(), "passthrough does not have IPv6", ipv6)
	}
}

func validatePassthroughRemoved(g Gomega, testNS netns.NsHandle) {
	_, err := netlink.LinkByName(PassthroughNames.HostSide)
	g.Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "host passthrough link %q should be deleted", PassthroughNames.HostSide)

	_ = netnamespace.In(testNS, func() error {
		validatePassthroughRemovedInNamespace(g)
		return nil
	})
}

func validatePassthroughRemovedInNamespace(g Gomega) {
	_, err := netlink.LinkByName(PassthroughNames.NamespaceSide)
	g.Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "namespace passthrough link %q should be deleted", PassthroughNames.NamespaceSide)
}
