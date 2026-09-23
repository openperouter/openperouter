// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"runtime"
	"slices"

	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
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

func underlayTestNSPath() string {
	return fmt.Sprintf("/var/run/netns/%s", underlayTestNS)
}

var _ = Describe("Underlay configuration should work when", func() {
	var testNs netns.NsHandle

	AfterEach(func() {
		cleanTest(underlayTestNS)
	})

	BeforeEach(func() {
		cleanTest(underlayTestNS)
		Expect(createInterface(underlayTestInterface, externalInterfaceIP)).To(Succeed())
		Expect(createInterface(underlayTestInterfaceEdit, externalInterfaceEditIP)).To(Succeed())
		testNs = createTestNS(underlayTestNS)
	})

	It("should work with a single underlay", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv4CIDR: "192.168.1.1/32",
			},
			TargetNS: underlayTestNSPath(),
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("creating the same underlay twice should be idempotent", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv4CIDR: "192.168.1.1/32",
			},
			TargetNS: underlayTestNSPath(),
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())
		err = SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("changing the underlay interface should restore old and move new", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv4CIDR: "192.168.1.1/32",
			},
			TargetNS: underlayTestNSPath(),
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		newParams := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterfaceEdit),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv4CIDR: "192.168.1.1/32",
			},
			TargetNS: underlayTestNSPath(),
		}
		err = SetupUnderlay(context.Background(), newParams)
		Expect(err).NotTo(HaveOccurred())

		By("verifying the new interface is in the target namespace")
		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, newParams)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("verifying the old interface was moved back to the default namespace")
		Eventually(func(g Gomega) {
			link, err := netlink.LinkByName(underlayTestInterface)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(link).NotTo(BeNil())
			g.Expect(link.Attrs().Flags & net.FlagUp).To(Equal(net.FlagUp))

			g.Expect(interfaceHasIP(link, externalInterfaceIP)).To(
				BeTrue(),
				"old interface should have its original IP after restore",
			)

			g.Expect(link.Attrs().Group).To(Equal(uint32(0)),
				"old interface should not have group ID after restore")
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("changing the vtepip should work", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv4CIDR: "192.168.1.1/32",
			},
			TargetNS: underlayTestNSPath(),
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		params.TunnelEndpoint.IPv4CIDR = "192.168.1.2/32"
		params.TunnelEndpoint.IPv6CIDR = "2001:db8:192:168:1::2/128"

		err = SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work without EVPN set", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TargetNS:           underlayTestNSPath(),
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work without EVPN set with IPv6 TunnelEndpoint", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TargetNS:           underlayTestNSPath(),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv6CIDR: "2001:db8:192:168::1/128",
			},
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should work without EVPN set with dual-stack TunnelEndpoint", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TargetNS:           underlayTestNSPath(),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv4CIDR: "192.168.1.1/32",
				IPv6CIDR: "2001:db8:192:168::1/128",
			},
		}
		err := SetupUnderlay(context.Background(), params)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
	It("RemoveUnderlay should move the underlay interfaces back to the default namespace", func() {
		underlayInterfaces := map[string]string{
			underlayTestInterface:     externalInterfaceIP,
			underlayTestInterfaceEdit: externalInterfaceEditIP,
		}
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(slices.Collect(maps.Keys(underlayInterfaces))...),
			TunnelEndpoint: &UnderlayTunnelEndpointParams{
				IPv4CIDR: "192.168.1.1/32",
			},
			TargetNS: underlayTestNSPath(),
		}
		Expect(SetupUnderlay(context.Background(), params)).To(Succeed())

		By("verifying the interfaces have the original IP while in the target namespace and have the correct group ID")
		Eventually(func(g Gomega) {
			validateUnderlayInNS(g, testNs, params)
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		ifacesToRemove := make([]UnderlayInterface, 0, len(underlayInterfaces))
		for name := range underlayInterfaces {
			ifacesToRemove = append(ifacesToRemove, UnderlayInterface{
				InterfaceName: name,
				Kind:          UnderlayInterfaceNetDev,
			})
		}
		Expect(RestoreUnderlay(context.Background(), underlayTestNSPath(), ifacesToRemove)).To(Succeed())

		By("verifying the loopback IPs were deleted from the target namespace")
		Eventually(func(g Gomega) {
			_ = netnamespace.In(testNs, func() error {
				checkInterfaceHasNoNonLoopbackIPs(g, loopbackName)
				return nil
			})
		}, 30*time.Second, 1*time.Second).Should(Succeed())

		By("verifying the interface was moved back, is up, still has the original IP and groupID was removed")
		Eventually(func(g Gomega) {
			for intf, ip := range underlayInterfaces {
				link, err := netlink.LinkByName(intf)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(link).NotTo(BeNil())
				g.Expect(link.Attrs().Flags&net.FlagUp).To(Equal(net.FlagUp), "interface should be administratively up")

				hasIP, err := interfaceHasIP(link, ip)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(hasIP).To(BeTrue(), "interface should have %s after moving back to the default namespace",
					ip)

				g.Expect(link.Attrs().Group).To(
					Equal(uint32(0)),
					"interface should not be part of a group after moving back to default namespace, found group: %d",
					link.Attrs().Group,
				)
			}
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})
})

var _ = Describe("UnderlayInterfacesToRemove", func() {
	DescribeTable("should return interfaces to remove",
		func(existing, requested []string, expected []UnderlayInterface) {
			Expect(UnderlayInterfacesToRemove(netdevInterfaces(existing...), netdevInterfaces(requested...))).To(Equal(expected))
		},
		Entry("empty existing returns empty", []string{}, []string{"nic1"}, netdevInterfaces()),
		Entry("nil existing returns empty", nil, []string{"nic1"}, netdevInterfaces()),
		Entry("same single interface returns empty", []string{"nic1"}, []string{"nic1"}, netdevInterfaces()),
		Entry("same multiple interfaces returns empty", []string{"nic1", "nic2"}, []string{"nic1", "nic2"}, netdevInterfaces()),
		Entry("adding interface returns empty", []string{"nic1"}, []string{"nic1", "nic2"}, netdevInterfaces()),
		Entry("removing interface returns removed", []string{"nic1", "nic2"}, []string{"nic1"}, netdevInterfaces("nic2")),
		Entry("replacing interface returns old", []string{"nic1"}, []string{"nic2"}, netdevInterfaces("nic1")),
		Entry("completely different set returns all old", []string{"nic1", "nic2"}, []string{"nic3", "nic4"}, netdevInterfaces("nic1", "nic2")),
	)

	It("returns an interface whose kind changed, preserving the old kind", func() {
		existing := []UnderlayInterface{
			{InterfaceName: "nic1", Kind: UnderlayInterfaceNetDev},
			{InterfaceName: "nic2", Kind: UnderlayInterfaceCNIDev},
		}
		requested := []UnderlayInterface{
			{InterfaceName: "nic1", Kind: UnderlayInterfaceCNIDev},
			{InterfaceName: "nic2", Kind: UnderlayInterfaceCNIDev},
		}
		Expect(UnderlayInterfacesToRemove(existing, requested)).To(Equal([]UnderlayInterface{
			{InterfaceName: "nic1", Kind: UnderlayInterfaceNetDev},
		}))
	})
})

var _ = Describe("UnderlayInterfaces", func() {
	AfterEach(func() {
		cleanTest(underlayTestNS)
	})

	BeforeEach(func() {
		cleanTest(underlayTestNS)
		Expect(createInterface(underlayTestInterface, externalInterfaceIP)).To(Succeed())
		createTestNS(underlayTestNS)
	})

	It("should return empty when no underlay interfaces exist", func() {
		ifaces, err := UnderlayInterfaces(underlayTestNSPath())
		Expect(err).NotTo(HaveOccurred())
		Expect(ifaces).To(BeEmpty())
	})

	It("should return interface names after setup", func() {
		params := UnderlayParams{
			UnderlayInterfaces: netdevInterfaces(underlayTestInterface),
			TargetNS:           underlayTestNSPath(),
		}
		Expect(SetupUnderlay(context.Background(), params)).To(Succeed())

		Eventually(func(g Gomega) {
			ifaces, err := UnderlayInterfaces(underlayTestNSPath())
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ifaces).To(Equal([]UnderlayInterface{
				{InterfaceName: underlayTestInterface, Kind: UnderlayInterfaceNetDev},
			}))
		}, 30*time.Second, 1*time.Second).Should(Succeed())
	})

	It("should error for non-existent namespace", func() {
		_, err := UnderlayInterfaces("/var/run/netns/doesnotexist")
		Expect(err).To(HaveOccurred())
	})
})

