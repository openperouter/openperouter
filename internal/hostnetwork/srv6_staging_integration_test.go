// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var _ = Describe("SRv6 staging recovery", func() {
	It("allows existing large configurations to retire without reserving another rotation", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		source := net.ParseIP("2001:db8::1")
		// Model a configuration accepted before rotation headroom was required.
		Expect(netnamespace.In(ns, func() error {
			domains := map[string]SRv6Isolation{}
			for i, name := range []string{"red", "blue"} {
				Expect(setupVRF(name, srv6VRF, guardedSRv6VRF)).To(Succeed())
				domains[name] = SRv6Isolation{Source: source.String(), UnderlayTable: 254,
					RemoteLocatorPrefixes: rotationLocators(32 + i*256)}
			}
			current, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			rules, err := planIsolationPriorities(desiredIsolationRules(domains), current)
			if err != nil {
				return err
			}
			Expect(stageIsolationRules(current, rules, netlink.RuleAdd)).To(Succeed())
			return setSRv6TunnelSource(source)
		})).To(Succeed())
		legacy := []L3VPNParams{{VRF: "red"}, {VRF: "blue"}}
		Expect(StageSRv6Isolation(path, legacy, source)).To(Succeed())
		Expect(PrepareSRv6Isolation(path, legacy, source)).To(Succeed())
		for _, vpn := range legacy {
			Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), vpn)).To(Succeed())
		}
		Expect(FinishSRv6Isolation(path, legacy, source)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			current, err := listIsolationPolicyRules()
			if err != nil {
				return err
			}
			Expect(ownedIsolationRules(current)).To(BeEmpty())
			Expect(getSRv6TunnelSource()).To(Equal(source.To16()))
			return nil
		})).To(Succeed())
	})

	It("reclaims superseded sources while preserving the actual source after a partial reload", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		config := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: rotationLocators(32)}
		vpns := []L3VPNParams{{VRF: "red", RoutingIsolation: config}}
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		active := config.Source
		for _, next := range []string{"2001:db8::2", "2001:db8::3", "2001:db8::4", "2001:db8::1"} {
			config.Source = next
			Expect(StageSRv6Isolation(path, vpns, net.ParseIP(next))).To(Succeed())
			Expect(netnamespace.In(ns, func() error {
				Expect(getSRv6TunnelSource()).To(Equal(net.ParseIP(active).To16()))
				expectStagedSourcePairs(vpns[0].VRF, config.RemoteLocatorPrefixes, active, next)
				if next == "2001:db8::3" {
					// FRR may change the source before reporting a reload failure.
					Expect(setSRv6TunnelSource(net.ParseIP(next))).To(Succeed())
					active = next
				}
				return nil
			})).To(Succeed())
		}
		Expect(ReconcileSRv6Isolation(path, vpns)).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			expectStagedSourcePairs("red", config.RemoteLocatorPrefixes, config.Source)
			return verifySRv6Isolation("red", *config)
		})).To(Succeed())
	})

	It("rejects a plan without source-rotation headroom before changing policy", func() {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		red := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 254, RemoteLocatorPrefixes: rotationLocators(32)}
		blue := *red
		blue.RemoteLocatorPrefixes = rotationLocators(256)
		var before []isolationPolicyRule
		Expect(netnamespace.In(ns, func() error {
			_, err := createVRF("red")
			if err != nil {
				return err
			}
			before, err = listIsolationPolicyRules()
			return err
		})).To(Succeed())
		vpns := []L3VPNParams{{VRF: "red", RoutingIsolation: red}, {VRF: "blue", RoutingIsolation: &blue}}
		Expect(StageSRv6Isolation(path, vpns, net.ParseIP(red.Source))).To(MatchError(ContainSubstring("source rotation")))
		Expect(netnamespace.In(ns, func() error {
			Expect(listIsolationPolicyRules()).To(Equal(before))
			return nil
		})).To(Succeed())
	})

	DescribeTable("retires dedicated guards before staging a shared main lookup", func(rollback bool) {
		ns := newGuardNamespace()
		path := fmt.Sprintf("/proc/self/fd/%d", ns)
		red := &SRv6Isolation{Source: "2001:db8::1", UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
		originalVPNs := []L3VPNParams{{VRF: "red", RoutingIsolation: red}}
		Expect(ReconcileSRv6Isolation(path, originalVPNs)).To(Succeed())
		blue := *red
		blue.UnderlayTable = 254
		next := []L3VPNParams{{VRF: "red"}, {VRF: "blue", RoutingIsolation: &blue}}
		var before []isolationPolicyRule
		Expect(netnamespace.In(ns, func() error { var err error; before, err = listIsolationPolicyRules(); return err })).To(Succeed())
		original := guardRouteDel
		DeferCleanup(func() { guardRouteDel = original })
		guardRouteDel = func(route *netlink.Route) error {
			Expect(listIsolationPolicyRules()).To(Equal(before), "old exceptions must survive until both guards are removed")
			if route.Family == netlink.FAMILY_V6 {
				return unix.EPERM
			}
			return original(route)
		}
		Expect(errors.Is(StageSRv6Isolation(path, next, net.ParseIP(red.Source)), unix.EPERM)).To(BeTrue())
		guardRouteDel = original
		if rollback {
			Expect(StageSRv6Isolation(path, originalVPNs, net.ParseIP(red.Source))).To(Succeed())
			Expect(ReconcileSRv6Isolation(path, originalVPNs)).To(Succeed())
			Expect(netnamespace.In(ns, func() error {
				vrf, err := lookupVRF("red")
				if err != nil {
					return err
				}
				for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
					Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(HaveLen(1))
				}
				return verifySRv6Isolation("red", *red)
			})).To(Succeed())
			return
		}
		Expect(StageSRv6Isolation(path, next, net.ParseIP(red.Source))).To(Succeed())
		Expect(netnamespace.In(ns, func() error {
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(BeEmpty())
			}
			return nil
		})).To(Succeed())
		for range 2 {
			Expect(PrepareSRv6Isolation(path, next, net.ParseIP(red.Source))).To(Succeed())
			Expect((&VNIConfigurator{ns: ns}).SetupL3VPN(context.Background(), next[0])).To(Succeed())
			Expect(FinishSRv6Isolation(path, next, net.ParseIP(red.Source))).To(Succeed())
		}
		Expect(netnamespace.In(ns, func() error {
			expectStagedSourcePairs("blue", blue.RemoteLocatorPrefixes, blue.Source)
			return verifySRv6Isolation("blue", blue)
		})).To(Succeed())
	}, Entry("retry", false), Entry("rollback desired after partial guard removal", true))

	It("keeps a non-VRF link when allocating its replacement table fails", func() {
		ns := newGuardNamespace()
		Expect(netnamespace.In(ns, func() error {
			link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "red", Alias: "original"}}
			Expect(netlink.LinkAdd(link)).To(Succeed())
			Expect(netlink.LinkSetAlias(link, "original")).To(Succeed())
			original := allocateVRFTable
			defer func() { allocateVRFTable = original }()
			allocateVRFTable = func(...uint32) (uint32, error) { return 0, unix.EPERM }
			_, err := createVRF("red", 701)
			Expect(errors.Is(err, unix.EPERM)).To(BeTrue())
			actual, err := netlink.LinkByName("red")
			Expect(err).To(Succeed())
			Expect(actual.Attrs().Index).To(Equal(link.Index))
			Expect(actual.Attrs().Alias).To(Equal("original"))
			calls := 0
			allocateVRFTable = func(excluded ...uint32) (uint32, error) {
				calls++
				Expect(excluded).To(Equal([]uint32{701}))
				return 901, nil
			}
			vrf, err := createVRF("red", 701)
			Expect(err).To(Succeed())
			Expect(vrf.Table).To(Equal(uint32(901)))
			Expect(calls).To(Equal(1))
			return nil
		})).To(Succeed())
	})
})

