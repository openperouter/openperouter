// SPDX-License-Identifier:Apache-2.0

//go:build runasroot
// +build runasroot

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
	DescribeTable("should enable sysctls",
		func(testNS string, sysctls []Sysctl, preEnable bool) {
			ns := createTestNS(testNS)
			defer cleanTest(testNS)

			if preEnable {
				for _, s := range sysctls {
					sysctlName := strings.ReplaceAll(s.Path, "/", ".")
					Expect(setSysctl(ns, sysctlName, "1")).To(Succeed())
				}
			}

			err := Ensure(fmt.Sprintf("/var/run/netns/%s", testNS), sysctls...)
			Expect(err).NotTo(HaveOccurred())

			for _, s := range sysctls {
				sysctlName := strings.ReplaceAll(s.Path, "/", ".")
				val, err := readSysctl(ns, sysctlName)
				Expect(err).NotTo(HaveOccurred())
				Expect(val).To(Equal("1"), "sysctl %s should be enabled", sysctlName)
			}
		},
		Entry("IPv6 forwarding when disabled", "test-ipv6-fwd", []Sysctl{IPv6Forwarding()}, false),
		Entry("IPv6 forwarding when already enabled", "test-ipv6-fwd-pre", []Sysctl{IPv6Forwarding()}, true),
		Entry("arp_accept all when disabled", "test-arp-all", []Sysctl{ArpAcceptAll()}, false),
		Entry("arp_accept all when already enabled", "test-arp-all-pre", []Sysctl{ArpAcceptAll()}, true),
		Entry("arp_accept default when disabled", "test-arp-def", []Sysctl{ArpAcceptDefault()}, false),
		Entry("arp_accept default when already enabled", "test-arp-def-pre", []Sysctl{ArpAcceptDefault()}, true),
	)
})
