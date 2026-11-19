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
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

var _ = Describe("SRv6 exception lifecycle", func() {
	It("filters concrete guard tables in the kernel when supported", func() {
		ns := newGuardNamespace()
		Expect(netnamespace.In(ns, func() error {
			for i, name := range []string{"red", "blue"} {
				vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: name}, Table: uint32(701 + i)}
				Expect(netlink.LinkAdd(vrf)).To(Succeed())
				Expect(ensureVRFDefaultGuards(vrf)).To(Succeed())
			}
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				messages, err := dumpGuardRoutes(family, 701, true)
				if errors.Is(err, unix.ENOPROTOOPT) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EINVAL) {
					Expect(listVRFDefaultGuards(vrfDefaultGuard(701, family))).To(HaveLen(1))
					continue
				}
				Expect(err).To(Succeed())
				allTables := vrfDefaultGuard(0, family)
				var guards []netlink.Route
				for _, message := range messages {
					route, err := decodeGuardRoute(message, family, &allTables)
					Expect(err).To(Succeed())
					if route != nil {
						guards = append(guards, *route)
					}
				}
				Expect(guards).To(Equal([]netlink.Route{vrfDefaultGuard(701, family)}))
			}
			return nil
		})).To(Succeed())
	})

	It("keeps shared-table guards until all obsolete owners have been deleted", func() {
		ns := newGuardNamespace()
		Expect(netnamespace.In(ns, func() error {
			for _, name := range []string{"red", "blue"} {
				Expect(netlink.LinkAdd(&netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: name}, Table: 901})).To(Succeed())
			}
			red, err := lookupVRF("red")
			Expect(err).To(Succeed())
			Expect(ensureVRFDefaultGuards(red)).To(Succeed())
			original := deleteVRFLink
			defer func() { deleteVRFLink = original }()
			calls := 0
			var survivor string
			deleteVRFLink = func(link netlink.Link) error {
				calls++
				if calls == 2 {
					survivor = link.Attrs().Name
					return unix.EPERM
				}
				return original(link)
			}
			Expect(errors.Is(errors.Join(removeNamespaceSideVRFs(nil)...), unix.EPERM)).To(BeTrue())
			Expect(lookupVRF(survivor)).NotTo(BeNil())
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(901, family))).To(HaveLen(1))
			}
			deleteVRFLink = original
			Expect(removeNamespaceSideVRFs(nil)).To(BeEmpty())
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(901, family))).To(BeEmpty())
			}
			return nil
		})).To(Succeed())
	})

	It("rejects foreign occupants of retained priorities before a legacy transition", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		Expect(ReconcileSRv6Isolation(path, []L3VPNParams{{VRF: "red", RoutingIsolation: config}})).To(Succeed())
		legacy := []L3VPNParams{{VRF: "red"}}
		Expect(StageSRv6Isolation(path, legacy, net.ParseIP(config.Source))).To(Succeed())
		foreign := netlink.NewRule()
		foreign.Family, foreign.Priority, foreign.Table, foreign.Protocol = netlink.FAMILY_V6, 1, 254, 99
		var before []isolationPolicyRule
		Expect(netnamespace.In(ns, func() error {
			if err := netlink.RuleAdd(foreign); err != nil {
				return err
			}
			var err error
			before, err = listIsolationPolicyRules()
			return err
		})).To(Succeed())
		Expect(PrepareSRv6Isolation(path, legacy, net.ParseIP(config.Source))).To(
			MatchError("foreign rule conflicts with isolation priority 1"))
		Expect(netnamespace.In(ns, func() error {
			Expect(listIsolationPolicyRules()).To(Equal(before))
			return netlink.RuleDel(foreign)
		})).To(Succeed())
		Expect(PrepareSRv6Isolation(path, legacy, net.ParseIP(config.Source))).To(Succeed())
	})

	It("distinguishes a source-specific IPv6 default from an unconditional guard", func() {
		ns := newGuardNamespace()
		Expect(netnamespace.In(ns, func() error {
			vrf, err := createVRF("red")
			if err != nil {
				return err
			}
			req := nl.NewNetlinkRequest(unix.RTM_NEWROUTE, unix.NLM_F_CREATE|unix.NLM_F_EXCL|unix.NLM_F_ACK)
			req.AddData(&nl.RtMsg{RtMsg: unix.RtMsg{Family: unix.AF_INET6, Src_len: 64,
				Type: unix.RTN_UNREACHABLE, Protocol: uint8(vrfGuardProtocol)}})
			req.AddData(nl.NewRtAttr(unix.RTA_TABLE, nl.Uint32Attr(vrf.Table)))
			req.AddData(nl.NewRtAttr(unix.RTA_PRIORITY, nl.Uint32Attr(vrfGuardPriority)))
			req.AddData(nl.NewRtAttr(unix.RTA_SRC, net.ParseIP("2001:db8:1::").To16()))
			_, err = req.Execute(unix.NETLINK_ROUTE, 0)
			if err != nil {
				return err
			}
			Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V6))).To(BeEmpty())
			Expect(ensureVRFDefaultGuards(vrf)).To(Succeed())
			Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V6))).To(HaveLen(1))
			Expect(removeVRFDefaultGuards(vrf)).To(Succeed())
			routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: int(vrf.Table)}, netlink.RT_FILTER_TABLE)
			Expect(err).To(Succeed())
			Expect(routes).To(HaveLen(1), "source-specific foreign default must survive guard cleanup")
			return nil
		})).To(Succeed())
	})

	It("classifies a partial legacy guard removal as a per-VRF transition failure", func() {
		ns := newGuardNamespace()
		Expect(netnamespace.In(ns, func() error {
			Expect(setupVRF("red", srv6VRF, guardedSRv6VRF)).To(Succeed())
			original := guardRouteDel
			defer func() { guardRouteDel = original }()
			guardRouteDel = func(route *netlink.Route) error {
				if route.Family == netlink.FAMILY_V6 {
					return unix.EPERM
				}
				return original(route)
			}
			err := setupVRF("red", srv6VRF)
			var guardErr *VRFGuardError
			Expect(errors.As(err, &guardErr)).To(BeTrue())
			Expect(guardErr.VRF).To(Equal("red"))
			Expect(errors.Is(err, unix.EPERM)).To(BeTrue())
			guardRouteDel = original
			Expect(setupVRF("red", srv6VRF)).To(Succeed())
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(BeEmpty())
			}
			return nil
		})).To(Succeed())
	})

	It("finishes a new desired state after a staged source change", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		original := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254,
			RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		initial := []L3VPNParams{{VRF: "red", RoutingIsolation: original},
			{VRF: "blue", RoutingIsolation: original}}
		Expect(ReconcileSRv6Isolation(path, initial)).To(Succeed())
		changed := *original
		changed.Source = "2001:db8::2"
		bothChanged := []L3VPNParams{{VRF: "red", RoutingIsolation: &changed},
			{VRF: "blue", RoutingIsolation: &changed}}
		Expect(StageSRv6Isolation(path, bothChanged, net.ParseIP(changed.Source))).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			return setSRv6TunnelSource(net.ParseIP(changed.Source))
		})).To(Succeed())
		// A new controller event may restore the earlier configuration before
		// the staged replacement has been retired.
		Expect(StageSRv6Isolation(path, initial, net.ParseIP(original.Source))).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, initial)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			return verifySRv6Isolation("red", *original)
		})).To(Succeed())
		Expect(StageSRv6Isolation(path, bothChanged, net.ParseIP(changed.Source))).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			return setSRv6TunnelSource(net.ParseIP(changed.Source))
		})).To(Succeed())
		transition := []L3VPNParams{{VRF: "red"}, {VRF: "blue", RoutingIsolation: &changed}}
		Expect(netnamespace.In(ns, func() error {
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, entry := range rules {
				if ownedIsolationPolicyRule(entry) && entry.rule.IifName == transition[0].VRF &&
					entry.rule.Type == unix.FR_ACT_UNREACHABLE &&
					entry.rule.Src.IP.Equal(net.ParseIP(changed.Source)) {
					return netlink.RuleDel(&entry.rule)
				}
			}
			return fmt.Errorf("staged red terminal was not found")
		})).To(Succeed())
		Expect(PrepareSRv6Isolation(path, transition, net.ParseIP(changed.Source))).To(
			MatchError(ContainSubstring("lacks a complete SRv6 exception")))
		for range 2 {
			Expect(StageSRv6Isolation(path, transition, net.ParseIP(changed.Source))).To(Succeed())
		}
		Expect(PrepareSRv6Isolation(path, transition, net.ParseIP(changed.Source))).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			return verifySRv6Isolation("red", SRv6Isolation{Source: changed.Source,
				UnderlayTable: changed.UnderlayTable, RemoteLocatorPrefixes: changed.RemoteLocatorPrefixes})
		})).To(Succeed())
		Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), transition[0])).To(Succeed())
		Expect(FinishSRv6Isolation(path, transition, net.ParseIP(changed.Source))).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, entry := range rules {
				if ownedIsolationPolicyRule(entry) {
					Expect(entry.rule.IifName).NotTo(Equal("red"))
					Expect(entry.rule.Src.IP.Equal(net.ParseIP(changed.Source))).To(BeTrue())
				}
			}
			return verifySRv6Isolation("blue", changed)
		})).To(Succeed())
		allLegacy := []L3VPNParams{{VRF: "red"}, {VRF: "blue"}}
		Expect(StageSRv6Isolation(path, allLegacy, net.ParseIP(changed.Source))).To(Succeed())
		Expect(PrepareSRv6Isolation(path, allLegacy, net.ParseIP(changed.Source))).To(Succeed())
		Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), allLegacy[1])).To(Succeed())
		for range 2 {
			Expect(FinishSRv6Isolation(path, allLegacy, net.ParseIP(changed.Source))).To(Succeed())
		}
		Expect(netnamespace.In(ns, func() error {
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, rule := range rules {
				Expect(ownedIsolationPolicyRule(rule)).To(BeFalse())
			}
			return nil
		})).To(Succeed())
	})

	It("allows a source update when legacy guards are gone and exception retirement is pending", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		original := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254,
			RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		initial := []L3VPNParams{{VRF: "red", RoutingIsolation: original}, {VRF: "blue", RoutingIsolation: original}}
		Expect(ReconcileSRv6Isolation(path, initial)).To(Succeed())
		// A failure before the final policy update can leave old rules after
		// attachment setup has already removed the legacy VRF's guards.
		Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), L3VPNParams{VRF: "red"})).To(Succeed())
		changed := *original
		changed.Source = "2001:db8::2"
		desired := []L3VPNParams{{VRF: "red"}, {VRF: "blue", RoutingIsolation: &changed}}
		Expect(StageSRv6Isolation(path, desired, nil)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			Expect(getSRv6TunnelSource()).To(Equal(net.ParseIP(original.Source).To16()))
			return nil
		})).To(Succeed())
		Expect(PrepareSRv6Isolation(path, desired, nil)).To(Succeed())
		Expect(FinishSRv6Isolation(path, desired, nil)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			if err := verifySRv6Isolation("blue", changed); err != nil {
				return err
			}
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, entry := range rules {
				if ownedIsolationPolicyRule(entry) {
					Expect(entry.rule.IifName).NotTo(Equal("red"))
					Expect(entry.rule.Src.IP.Equal(net.ParseIP(changed.Source))).To(BeTrue())
				}
			}
			return nil
		})).To(Succeed())
	})

	It("reports a failed packet listener instead of treating it as isolation", func() {
		ns := newGuardNamespace()
		var receiver net.PacketConn
		Expect(netnamespace.In(ns, func() error {
			var err error
			receiver, err = net.ListenPacket("udp4", "127.0.0.1:17001")
			return err
		})).To(Succeed())
		defer func() { Expect(receiver.Close()).To(Succeed()) }()
		delivered, err := srv6PacketDelivered(ns, netlink.FAMILY_V4, "127.0.0.1", ns)
		Expect(delivered).To(BeFalse())
		Expect(errors.Is(err, unix.EADDRINUSE)).To(BeTrue(), "unexpected listener error: %v", err)
	})

	It("keeps live VRF guards on link deletion failure and retries orphan guard cleanup", func() {
		ns := newGuardNamespace()
		Expect(netnamespace.In(ns, func() error {
			Expect(setupVRF("red", evpnVRF)).To(Succeed())
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			originalDelete, originalRouteDelete := deleteVRFLink, guardRouteDel
			defer func() { deleteVRFLink, guardRouteDel = originalDelete, originalRouteDelete }()
			failure := errors.New("injected link deletion failure")
			deleteVRFLink = func(netlink.Link) error { return failure }
			Expect(errors.Join(removeNamespaceSideVRFs(map[string]bool{})...)).To(MatchError(ContainSubstring(failure.Error())))
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(HaveLen(1))
			}
			deleteVRFLink = originalDelete
			guardRouteDel = func(route *netlink.Route) error {
				if route.Family == netlink.FAMILY_V6 {
					return errors.New("injected IPv6 guard deletion failure")
				}
				return originalRouteDelete(route)
			}
			Expect(removeNamespaceSideVRFs(map[string]bool{})).NotTo(BeEmpty())
			_, err = lookupVRF("red")
			Expect(err).To(HaveOccurred(), "guard deletion failure must occur after deleting the obsolete VRF")
			guardRouteDel = originalRouteDelete
			Expect(removeNamespaceSideVRFs(map[string]bool{})).To(BeEmpty())
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(BeEmpty())
			}
			return nil
		})).To(Succeed())
	})

	It("retains exceptions until a legacy VPN has removed its guards", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254,
			RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		isolated := []L3VPNParams{{VRF: "red", RoutingIsolation: config}}
		legacy := []L3VPNParams{{VRF: "red"}}
		Expect(ReconcileSRv6Isolation(path, isolated)).To(Succeed())
		for range 2 {
			Expect(PrepareSRv6Isolation(path, legacy, nil)).To(Succeed())
			Expect(netnamespace.In(ns, func() error {
				vrf, err := lookupVRF("red")
				if err != nil {
					return err
				}
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4))).To(HaveLen(1))
				return verifySRv6Isolation("red", *config)
			})).To(Succeed())
		}
		Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), legacy[0])).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, legacy)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4))).To(BeEmpty())
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, rule := range rules {
				Expect(ownedIsolationPolicyRule(rule)).To(BeFalse())
			}
			return nil
		})).To(Succeed())
	})

	It("retires only a legacy VPN's rules while another isolated VPN remains", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254,
			RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		isolated := []L3VPNParams{{VRF: "red", RoutingIsolation: config}, {VRF: "blue", RoutingIsolation: config}}
		transition := []L3VPNParams{{VRF: "red"}, {VRF: "blue", RoutingIsolation: config}}
		Expect(ReconcileSRv6Isolation(path, isolated)).To(Succeed())
		Expect(PrepareSRv6Isolation(path, transition, nil)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			return verifySRv6Isolation("red", *config)
		})).To(Succeed())
		Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), transition[0])).To(Succeed())
		Expect(ReconcileSRv6Isolation(path, transition)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			if err := verifySRv6Isolation("blue", *config); err != nil {
				return err
			}
			red, err := lookupVRF("red")
			if err != nil {
				return err
			}
			Expect(listVRFDefaultGuards(vrfDefaultGuard(red.Table, netlink.FAMILY_V4))).To(BeEmpty())
			rules, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			for _, rule := range rules {
				if ownedIsolationPolicyRule(rule) {
					Expect(rule.rule.IifName).NotTo(Equal("red"))
				}
			}
			return nil
		})).To(Succeed())
	})

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

	It("cleans managed obsolete attachments without detaching foreign ports and retries partial deletion", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		var owner *netlink.Vrf
		Expect(netnamespace.In(ns, func() error {
			if err := setupVRF("old", evpnVRF); err != nil {
				return err
			}
			var err error
			owner, err = lookupVRF("old")
			if err != nil {
				return err
			}
			port := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "pe-s-201", MasterIndex: owner.Index}, PeerName: "host-s-201"}
			if err := netlink.LinkAdd(port); err != nil {
				return err
			}
			bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: BridgeName(300), MasterIndex: owner.Index}}
			if err := netlink.LinkAdd(bridge); err != nil {
				return err
			}
			veth := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "pe-e-300", MasterIndex: bridge.Index}, PeerName: "host-e-300"}
			if err := netlink.LinkAdd(veth); err != nil {
				return err
			}
			vxlan := &netlink.Vxlan{LinkAttrs: netlink.LinkAttrs{Name: vxLanNameFromVNI(300), MasterIndex: bridge.Index},
				VxlanId: 300, Port: 4789}
			if err := netlink.LinkAdd(vxlan); err != nil {
				return err
			}
			return netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "foreign-port", MasterIndex: bridge.Index}})
		})).To(Succeed())
		conflict := &VRFTableOwnershipError{VRF: owner.Name, Table: owner.Table}
		Expect(CleanupObsoleteIsolationVRF(path, nil, conflict)).To(MatchError(ContainSubstring("foreign-port")))
		Expect(netnamespace.In(ns, func() error {
			for _, name := range []string{"old", "pe-s-201", "pe-e-300", BridgeName(300), vxLanNameFromVNI(300)} {
				Expect(netlink.LinkByName(name)).NotTo(BeNil())
			}
			foreign, err := netlink.LinkByName("foreign-port")
			if err != nil {
				return err
			}
			return netlink.LinkDel(foreign)
		})).To(Succeed())
		originalDelete := deleteVRFAttachment
		defer func() { deleteVRFAttachment = originalDelete }()
		failure := errors.New("injected attachment deletion failure")
		calls := 0
		deleteVRFAttachment = func(link netlink.Link) error {
			calls++
			if calls == 2 {
				return failure
			}
			return originalDelete(link)
		}
		Expect(errors.Is(CleanupObsoleteIsolationVRF(path, nil, conflict), failure)).To(BeTrue())
		Expect(calls).To(Equal(2))
		Expect(netnamespace.In(ns, func() error {
			Expect(lookupVRF("old")).NotTo(BeNil())
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(owner.Table, family))).To(HaveLen(1))
			}
			return nil
		})).To(Succeed())
		deleteVRFAttachment = originalDelete
		Expect(CleanupObsoleteIsolationVRF(path, nil, conflict)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			for _, name := range []string{"old", "pe-s-201", "host-s-201", "pe-e-300", "host-e-300", BridgeName(300), vxLanNameFromVNI(300)} {
				_, err := netlink.LinkByName(name)
				var missing netlink.LinkNotFoundError
				Expect(errors.As(err, &missing)).To(BeTrue(), "link %s: %v", name, err)
			}
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(owner.Table, family))).To(BeEmpty())
			}
			return nil
		})).To(Succeed())
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