func expectStagedSourcePairs(vrf string, locators []string, sources ...string) {
	GinkgoHelper()
	current, err := listIsolationPolicyRules()
	Expect(err).To(Succeed())
	expected := map[string]bool{}
	for _, source := range sources {
		for _, iface := range []string{vrf, "lo"} {
			for _, locator := range locators {
				expected[source+"/"+iface+"/"+locator] = true
			}
		}
	}
	pairs := map[string][]netlink.Rule{}
	for _, entry := range current {
		if ownedIsolationPolicyRule(entry) {
			rule := entry.rule
			key := rule.Src.IP.String() + "/" + rule.IifName + "/" + rule.Dst.String()
			Expect(expected).To(HaveKey(key))
			Expect(rule.Protocol).To(Equal(uint8(242)))
			pairs[key] = append(pairs[key], rule)
		}
	}
	Expect(pairs).To(HaveLen(len(expected)))
	for key, rules := range pairs {
		Expect(rules).To(HaveLen(2), key)
		Expect(rules[0].Type).To(Equal(uint8(unix.FR_ACT_TO_TBL)), key)
		Expect(rules[0].Table).To(Equal(254), key)
		Expect(rules[1].Type).To(Equal(uint8(unix.FR_ACT_UNREACHABLE)), key)
		Expect(rules[1].Table).To(BeZero(), key)
		Expect(rules[1].Priority).To(Equal(rules[0].Priority+1), key)
		Expect(rules[1].Priority).To(BeNumerically("<", isolationL3MDevPriority(current)), key)
	}
}
