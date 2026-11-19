// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var _ = Describe("SRv6 exception lifecycle", func() {
	It("updates shared exceptions, detects conflicts and cleans only owned rules without down/up", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		config := &SRv6Isolation{Source: "2001:db8:ffff::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		vpns := []L3VPNParams{{VRF: "red", TargetNS: path, RoutingIsolation: config}, {VRF: "blue", TargetNS: path, RoutingIsolation: config}}
		Expect(netnamespace.In(ns, func() error {
			lo, err := netlink.LinkByName("lo")
			if err != nil {
				return err
			}
			Expect(netlink.LinkSetUp(lo)).To(Succeed())
			guardAddress(lo, config.Source+"/128")
			guardAddress(lo, "2001:db8:ffff::2/128")
			return nil
		})).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		for _, vpn := range vpns {
			Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), vpn)).To(Succeed())
		}
		updates := make(chan netlink.LinkUpdate, 1024)
		done := make(chan struct{})
		Expect(netlink.LinkSubscribeAt(ns, updates, done)).To(Succeed())
		defer close(done)
		var original []isolationPolicyRule
		Expect(netnamespace.In(ns, func() error { var err error; original, err = listIsolationPolicyRules(); return err })).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		Expect(netnamespace.In(ns, func() error { Expect(listIsolationPolicyRules()).To(Equal(original)); return nil })).To(Succeed())
		vpns = vpns[:1]
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			Expect(verifySRv6Isolation("red", *config)).To(Succeed())
			for _, name := range []string{"red", "blue"} {
				vrf, err := lookupVRF(name)
				if err != nil {
					return err
				}
				Expect(vrf.Flags & net.FlagUp).To(Equal(net.FlagUp))
			}
			foreign := netlink.NewRule()
			foreign.Family, foreign.Priority, foreign.Protocol, foreign.Type = netlink.FAMILY_V6, 3, unix.RTPROT_STATIC, unix.FR_ACT_UNREACHABLE
			foreign.Src = guardCIDR("2001:db8:bad::/64")
			return netlink.RuleAdd(foreign)
		})).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, vpns)).To(MatchError(ContainSubstring("foreign rule conflicts")))
		Expect(netnamespace.In(ns, func() error {
			foreign := netlink.NewRule()
			foreign.Family, foreign.Priority, foreign.Protocol, foreign.Type = netlink.FAMILY_V6, 3, unix.RTPROT_STATIC, unix.FR_ACT_UNREACHABLE
			foreign.Src = guardCIDR("2001:db8:bad::/64")
			if err := netlink.RuleDel(foreign); err != nil {
				return err
			}
			foreign.Priority = 900
			return netlink.RuleAdd(foreign)
		})).To(Succeed())
		config.Source = "2001:db8:ffff::2"
		config.UnderlayTable = 702
		config.RemoteLocatorPrefixes = []string{"fc00:30::/48"}
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		Expect(netnamespace.In(ns, func() error { return verifySRv6Isolation("red", *config) })).To(Succeed())
		Consistently(func(g Gomega) {
			for {
				select {
				case update, ok := <-updates:
					g.Expect(ok).To(BeTrue(), "link subscription closed")
					name := update.Attrs().Name
					if name == "red" || name == "blue" {
						g.Expect(update.Attrs().Flags&net.FlagUp).To(Equal(net.FlagUp), "%s went down", name)
					}
				default:
					return
				}
			}
		}, 200*time.Millisecond, 10*time.Millisecond).Should(Succeed())

		Expect(netnamespace.In(ns, func() error {
			for _, name := range []string{"red", "blue"} {
				vrf, err := lookupVRF(name)
				if err != nil {
					return err
				}
				if err := netlink.LinkDel(vrf); err != nil {
					return err
				}
			}
			return nil
		})).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, nil)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			foreignFound := false
			for _, entry := range rules {
				Expect(ownedIsolationPolicyRule(entry)).To(BeFalse())
				foreignFound = foreignFound || entry.rule.Priority == 900
			}
			Expect(foreignFound).To(BeTrue())
			return nil
		})).To(Succeed())
	})
	It("rejects a removed tenant's table before changing rules or source", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		Expect(netnamespace.In(ns, func() error {
			return netlink.LinkAdd(&netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "removed"}, Table: 701})
		})).To(Succeed())
		config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		Expect(ReconcileSRv6Isolation(path, []L3VPNParams{{VRF: "red", RoutingIsolation: config}})).To(
			MatchError("underlayTable 701 belongs to tenant removed"))
		Expect(netnamespace.In(ns, func() error {
			_, err := lookupVRF("red")
			Expect(err).To(HaveOccurred())
			return nil
		})).To(Succeed())
	})

	It("cleans only an obsolete managed table owner before retrying isolation", func() {
		for _, managed := range []bool{false, true} {
			ns := newGuardNamespace()
			path := fmt.Sprintf("/proc/self/fd/%d", ns)
			owner := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "old"}, Table: 701}
			Expect(netnamespace.In(ns, func() error {
				if err := netlink.LinkAdd(owner); err != nil {
					return err
				}
				if managed {
					return netlink.LinkSetAlias(owner, managedVRFAlias)
				}
				return nil
			})).To(Succeed())
			config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
			vpns := []L3VPNParams{{VRF: "red", RoutingIsolation: config}}
			err := ReconcileSRv6Isolation(path, vpns)
			var conflict *VRFTableOwnershipError
			Expect(errors.As(err, &conflict)).To(BeTrue())
			Expect(CleanupObsoleteIsolationVRF(path, map[string]bool{"red": true, "old": true}, conflict)).To(MatchError(ContainSubstring("belongs to tenant old")))
			cleanup := CleanupObsoleteIsolationVRF(path, map[string]bool{"red": true}, conflict)
			if managed {
				Expect(cleanup).To(Succeed())
				Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
			} else {
				Expect(cleanup).To(MatchError(ContainSubstring("belongs to tenant old")))
				Expect(netnamespace.In(ns, func() error { _, err := lookupVRF("old"); return err })).To(Succeed())
			}
		}
	})

	It("retains shared-table guards when deleting an obsolete VRF and retries dump failures", func() {
		ns := newGuardNamespace()
		Expect(netnamespace.In(ns, func() error {
			for _, name := range []string{"red", "blue"} {
				Expect(netlink.LinkAdd(&netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: name}, Table: 901})).To(Succeed())
			}
			red, err := lookupVRF("red")
			if err != nil {
				return err
			}
			Expect(ensureVRFDefaultGuards(red)).To(Succeed())
			Expect(removeNamespaceSideVRFs(map[string]bool{"red": true})).To(BeEmpty())
			Expect(listVRFDefaultGuards(vrfDefaultGuard(901, netlink.FAMILY_V4))).To(HaveLen(1))
			originalList := guardRouteList
			guardRouteList = func(int, *netlink.Route, uint64) ([]netlink.Route, error) { return nil, unix.EPERM }
			failed := removeNamespaceSideVRFs(map[string]bool{})
			guardRouteList = originalList
			Expect(failed).To(HaveLen(1))
			Expect(lookupVRF("red")).NotTo(BeNil())
			Expect(removeNamespaceSideVRFs(map[string]bool{})).To(BeEmpty())
			_, err = lookupVRF("red")
			Expect(err).To(HaveOccurred())
			return nil
		})).To(Succeed())
	})

	It("distinguishes an empty capture queue from an invalid capture descriptor", func() {
		fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK, 0)
		Expect(err).To(Succeed())
		defer func() { Expect(unix.Close(fds[0])).To(Succeed()); Expect(unix.Close(fds[1])).To(Succeed()) }()
		Expect(capturedSRv6Source(fds[0], net.ParseIP("2001:db8::1"))).To(BeFalse())
		_, err = capturedSRv6Source(-1, net.ParseIP("2001:db8::1"))
		Expect(errors.Is(err, unix.EBADF)).To(BeTrue())
	})

	It("cleans owned rules despite a foreign rule using the reserved protocol", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		vpns := []L3VPNParams{{VRF: "red", RoutingIsolation: config}}
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		foreign := netlink.NewRule()
		foreign.Family, foreign.Priority, foreign.Protocol, foreign.Type = netlink.FAMILY_V6, 900, 242, unix.FR_ACT_UNREACHABLE
		foreign.Src = guardCIDR("2001:db8:bad::/64")
		Expect(netnamespace.In(ns, func() error { return netlink.RuleAdd(foreign) })).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, vpns)).To(MatchError(ContainSubstring("reserved OpenPERouter protocol")))
		for range 2 {
			Expect(ReconcileSRv6Isolation(path, nil)).To(Succeed())
		}
		Expect(netnamespace.In(ns, func() error {
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			found := false
			for _, entry := range rules {
				Expect(ownedIsolationPolicyRule(entry)).To(BeFalse())
				found = found || entry.rule.Priority == foreign.Priority && entry.rule.Protocol == foreign.Protocol
			}
			Expect(found).To(BeTrue())
			return nil
		})).To(Succeed())
	})

	It("verifies adjacent lookup and terminal priorities and repairs malformed pairs", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		vpns := []L3VPNParams{{VRF: "red", RoutingIsolation: config}}
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, entry := range rules {
				if ownedIsolationPolicyRule(entry) && entry.rule.IifName == "lo" && entry.rule.Type == unix.FR_ACT_UNREACHABLE {
					rule := entry.rule
					if err := netlink.RuleDel(&rule); err != nil {
						return err
					}
					rule.Priority = 10
					if err := netlink.RuleAdd(&rule); err != nil {
						return err
					}
				}
			}
			Expect(verifySRv6Isolation("red", *config)).To(MatchError(ContainSubstring("must be reconciled")))
			return nil
		})).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		Expect(netnamespace.In(ns, func() error { return verifySRv6Isolation("red", *config) })).To(Succeed())
	})

})
