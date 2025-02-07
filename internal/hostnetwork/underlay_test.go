// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	externalInterfaceIP       = "192.170.0.9/24"
	underlayTestNS            = "underlaytest"
	underlayTestInterface     = "testundfirst"
	underlayTestInterfaceEdit = "testundsec"
	externalInterfaceEditIP   = "192.170.0.10/24"
)

var _ = Describe("Underlay configuration should work when", func() {
	var testNs netns.NsHandle

	AfterEach(func() {
		cleanTest(underlayTestNS)
	})

	BeforeEach(func() {
		cleanTest(underlayTestNS)

		toMove := &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name: underlayTestInterface,
			},
		}
		err := netlink.LinkAdd(toMove)
		Expect(err).NotTo(HaveOccurred())

		err = assignIPToInterface(toMove, externalInterfaceIP)
		Expect(err).NotTo(HaveOccurred())

		toEdit := &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name: underlayTestInterfaceEdit,
			},
		}
		err = netlink.LinkAdd(toEdit)
		Expect(err).NotTo(HaveOccurred())

		err = assignIPToInterface(toEdit, externalInterfaceEditIP)
		Expect(err).NotTo(HaveOccurred())

		testNs = createTestNS(underlayTestNS)
	})

	It("should work with a single underlay", func() {
		params := UnderlayParams{
			UnderlayInterface: underlayTestInterface,
			VtepIP:            "192.168.1.1/32",
			TargetNS:          underlayTestNS,
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlay(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("creating the same underlay twice should be idempotent", func() {
		params := UnderlayParams{
			UnderlayInterface: underlayTestInterface,
			VtepIP:            "192.168.1.1/32",
			TargetNS:          underlayTestNS,
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())
		err = SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlay(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("changing the underlay interface should error", func() {
		params := UnderlayParams{
			UnderlayInterface: underlayTestInterface,
			VtepIP:            "192.168.1.1/32",
			TargetNS:          underlayTestNS,
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			validateUnderlay(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		params.UnderlayInterface = underlayTestInterfaceEdit

		err = SetupUnderlay(context.Background(), params)
		u := UnderlayExistsError("")
		Expect(errors.As(err, &u)).To(BeTrue())
	})

	It("changing the underlay interface should error", func() {
		params := UnderlayParams{
			UnderlayInterface: underlayTestInterface,
			VtepIP:            "192.168.1.1/32",
			TargetNS:          underlayTestNS,
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlay(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		params.VtepIP = "192.168.1.2/32"

		err = SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlay(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
})

func validateUnderlay(g Gomega, ns netns.NsHandle, params UnderlayParams) {
	_ = inNamespace(ns, func() error {
		links, err := netlink.LinkList()
		g.Expect(err).NotTo(HaveOccurred())
		loopbackFound := false
		mainNicFound := false
		for _, l := range links {
			if l.Attrs().Name == UnderlayLoopback {
				loopbackFound = true
				validateIP(g, l, params.VtepIP)
			}
			if l.Attrs().Name == params.UnderlayInterface {
				mainNicFound = true
				validateIP(g, l, externalInterfaceIP)
				validateIP(g, l, underlayInterfaceSpecialAddr)
			}

		}
		g.Expect(loopbackFound).To(BeTrue(), fmt.Sprintf("failed to find loopback in ns, links %v", links))
		g.Expect(mainNicFound).To(BeTrue(), fmt.Sprintf("failed to find underlay interface in ns, links %v", links))
		return nil
	})

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

func cleanTest(namespace string) {
	err := netns.DeleteNamed(namespace)
	if !errors.Is(err, os.ErrNotExist) {
		Expect(err).NotTo(HaveOccurred())
	}
	links, err := netlink.LinkList()
	if err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	for _, l := range links {
		if strings.HasPrefix(l.Attrs().Name, "test") ||
			strings.HasPrefix(l.Attrs().Name, PEVethPrefix) ||
			strings.HasPrefix(l.Attrs().Name, HostVethPrefix) {
			err := netlink.LinkDel(l)
			Expect(err).NotTo(HaveOccurred())
		}
	}
	loopback, err := netlink.LinkByName(UnderlayLoopback)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	err = netlink.LinkDel(loopback)
	Expect(err).NotTo(HaveOccurred())
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
