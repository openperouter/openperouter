// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/cniinvoker"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	underlayCNITestNS        = "underlaycnitest"
	underlayCNITestPlugin    = "fakeunderlay"
	underlayCNITestAddress   = "192.168.11.5/24"
	underlayCNITestInterface = "testcniphys"
	underlayCNITestPhysIP    = "192.170.0.11/24"
)

func underlayCNITestNSPath() string {
	return fmt.Sprintf("/var/run/netns/%s", underlayCNITestNS)
}

// underlayCNITestConfig is a CNI conflist backed by a fake plugin executable
// (see fakeCNIPlugin) that creates a dummy link inside the target netns.
var underlayCNITestConfig = fmt.Sprintf(`{
  "cniVersion": "1.0.0",
  "name": "underlay-cni-test",
  "plugins": [
    {
      "type": %q
    }
  ]
}`, underlayCNITestPlugin)

// fakeCNIPlugin creates a dummy link with a fixed address inside CNI_NETNS on
// ADD and deletes it on DEL, recording every invocation in CNI_TEST_LOG. It
// lets the tests exercise the full CNI provisioning path without shipping
// real plugins.
const fakeCNIPlugin = `#!/bin/sh
set -e
echo "$CNI_COMMAND $CNI_IFNAME" >> "$CNI_TEST_LOG"
case "$CNI_COMMAND" in
ADD)
  nsenter --net="$CNI_NETNS" ip link add "$CNI_IFNAME" type dummy
  nsenter --net="$CNI_NETNS" ip addr add "` + underlayCNITestAddress + `" dev "$CNI_IFNAME"
  nsenter --net="$CNI_NETNS" ip link set "$CNI_IFNAME" up
  echo "{\"cniVersion\":\"1.0.0\",\"interfaces\":[{\"name\":\"$CNI_IFNAME\",\"sandbox\":\"$CNI_NETNS\"}],\"ips\":[{\"address\":\"` + underlayCNITestAddress + `\",\"interface\":0}]}"
  ;;
DEL)
  nsenter --net="$CNI_NETNS" ip link del "$CNI_IFNAME" 2>/dev/null || true
  ;;
esac
`

