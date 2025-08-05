// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
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
			cleanLoopbackTest(testNS)
		})

		It("should enable IPv6 forwarding", func() {
			err := EnsureIPv6Forwarding(testNS)
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
			cleanLoopbackTest(testNS)
		})

		It("should not change the forwarding setting", func() {
			err := EnsureIPv6Forwarding(testNS)
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
