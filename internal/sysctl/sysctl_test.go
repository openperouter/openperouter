// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package sysctl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netns"
)

func readSysctl(ns netns.NsHandle, name string) (string, error) {
	var output string
	err := netnamespace.In(ns, func() error {
		out, err := exec.Command("sysctl", "-n", name).CombinedOutput()
		output = strings.TrimSpace(string(out))
		return err
	})
	return output, err
}

func setSysctl(ns netns.NsHandle, name, value string) error {
	return netnamespace.In(ns, func() error {
		_, err := exec.Command("sysctl", "-w", fmt.Sprintf("%s=%s", name, value)).CombinedOutput()
		return err
	})
}

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

func cleanTest(namespace string) {
	err := netns.DeleteNamed(namespace)
	if !errors.Is(err, os.ErrNotExist) {
		Expect(err).NotTo(HaveOccurred())
	}
}

var _ = Describe("Ensure", func() {
	DescribeTable("should set sysctls",
		func(testNS string, sysctls []Sysctl, preEnable, expected string) {
			ns := createTestNS(testNS)
			defer cleanTest(testNS)

			for _, s := range sysctls {
				sysctlName := strings.ReplaceAll(s.Path, "/", ".")
				Expect(setSysctl(ns, sysctlName, preEnable)).To(Succeed())
			}

			Expect(EnsureInNamespace(fmt.Sprintf("/var/run/netns/%s", testNS), sysctls...)).To(Succeed())

			for _, s := range sysctls {
				sysctlName := strings.ReplaceAll(s.Path, "/", ".")
				val, err := readSysctl(ns, sysctlName)
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal(expected), "sysctl %q should be %q but got %q", sysctlName, expected, val)
			}
		},
		Entry("IPv4 forwarding when disabled", "test-ipv4-fwd", []Sysctl{IPv4Forwarding()}, "0", "1"),
		Entry("IPv4 forwarding when already enabled", "test-ipv4-fwd-pre", []Sysctl{IPv4Forwarding()}, "1", "1"),
		Entry("IPv6 forwarding when disabled", "test-ipv6-fwd", []Sysctl{IPv6Forwarding()}, "0", "1"),
		Entry("IPv6 forwarding when already enabled", "test-ipv6-fwd-pre", []Sysctl{IPv6Forwarding()}, "1", "1"),
		Entry("arp_accept all when disabled", "test-arp-all", []Sysctl{ArpAcceptAll()}, "0", "1"),
		Entry("arp_accept all when already enabled", "test-arp-all-pre", []Sysctl{ArpAcceptAll()}, "1", "1"),
		Entry("arp_accept default when disabled", "test-arp-def", []Sysctl{ArpAcceptDefault()}, "0", "1"),
		Entry("arp_accept default when already enabled", "test-arp-def-pre", []Sysctl{ArpAcceptDefault()}, "1", "1"),
		Entry("rp default filter when disabled", "test-rpfilter-def", []Sysctl{DisableRPFilter("default")}, "0", "0"),
		Entry("rp default filter when enabled", "test-rpfilter-def-pre", []Sysctl{DisableRPFilter("default")}, "1", "0"),
	)

	It("should skip optional sysctl when proc file does not exist", func() {
		testNS := "test-optional-skip"
		_ = createTestNS(testNS)
		defer cleanTest(testNS)

		s := Sysctl{
			Path:               "net/ipv6/conf/all/nonexistent_sysctl",
			Description:        "nonexistent sysctl with UnsupportedWarning",
			UnsupportedWarning: "expected to be skipped",
		}
		err := EnsureInNamespace(fmt.Sprintf("/var/run/netns/%s", testNS), s)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail for non-optional sysctl when proc file does not exist", func() {
		testNS := "test-nonoptional-fail"
		_ = createTestNS(testNS)
		defer cleanTest(testNS)

		s := Sysctl{
			Path:        "net/ipv6/conf/all/nonexistent_sysctl",
			Description: "nonexistent sysctl without UnsupportedWarning",
		}
		err := EnsureInNamespace(fmt.Sprintf("/var/run/netns/%s", testNS), s)
		Expect(err).To(HaveOccurred())
	})
})