var _ = Describe("Underlay CNI configuration", func() {
	var testNs netns.NsHandle
	var testDir string
	var logPath string

	cniParams := func(ifNames ...string) UnderlayParams {
		interfaces := make([]UnderlayInterface, 0, len(ifNames))
		for _, ifName := range ifNames {
			interfaces = append(interfaces, UnderlayInterface{
				InterfaceName: ifName,
				Kind:          UnderlayInterfaceCNIDev,
				CNI: &CNIDeviceParams{
					Config: []byte(underlayCNITestConfig),
				},
			})
		}
		return UnderlayParams{
			TargetNS:           underlayCNITestNSPath(),
			UnderlayInterfaces: interfaces,
		}
	}

	loggedCommands := func() []string {
		content, err := os.ReadFile(logPath)
		if os.IsNotExist(err) {
			return nil
		}
		Expect(err).NotTo(HaveOccurred())
		return strings.Split(strings.TrimSpace(string(content)), "\n")
	}

	BeforeEach(func() {
		cleanTest(underlayCNITestNS)
		testNs = createTestNS(underlayCNITestNS)

		var err error
		testDir, err = os.MkdirTemp("", "underlaycni")
		Expect(err).NotTo(HaveOccurred())
		binDir := filepath.Join(testDir, "bin")
		Expect(os.Mkdir(binDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(binDir, underlayCNITestPlugin), []byte(fakeCNIPlugin), 0o755)).To(Succeed())
		logPath = filepath.Join(testDir, "invocations.log")
		Expect(os.Setenv("CNI_TEST_LOG", logPath)).To(Succeed())

		cniinvoker.Init([]string{binDir}, filepath.Join(testDir, "cache"), "testnode", nil)
	})

	AfterEach(func() {
		cniinvoker.Invoker = nil
		cleanTest(underlayCNITestNS)
		Expect(os.Unsetenv("CNI_TEST_LOG")).To(Succeed())
		Expect(os.RemoveAll(testDir)).To(Succeed())
	})

	It("computes the underlay MTU from both netdev and cni interfaces", func() {
		setupFakeUnderlay(testNs, "testnetdevmtu", 9000)
		// The fake CNI plugin provisions the interface with the default
		// 1500 MTU; it is discovered through the CNI cache since it does
		// not carry the underlay group ID.
		Expect(SetupUnderlay(context.Background(), cniParams("net1"))).To(Succeed())

		mtu, err := findUnderlayMTU(testNs)
		Expect(err).NotTo(HaveOccurred())
		Expect(mtu).To(Equal(1500), "the lowest MTU among the underlay interfaces should win")
	})

	It("returns the cni interfaces with their kind from UnderlayInterfaces", func() {
		Expect(SetupUnderlay(context.Background(), cniParams("net1"))).To(Succeed())
		setupFakeUnderlay(testNs, "testnetdev", 1500)

		ifaces, err := UnderlayInterfaces(underlayCNITestNSPath())
		Expect(err).NotTo(HaveOccurred())
		Expect(ifaces).To(ConsistOf(
			UnderlayInterface{InterfaceName: "net1", Kind: UnderlayInterfaceCNIDev},
			UnderlayInterface{InterfaceName: "testnetdev", Kind: UnderlayInterfaceNetDev},
		))
	})

	It("provisions a cni interface recorded in the cni cache", func() {
		params := cniParams("net1")
		Expect(SetupUnderlay(context.Background(), params)).To(Succeed())

		validateCNIInterfaceInNS(testNs, "net1")
		cached, err := cniinvoker.Invoker.CachedIfNames()
		Expect(err).NotTo(HaveOccurred())
		Expect(cached).To(ConsistOf("net1"),
			"the cni cache should be the source of truth for the provisioned interfaces")
	})

	It("is idempotent through the cni cache", func() {
		params := cniParams("net1")
		Expect(SetupUnderlay(context.Background(), params)).To(Succeed())
		Expect(SetupUnderlay(context.Background(), params)).To(Succeed())

		Expect(loggedCommands()).To(ConsistOf("ADD net1"), "the second setup should be served from the cache")
		validateCNIInterfaceInNS(testNs, "net1")
	})

	It("deletes the stale cni interface when it is renamed in the spec", func() {
		Expect(SetupUnderlay(context.Background(), cniParams("net1"))).To(Succeed())

		Expect(SetupUnderlay(context.Background(), cniParams("underlay0"))).To(Succeed())

		Expect(loggedCommands()).To(ConsistOf("ADD net1", "DEL net1", "ADD underlay0"))
		validateCNIInterfaceInNS(testNs, "underlay0")
		err := netnamespace.In(testNs, func() error {
			_, err := netlink.LinkByName("net1")
			Expect(err).To(MatchError(ContainSubstring("Link not found")), "the cni interface should be gone")
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("deletes the cni interfaces when they are removed from the spec", func() {
		Expect(SetupUnderlay(context.Background(), cniParams("net1"))).To(Succeed())

		params := cniParams()
		params.UnderlayInterfaces = append(params.UnderlayInterfaces, netdevInterfaces(underlayCNITestInterface)...)
		Expect(createInterface(underlayCNITestInterface, underlayCNITestPhysIP)).To(Succeed())
		Expect(SetupUnderlay(context.Background(), params)).To(Succeed())

		Expect(loggedCommands()).To(ConsistOf("ADD net1", "DEL net1"))
		err := netnamespace.In(testNs, func() error {
			_, err := netlink.LinkByName("net1")
			Expect(err).To(MatchError(ContainSubstring("Link not found")), "the cni interface should be gone")
			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("removes the cni interfaces and their cache on RestoreUnderlay", func() {
		Expect(SetupUnderlay(context.Background(), cniParams("net1"))).To(Succeed())

		removeAllUnderlayInterfaces := func() {
			existing, err := UnderlayInterfaces(underlayCNITestNSPath())
			Expect(err).NotTo(HaveOccurred())
			Expect(RestoreUnderlay(context.Background(), underlayCNITestNSPath(),
				existing)).To(Succeed())
		}

		removeAllUnderlayInterfaces()
		Expect(loggedCommands()).To(ConsistOf("ADD net1", "DEL net1"))
		err := netnamespace.In(testNs, func() error {
			_, err := netlink.LinkByName("net1")
			Expect(err).To(MatchError(ContainSubstring("Link not found")), "the cni interface should be gone")
			return nil
		})
		Expect(err).NotTo(HaveOccurred())

		// The cache is empty: removing again invokes nothing.
		removeAllUnderlayInterfaces()
		Expect(loggedCommands()).To(ConsistOf("ADD net1", "DEL net1"))
	})

	It("provisions again after the namespace is rebuilt and the cache cleared", func() {
		Expect(SetupUnderlay(context.Background(), cniParams("net1"))).To(Succeed())

		// Simulate the netns rebuild flow: CNI DEL, then netns rebuild,
		// then a reconcile with a different interface name.
		existing, err := UnderlayInterfaces(underlayCNITestNSPath())
		Expect(err).NotTo(HaveOccurred())
		Expect(RestoreUnderlay(context.Background(), underlayCNITestNSPath(),
			existing)).To(Succeed())
		cleanTest(underlayCNITestNS)
		testNs = createTestNS(underlayCNITestNS)

		Expect(SetupUnderlay(context.Background(), cniParams("net1"))).To(Succeed())
		Expect(loggedCommands()).To(ConsistOf("ADD net1", "DEL net1", "ADD net1"))
		validateCNIInterfaceInNS(testNs, "net1")
	})
})

// validateCNIInterfaceInNS checks that the CNI-provisioned interface exists
// in the namespace, is up, has the address the fake plugin assigns and does
// not carry the underlay group ID (the libcni result cache is the source of
// truth for CNI interfaces, not the group marking).
func validateCNIInterfaceInNS(ns netns.NsHandle, ifName string) {
	err := netnamespace.In(ns, func() error {
		link, err := netlink.LinkByName(ifName)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("cni interface %s not found in namespace", ifName))
		Expect(link.Attrs().Group).NotTo(Equal(uint32(UnderlayGroupID)),
			fmt.Sprintf("cni interface %s should not carry the underlay group ID", ifName))
		Expect(link.Attrs().Flags&net.FlagUp).NotTo(BeZero(), fmt.Sprintf("cni interface %s is not up", ifName))
		validateIP(Default, link, underlayCNITestAddress)
		return nil
	})
	Expect(err).NotTo(HaveOccurred())
}
