// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"fmt"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var _ = Describe("Minimal SRv6 workaround", func() {
	It("reproduces the unreachable-default conflict and restores both tenant families", func() {
		left, right, local, remote := minimalSRv6Fixture(false)
		paths := minimalSRv6Paths(right)
		checkMixedTransportPaths(left, paths)
		By("unreachable defaults alone block the outer SRv6 relookup")
		local.stop()
		remote.stop()
		Expect(netnamespace.In(left.ns, func() error {
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			return ensureVRFDefaultGuards(vrf)
		})).To(Succeed())
		local.start()
		remote.start()
		Eventually(func() bool { return minimalSRv6RoutesMatch(left, paths, 1) },
			10*time.Second, 100*time.Millisecond).Should(BeTrue())
		for _, path := range paths {
			Consistently(func() error { return path.packet(left.clientNS) }, time.Second, 100*time.Millisecond).ShouldNot(Succeed(), "unreachable conflict for %s", path.target)
		}
		By("scoped exceptions restore the original dataplane")
		reconcileSRv6TestNode(left, srv6TestIsolation(left))
		checkMixedTransportPaths(left, paths)
	})

	// runasroot and real FRR are explicit opt-ins for this live transition check.
	It("leaves a live legacy VPN forwarding unchanged on a priority-one conflict", func() {
		left, right, _, _ := minimalSRv6Fixture(false)
		paths := minimalSRv6Paths(right)
		checkMixedTransportPaths(left, paths)
		foreign := netlink.NewRule()
		foreign.Family, foreign.Priority, foreign.Type = netlink.FAMILY_V6, 1, unix.FR_ACT_UNREACHABLE
		foreign.Src = guardCIDR("2001:db8:bad::/64")
		var before []isolationPolicyRule
		var originalSource net.IP
		Expect(netnamespace.In(left.ns, func() error {
			if err := netlink.RuleAdd(foreign); err != nil {
				return err
			}
			var err error
			before, err = listIsolationPolicyRules()
			if err != nil {
				return err
			}
			originalSource, err = getSRv6TunnelSource()
			return err
		})).To(Succeed())
		config := srv6TestIsolation(left)
		config.UnderlayTable = 254
		path := fmt.Sprintf("/proc/self/fd/%d", left.ns)
		Expect(ReconcileSRv6Isolation(path, []L3VPNParams{{VRF: "red", RoutingIsolation: config}}, "blue")).To(
			MatchError(ContainSubstring("foreign rule conflicts")))
		Expect(netnamespace.In(left.ns, func() error {
			Expect(listIsolationPolicyRules()).To(Equal(before))
			Expect(getSRv6TunnelSource()).To(Equal(originalSource))
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(BeEmpty())
			}
			return nil
		})).To(Succeed())
		checkMixedTransportPaths(left, paths)
	})

	It("needs terminal failure to prevent outer packets taking a tenant default when the selected underlay has no route", func() {
		left, right, _, _ := minimalSRv6Fixture(false)
		config := srv6TestIsolation(left)
		config.UnderlayTable = 701
		copySRv6TestUnderlay(left, 701)
		reconcileSRv6TestNode(left, config)
		paths := minimalSRv6Paths(right)
		wrong := newGuardNamespace()
		var capture int
		var closeCapture func()
		Expect(netnamespace.In(left.ns, func() error {
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			port := guardVeth(wrong, "wrong", "2001:db8:bad::1/64", "2001:db8:bad::2/64", vrf)
			route := netlink.Route{Table: int(vrf.Table), Dst: guardCIDR("::/0"), Gw: net.ParseIP("2001:db8:bad::2"), LinkIndex: port.Attrs().Index, Priority: 20, Protocol: unix.RTPROT_STATIC}
			if err := netlink.RouteAdd(&route); err != nil {
				return err
			}
			capture, closeCapture = transportCaptureSocket(port.Attrs().Index)
			return nil
		})).To(Succeed())
		defer func() { closeCapture() }()
		checkMixedTransportPaths(left, paths)
		route := removeTestLocatorRoute(left, 701)
		Expect(paths[1].packet(left.clientNS)).NotTo(Succeed())
		Expect(capturedSRv6Source(capture, net.ParseIP(config.Source))).To(BeFalse())
		By("negative control removes only terminal rules, retaining guards and narrow lookups")
		Expect(netnamespace.In(left.ns, func() error {
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, entry := range rules {
				if ownedIsolationPolicyRule(entry) && entry.rule.Type == unix.FR_ACT_UNREACHABLE {
					r := entry.rule
					if err := netlink.RuleDel(&r); err != nil {
						return err
					}
				}
			}
			return nil
		})).To(Succeed())
		for range 3 {
			Eventually(func() (bool, error) {
				Expect(paths[1].packet(left.clientNS)).NotTo(Succeed())
				return capturedSRv6Source(capture, net.ParseIP(config.Source))
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"outer SRv6 must be observed on the wrong tenant default")
		}
		GinkgoWriter.Println("CONFIRMED: without terminal action, outer SRv6 packets take an unrelated tenant default")
		reconcileSRv6TestNode(left, config)
		// Drain frames from the deliberate unprotected control before probing repair.
		for {
			matched, err := capturedSRv6Source(capture, net.ParseIP(config.Source))
			Expect(err).To(Succeed())
			if !matched {
				break
			}
		}
		Expect(paths[1].packet(left.clientNS)).NotTo(Succeed())
		Expect(capturedSRv6Source(capture, net.ParseIP(config.Source))).To(BeFalse())
		Expect(netnamespace.In(left.ns, func() error { return netlink.RouteAdd(&route) })).To(Succeed())
		checkMixedTransportPaths(left, paths)
	})

	It("rejects dedicated underlay for legacy End.DT6 because decap/re-encap bypasses its scoped lookup", func() {
		left, right, _, remote := minimalSRv6Fixture(true)
		config := srv6TestIsolation(left)
		reconcileSRv6TestNode(left, config)
		ingress := newSRv6TransitIngress(left, "2001:db8:102::2")
		packet := func() error {
			return netnamespace.In(ingress, func() error {
				return guardPacketVia(netlink.FAMILY_V6, "2001:db8:102::2", right.clientNS, "", "2001:db8:feed::2")
			})
		}
		Eventually(packet, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		copySRv6TestUnderlay(left, 701)
		var before []isolationPolicyRule
		var beforeSource net.IP
		Expect(netnamespace.In(left.ns, func() error {
			var err error
			before, err = listIsolationPolicyRules()
			if err != nil {
				return err
			}
			beforeSource, err = getSRv6TunnelSource()
			return err
		})).To(Succeed())
		custom := *config
		custom.UnderlayTable = 701
		targetNS := fmt.Sprintf("/proc/self/fd/%d", left.ns)
		Expect(ReconcileSRv6Isolation(targetNS, []L3VPNParams{{VRF: "red", RoutingIsolation: &custom}})).To(
			MatchError(ContainSubstring("preserves underlay iif")))
		Expect(netnamespace.In(left.ns, func() error {
			Expect(listIsolationPolicyRules()).To(Equal(before))
			Expect(getSRv6TunnelSource()).To(Equal(beforeSource))
			return nil
		})).To(Succeed())
		Expect(packet()).To(Succeed())
		By("runtime rejects real legacy SID state, leaving the working main exceptions intact")
		Expect(netnamespace.In(left.ns, func() error {
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			custom := *config
			custom.UnderlayTable = 701
			Expect(validateSRv6Decapsulation(vrf, custom)).To(MatchError(ContainSubstring("preserves underlay iif")))
			// Construct only the unsupported negative control directly, bypassing the
			// API/runtime check. This must never be installed by normal reconciliation.
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, entry := range rules {
				if !ownedIsolationPolicyRule(entry) || entry.rule.Type != unix.FR_ACT_TO_TBL {
					continue
				}
				r := entry.rule
				if err := netlink.RuleDel(&r); err != nil {
					return err
				}
				r.Table = 701
				if err := netlink.RuleAdd(&r); err != nil {
					return err
				}
			}
			return nil
		})).To(Succeed())
		Eventually(packet, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		removeTestLocatorRoute(left, 701)
		var capture int
		Expect(netnamespace.In(right.ns, func() error {
			port, err := netlink.LinkByName(right.transit)
			if err != nil {
				return err
			}
			capture = srv6CaptureSocket(port.Attrs().Index)
			return nil
		})).To(Succeed())
		defer func() { Expect(unix.Close(capture)).To(Succeed()) }()
		refreshMinimalSRv6Routes(left, remote, minimalSRv6Paths(right))
		for range 3 {
			Expect(packet()).To(Succeed())
			Expect(capturedSRv6Source(capture, net.ParseIP(config.Source))).To(BeTrue())
		}
		GinkgoWriter.Println("CONFIRMED: legacy End.DT6 transit uses main despite missing selected-table locator route")
	})
})

func minimalSRv6Fixture(perFamily bool) (srv6GuardNode, srv6GuardNode, *guardFRR, *guardFRR) {
	requireGuardFRR()
	left, right := newMixedSRv6Pair()
	a := newGuardFRR(left.ns, left.configuration(perFamily, frr.HEncaps))
	b := newGuardFRR(right.ns, right.configuration(perFamily, frr.HEncaps))
	a.start()
	b.start()
	return left, right, a, b
}

func minimalSRv6Paths(right srv6GuardNode) []mixedTransportPath {
	return []mixedTransportPath{
		{transport: "srv6", family: netlink.FAMILY_V4, target: "198.51.100.2", prefix: "198.51.100.0/24", receiver: right.clientNS},
		{transport: "srv6", family: netlink.FAMILY_V6, target: "2001:db8:102::2", prefix: "2001:db8:102::/64", receiver: right.clientNS},
	}
}

func removeTestLocatorRoute(node srv6GuardNode, table int) netlink.Route {
	var route netlink.Route
	Expect(netnamespace.In(node.ns, func() error {
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: table, Dst: guardCIDR("fc00:10:2::/48")}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
		if err != nil {
			return err
		}
		Expect(routes).To(HaveLen(1))
		route = routes[0]
		return netlink.RouteDel(&route)
	})).To(Succeed())
	return route
}

