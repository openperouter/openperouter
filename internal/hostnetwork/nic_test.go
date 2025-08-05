// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	externalInterfaceIP     = "192.170.0.9/24"
	nicTestNS               = "nictest"
	nicTestInterface        = "testnicfirst"
	nicTestInterfaceEdit    = "testnicsec"
	externalInterfaceEditIP = "192.170.0.10/24"
)

var _ = Describe("NIC configuration should work when", func() {
	var testNs netns.NsHandle

	AfterEach(func() {
		cleanNICTest(nicTestNS)
	})

	BeforeEach(func() {
		cleanNICTest(nicTestNS)

		toMove := &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name: nicTestInterface,
			},
		}
		err := netlink.LinkAdd(toMove)
		Expect(err).NotTo(HaveOccurred())

		err = assignIPToInterface(toMove, externalInterfaceIP)
		Expect(err).NotTo(HaveOccurred())

		toEdit := &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name: nicTestInterfaceEdit,
			},
		}
		err = netlink.LinkAdd(toEdit)
		Expect(err).NotTo(HaveOccurred())

		err = assignIPToInterface(toEdit, externalInterfaceEditIP)
		Expect(err).NotTo(HaveOccurred())

		testNs = createTestNS(nicTestNS)
	})

	It("should error when trying to change underlay interface", func() {
		params1 := NICParams{
			UnderlayInterface: nicTestInterface,
			TargetNS:          nicTestNS,
		}
		_, err := SetupNIC(context.Background(), params1)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateNICInNS(g, testNs, nicTestInterface, externalInterfaceIP)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		params2 := NICParams{
			UnderlayInterface: nicTestInterfaceEdit,
			TargetNS:          nicTestNS,
		}
		_, err = SetupNIC(context.Background(), params2)
		u := UnderlayExistsError("")
		Expect(errors.As(err, &u)).To(BeTrue())
	})

})

func validateNICInNS(g Gomega, ns netns.NsHandle, interfaceName string, expectedIPs ...string) {
	_ = inNamespace(ns, func() error {
		validateNIC(g, interfaceName, expectedIPs...)
		return nil
	})
}

func validateNIC(g Gomega, interfaceName string, expectedIPs ...string) {
	links, err := netlink.LinkList()
	g.Expect(err).NotTo(HaveOccurred())

	nicFound := false
	for _, l := range links {
		if l.Attrs().Name == interfaceName {
			nicFound = true
			// Validate all expected IPs are present
			for _, ip := range expectedIPs {
				validateIP(g, l, ip)
			}
			// Validate special underlay address is present
			validateIP(g, l, underlayInterfaceSpecialAddr)
			break
		}
	}
	g.Expect(nicFound).To(BeTrue(), fmt.Sprintf("failed to find NIC interface %s in ns, links %v", interfaceName, links))
}

func cleanNICTest(namespace string) {
	err := netns.DeleteNamed(namespace)
	if !errors.Is(err, os.ErrNotExist) {
		Expect(err).NotTo(HaveOccurred())
	}

	// Clean up test interfaces in main namespace
	links, err := netlink.LinkList()
	if err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	for _, l := range links {
		if strings.HasPrefix(l.Attrs().Name, "testnic") ||
			strings.HasPrefix(l.Attrs().Name, PEVethPrefix) ||
			strings.HasPrefix(l.Attrs().Name, HostVethPrefix) {
			err := netlink.LinkDel(l)
			Expect(err).NotTo(HaveOccurred())
		}
	}
}
