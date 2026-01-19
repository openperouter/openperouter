// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netns"
)

var _ = Describe("EnsureIPv6Forwarding", func() {
	Context("when IPv6 forwarding is disabled", func() {
		testNS := "test-ipv6-forwarding"
		var ns netns.NsHandle

		BeforeEach(func() {
			ns = createTestNS(testNS)
		})

		AfterEach(func() {
			cleanTest(testNS)
		})

		It("should enable IPv6 forwarding", func() {
			err := EnsureIPv6Forwarding(fmt.Sprintf("/var/run/netns/%s", testNS))
			Expect(err).NotTo(HaveOccurred())

			var output string
			err = inNamespace(ns, func() error {
				out, err := exec.Command("sysctl", "-n", "net.ipv6.conf.all.forwarding").CombinedOutput()
				output = string(out)
				return err
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(strings.TrimSpace(output)).To(Equal("1"))
		})
	})

	Context("when IPv6 forwarding is already enabled", func() {
		testNS := "test-ipv6-forwarding-already"
		var ns netns.NsHandle

		BeforeEach(func() {
			ns = createTestNS(testNS)
			err := inNamespace(ns, func() error {
				_, setErr := exec.Command("sysctl", "-w", "net.ipv6.conf.all.forwarding=1").CombinedOutput()
				return setErr
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cleanTest(testNS)
		})

		It("should not change the forwarding setting", func() {
			err := EnsureIPv6Forwarding(fmt.Sprintf("/var/run/netns/%s", testNS))
			Expect(err).NotTo(HaveOccurred())

			var output string
			err = inNamespace(ns, func() error {
				out, err := exec.Command("sysctl", "-n", "net.ipv6.conf.all.forwarding").CombinedOutput()
				output = string(out)
				return err
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(strings.TrimSpace(output)).To(Equal("1"))
		})
	})
})

var _ = Describe("EnsureArpAccept", func() {
	Context("when arp_accept is disabled", func() {
		testNS := "test-arp-accept"
		var ns netns.NsHandle

		BeforeEach(func() {
			ns = createTestNS(testNS)
		})

		AfterEach(func() {
			cleanTest(testNS)
		})

		It("should enable arp_accept on all and default", func() {
			err := EnsureArpAccept(fmt.Sprintf("/var/run/netns/%s", testNS))
			Expect(err).NotTo(HaveOccurred())

			var allOutput, defaultOutput string
			err = inNamespace(ns, func() error {
				out, err := exec.Command("sysctl", "-n", "net.ipv4.conf.all.arp_accept").CombinedOutput()
				if err != nil {
					return err
				}
				allOutput = string(out)

				out, err = exec.Command("sysctl", "-n", "net.ipv4.conf.default.arp_accept").CombinedOutput()
				if err != nil {
					return err
				}
				defaultOutput = string(out)
				return nil
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(strings.TrimSpace(allOutput)).To(Equal("1"))
			Expect(strings.TrimSpace(defaultOutput)).To(Equal("1"))
		})
	})

	Context("when arp_accept is already enabled", func() {
		testNS := "test-arp-accept-already"
		var ns netns.NsHandle

		BeforeEach(func() {
			ns = createTestNS(testNS)
			err := inNamespace(ns, func() error {
				_, setErr := exec.Command("sysctl", "-w", "net.ipv4.conf.all.arp_accept=1").CombinedOutput()
				if setErr != nil {
					return setErr
				}
				_, setErr = exec.Command("sysctl", "-w", "net.ipv4.conf.default.arp_accept=1").CombinedOutput()
				return setErr
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cleanTest(testNS)
		})

		It("should not change the arp_accept setting", func() {
			err := EnsureArpAccept(fmt.Sprintf("/var/run/netns/%s", testNS))
			Expect(err).NotTo(HaveOccurred())

			var allOutput, defaultOutput string
			err = inNamespace(ns, func() error {
				out, err := exec.Command("sysctl", "-n", "net.ipv4.conf.all.arp_accept").CombinedOutput()
				if err != nil {
					return err
				}
				allOutput = string(out)

				out, err = exec.Command("sysctl", "-n", "net.ipv4.conf.default.arp_accept").CombinedOutput()
				if err != nil {
					return err
				}
				defaultOutput = string(out)
				return nil
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(strings.TrimSpace(allOutput)).To(Equal("1"))
			Expect(strings.TrimSpace(defaultOutput)).To(Equal("1"))
		})
	})
})
