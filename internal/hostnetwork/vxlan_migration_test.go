// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var _ = Describe("VXLAN capability probe and legacy purge", func() {
	const nsName = "vxlanmigtestns"
	var ns netns.NsHandle

	BeforeEach(func() {
		cleanTest(nsName)
		ns = createTestNS(nsName)
	})
	AfterEach(func() {
		cleanTest(nsName)
	})

	addLegacyVXLAN := func(name string, vni int, port int) {
		Expect(netnamespace.In(ns, func() error {
			vxlan := &netlink.Vxlan{
				LinkAttrs: netlink.LinkAttrs{Name: name},
				VxlanId:   vni,
				Port:      port,
			}
			return netlink.LinkAdd(vxlan)
		})).To(Succeed())
	}

	addExternalVXLAN := func(name string, port int) {
		Expect(netnamespace.In(ns, func() error {
			_, err := createVniFilterVxlan(&netlink.Vxlan{
				LinkAttrs: netlink.LinkAttrs{Name: name},
				FlowBased: true,
				Port:      port,
			})
			return err
		})).To(Succeed())
	}

	It("purges a legacy VXLAN on a configured port and keeps a migrated one", func() {
		addLegacyVXLAN("vni100", 100, 4789)
		addExternalVXLAN("vni200", 4789)

		Expect(PurgeLegacyVXLans("/var/run/netns/"+nsName, []int32{4789})).To(Succeed())

		Expect(netnamespace.In(ns, func() error {
			_, err := netlink.LinkByName("vni100")
			Expect(errors.As(err, &netlink.LinkNotFoundError{})).To(BeTrue(), "legacy VXLAN must be purged")
			_, err = netlink.LinkByName("vni200")
			Expect(err).NotTo(HaveOccurred(), "migrated VXLAN must be kept")
			return nil
		})).To(Succeed())
	})

	It("ignores a legacy VXLAN on an unconfigured port", func() {
		addLegacyVXLAN("vni300", 300, 4790)

		Expect(PurgeLegacyVXLans("/var/run/netns/"+nsName, []int32{4789})).To(Succeed())

		Expect(netnamespace.In(ns, func() error {
			_, err := netlink.LinkByName("vni300")
			Expect(err).NotTo(HaveOccurred(), "VXLAN on another port must be kept")
			return nil
		})).To(Succeed())
	})

	It("fails closed and does not cache an unsupported kernel", func() {
		original := probeVniFilterDevice
		DeferCleanup(func() {
			probeVniFilterDevice = original
			vniFilterSupport.Store(false)
		})
		vniFilterSupport.Store(false)
		probeVniFilterDevice = func() error { return errors.New("operation not supported") }

		err := EnsureVniFilterSupport("/var/run/netns/" + nsName)
		Expect(err).To(MatchError(ContainSubstring("Linux 5.18")))
		Expect(vniFilterSupport.Load()).To(BeFalse(), "a failed probe must not be cached")
	})

	It("caches a successful probe", func() {
		original := probeVniFilterDevice
		DeferCleanup(func() {
			probeVniFilterDevice = original
			vniFilterSupport.Store(false)
		})
		vniFilterSupport.Store(false)

		Expect(EnsureVniFilterSupport("/var/run/netns/" + nsName)).To(Succeed())
		Expect(vniFilterSupport.Load()).To(BeTrue())

		probeVniFilterDevice = func() error { return errors.New("must not run again") }
		Expect(EnsureVniFilterSupport("/var/run/netns/" + nsName)).To(Succeed())
	})
})