// netdevInterfaces builds UnderlayInterface entries of the netdev kind for
// the given names.
func netdevInterfaces(names ...string) []UnderlayInterface {
	res := make([]UnderlayInterface, 0, len(names))
	for _, name := range names {
		res = append(res, UnderlayInterface{
			InterfaceName: name,
			Kind:          UnderlayInterfaceNetDev,
		})
	}
	return res
}

func validateUnderlayInNS(g Gomega, ns netns.NsHandle, params UnderlayParams) {
	_ = netnamespace.In(ns, func() error {
		validateUnderlay(
			g,
			params,
			map[string]string{
				underlayTestInterface:     externalInterfaceIP,
				underlayTestInterfaceEdit: externalInterfaceEditIP,
			},
		)
		return nil
	})
}

// validateUnderlay checks that everything inside the underlay was configured as expected.
// If the caller does not care about interface IP address validation, they can set interfaceIPs to empty and these
// checks will be skipped.
func validateUnderlay(g Gomega, params UnderlayParams, interfaceIPs map[string]string) {
	links, err := netlink.LinkList()
	g.Expect(err).NotTo(HaveOccurred())
	foundInterfaces := map[string]bool{}
	for _, l := range links {
		if l.Attrs().Name == loopbackName {
			validateLoopback(g, l, params)
		}
		for _, underlayIface := range params.UnderlayInterfaces {
			if l.Attrs().Name == underlayIface.InterfaceName {
				foundInterfaces[underlayIface.InterfaceName] = true
				validateGroupID(g, l, UnderlayGroupID)

				if len(interfaceIPs) == 0 {
					continue
				}
				ip := interfaceIPs[underlayIface.InterfaceName]
				g.Expect(ip).NotTo(BeEmpty())
				validateIP(g, l, ip)
			}
		}
	}
	for _, underlayIface := range params.UnderlayInterfaces {
		g.Expect(foundInterfaces).To(HaveKey(underlayIface.InterfaceName),
			fmt.Sprintf("underlay interface %s not found in ns, links %v", underlayIface.InterfaceName, links))
	}
}

