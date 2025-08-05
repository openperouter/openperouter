// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	loopbackTestNS = "loopbacktest"
)

var _ = Describe("Loopback configuration should work when", func() {
	var testNs netns.NsHandle

	AfterEach(func() {
		cleanLoopbackTest(loopbackTestNS)
	})

	BeforeEach(func() {
		cleanLoopbackTest(loopbackTestNS)
		testNs = createTestNS(loopbackTestNS)
	})

	It("should handle changing VTEP IP", func() {
		vtepIP1 := "192.168.1.1/32"
		vtepIP2 := "192.168.1.2/32"

		params1 := LoopbackParams{
			TargetNS: loopbackTestNS,
			VtepIP:   vtepIP1,
		}
		err := SetupLoopback(context.Background(), params1)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateLoopbackInNS(g, testNs, vtepIP1)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		params2 := LoopbackParams{
			TargetNS: loopbackTestNS,
			VtepIP:   vtepIP2,
		}
		err = SetupLoopback(context.Background(), params2)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateLoopbackInNS(g, testNs, vtepIP2)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
})

func validateLoopbackInNS(g Gomega, ns netns.NsHandle, vtepIP string) {
	_ = inNamespace(ns, func() error {
		validateLoopback(g, vtepIP)
		return nil
	})
}

func validateLoopback(g Gomega, vtepIP string) {
	links, err := netlink.LinkList()
	g.Expect(err).NotTo(HaveOccurred())

	loopbackFound := false
	for _, l := range links {
		if l.Attrs().Name == UnderlayLoopback {
			loopbackFound = true
			validateIP(g, l, vtepIP)
			break
		}
	}
	g.Expect(loopbackFound).To(BeTrue(), fmt.Sprintf("failed to find loopback interface %s in ns, links %v", UnderlayLoopback, links))
}

func cleanLoopbackTest(namespace string) {
	err := netns.DeleteNamed(namespace)
	if !errors.Is(err, os.ErrNotExist) {
		Expect(err).NotTo(HaveOccurred())
	}

	// Clean up any test loopback interfaces in the main namespace
	loopback, err := netlink.LinkByName(UnderlayLoopback)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	err = netlink.LinkDel(loopback)
	Expect(err).NotTo(HaveOccurred())
}

func validateIP(g Gomega, l netlink.Link, address string) {
	addresses, err := netlink.AddrList(l, netlink.FAMILY_ALL)
	g.Expect(err).NotTo(HaveOccurred())

	found := false
	for _, a := range addresses {
		if a.IPNet.String() == address {
			found = true
			break
		}
	}
	g.Expect(found).To(BeTrue(), fmt.Sprintf("failed to find address %s for %s: %v", address, l.Attrs().Name, addresses))
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