// Replacing the BGP route recreates its LWT state, unlike flushing only the
// general IPv6 route cache. This makes the control exercise an uncached lookup.
func refreshMinimalSRv6Routes(node srv6GuardNode, remote *guardFRR, paths []mixedTransportPath) {
	setMixedTransportNetworks(remote, 65002, paths, "srv6", false)
	Eventually(func() bool { return minimalSRv6RoutesMatch(node, paths, 0) },
		10*time.Second, 100*time.Millisecond).Should(BeTrue())
	setMixedTransportNetworks(remote, 65002, paths, "srv6", true)
	Eventually(func() bool { return minimalSRv6RoutesMatch(node, paths, 1) },
		10*time.Second, 100*time.Millisecond).Should(BeTrue())
}

func minimalSRv6RoutesMatch(node srv6GuardNode, paths []mixedTransportPath, count int) bool {
	matched := true
	Expect(netnamespace.In(node.ns, func() error {
		vrf, err := lookupVRF("red")
		if err != nil {
			return err
		}
		for _, path := range paths {
			routes, err := netlink.RouteListFiltered(path.family, &netlink.Route{Table: int(vrf.Table), Dst: guardCIDR(path.prefix)}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
			if err != nil {
				return err
			}
			matched = matched && len(routes) == count
		}
		return nil
	})).To(Succeed())
	return matched
}