func validateLoopback(g Gomega, l netlink.Link, params UnderlayParams) {
	hasIP := false
	if params.TunnelEndpoint != nil && params.TunnelEndpoint.IPv4CIDR != "" {
		hasIP = true
		validateIP(g, l, params.TunnelEndpoint.IPv4CIDR)
	}
	if params.TunnelEndpoint != nil && params.TunnelEndpoint.IPv6CIDR != "" {
		hasIP = true
		validateIP(g, l, params.TunnelEndpoint.IPv6CIDR)
	}
	if hasIP {
		return
	}
	checkInterfaceHasNoNonLoopbackIPs(g, loopbackName)
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

func validateGroupID(g Gomega, l netlink.Link, expectedGroupID uint32) {
	actualGroupID := l.Attrs().Group
	g.Expect(actualGroupID).To(Equal(expectedGroupID), fmt.Sprintf("interface %s has wrong group ID: expected %d, got %d", l.Attrs().Name, expectedGroupID, actualGroupID))
}

func cleanTest(namespace string) {
	err := netns.DeleteNamed(namespace)
	if !errors.Is(err, os.ErrNotExist) {
		Expect(err).NotTo(HaveOccurred())
	}

	// Clean up OVS bridges BEFORE deleting veths
	// This ensures OVS can properly detach ports that reference the veth devices
	cleanupOVSBridges()

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

	err = RemoveLinkByName(PassthroughNames.HostSide)
	Expect(err).NotTo(HaveOccurred())

	curNS, err := netns.Get()
	defer func() {
		if err := curNS.Close(); err != nil {
			GinkgoWriter.Printf("couldn't close curNS, err: %v", err)
		}
	}()
	Expect(err).NotTo(HaveOccurred())

	handle, err := netlink.NewHandleAt(curNS)
	Expect(err).NotTo(HaveOccurred())
	defer handle.Close()

	err = clearNonDefaultLoopbackIPs(handle, loopbackName)
	Expect(err).NotTo(HaveOccurred())
}

func createInterface(intf, ip string) error {
	toMove := &netlink.Dummy{
		LinkAttrs: netlink.LinkAttrs{
			Name: intf,
		},
	}
	if err := netlink.LinkAdd(toMove); err != nil {
		return err
	}

	return AssignIPToInterface(toMove, ip)
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
