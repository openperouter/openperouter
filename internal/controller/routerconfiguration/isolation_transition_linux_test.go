// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconcileStagesIsolationBeforeFRR(t *testing.T) {
	host, target, config := isolationTransitionFixture(t)
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	oldSource := *config.Underlays[0].Spec.SRV6.EncapsulationSource
	newSource := "2001:db8:ffff::99"
	config.Underlays[0].Spec.SRV6.EncapsulationSource = &newSource
	failure := errors.New("FRR reload interrupted")
	for range 2 {
		calls := 0
		err := Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
			nil, &KernelDatapathConfigurator{}, func(_ context.Context, data frrConfigData) error {
				calls++
				require.Len(t, data.L3VPNs, 1)
				require.NoError(t, netnamespace.In(target, func() error {
					rules, err := netlink.RuleList(netlink.FAMILY_V6)
					require.NoError(t, err)
					requireOwnedSourcePairs(t, rules, isolationRuleActions(t), []string{oldSource, newSource})
					return nil
				}))
				return failure
			})
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, calls)
	}
}

func TestReconcileRejectsRetainedEVPNTableBeforeFRR(t *testing.T) {
	for _, tc := range []struct {
		name      string
		retained  bool
		wantError string
	}{
		{name: "retained EVPN", retained: true, wantError: "underlayTable 701 belongs to tenant evpn"},
		{name: "attached obsolete owner", wantError: "cannot remove obsolete VRF evpn while interface evpn-port is attached"},
	} {
		t.Run(tc.name, func(t *testing.T) { checkIsolationTableOwner(t, tc.retained, tc.wantError) })
	}
}

func TestKernelConfiguratorRetiresLegacyDespiteIndependentL2Failure(t *testing.T) {
	host, target, config := isolationTransitionFixture(t)
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	config.L3VPNs[0].Spec.RoutingIsolation = nil
	config.Underlays[0].Spec.SRV6.EncapsulationSource = nil
	config.L2VNIs = []v1alpha1.L2VNI{{ObjectMeta: metav1.ObjectMeta{Name: "blue"},
		Spec: v1alpha1.L2VNISpec{VNI: 300, VXLanPort: new(int32(4789)),
			HostMaster: &v1alpha1.HostMaster{Type: v1alpha1.LinuxBridge,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{Lifecycle: v1alpha1.BridgeLifecycleExternal, Name: new("missing")}}}}}
	for range 2 {
		require.NoError(t, netnamespace.In(host, func() error {
			err := (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
			require.Equal(t, []v1alpha1.FailedResource{{Kind: openpeerrors.KindL2VNI, Name: "blue",
				Reason:  v1alpha1.FailedResourceReasonOverlayAttachmentFailed,
				Message: "SetupL2VNI: failed to get host master for VRF : could not find host master missing: Link not found",
			}}, openpeerrors.CollectFailures(err))
			require.EqualError(t, err, "L2VNI/blue: SetupL2VNI: failed to get host master for VRF : could not find host master missing: Link not found")
			return nil
		}))
		require.NoError(t, netnamespace.In(target, func() error {
			rules, err := netlink.RuleList(netlink.FAMILY_V6)
			require.NoError(t, err)
			for _, rule := range rules {
				require.NotEqual(t, uint8(242), rule.Protocol, "successful legacy transition must retire its rules")
			}
			require.True(t, configuratorTunnelSource(t).Equal(net.ParseIP("2001:db8:ffff::1")))
			vrf, err := netlink.LinkByName("red")
			require.NoError(t, err)
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				routes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: int(vrf.(*netlink.Vrf).Table), Protocol: 242},
					netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
				require.NoError(t, err)
				require.Empty(t, routes)
			}
			return nil
		}))
	}
	config.L2VNIs = nil
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
}

func TestKernelConfiguratorPassthroughCleanupAfterVRFRemovalFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		orphanOnly bool
	}{
		{name: "orphan guards", orphanOnly: true},
		{name: "live VRF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host, target, config := isolationTransitionFixture(t)
			original := removeNonConfiguredVRFs
			t.Cleanup(func() { removeNonConfiguredVRFs = original })
			failure := errors.New("VRF cleanup interrupted")
			removeNonConfiguredVRFs = func(string, map[string]bool) error {
				return &hostnetwork.VRFRemovalError{OnlyOrphanGuards: tc.orphanOnly, Err: failure}
			}
			require.NoError(t, netnamespace.In(host, func() error {
				link := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: hostnetwork.PassthroughNames.HostSide},
					PeerName: hostnetwork.PassthroughNames.NamespaceSide, PeerNamespace: netlink.NsFd(target)}
				return netlink.LinkAdd(link)
			}))
			checkLinks := func(wantRemoved bool) {
				t.Helper()
				for _, side := range []struct {
					ns   netns.NsHandle
					name string
				}{
					{ns: host, name: hostnetwork.PassthroughNames.HostSide},
					{ns: target, name: hostnetwork.PassthroughNames.NamespaceSide},
				} {
					require.NoError(t, netnamespace.In(side.ns, func() error {
						_, err := netlink.LinkByName(side.name)
						if wantRemoved {
							var missing netlink.LinkNotFoundError
							require.ErrorAs(t, err, &missing)
							return nil
						}
						return err
					}))
				}
			}
			for range 2 {
				err := netnamespace.In(host, func() error {
					return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
				})
				require.ErrorIs(t, err, failure)
				checkLinks(tc.orphanOnly)
			}
			removeNonConfiguredVRFs = original
			require.NoError(t, netnamespace.In(host, func() error {
				return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
			}))
			checkLinks(true)
		})
	}
}

func TestKernelConfiguratorCleansOrphanGuardsWithoutUnderlay(t *testing.T) {
	target := configuratorTestNamespace(t)
	path := fmt.Sprintf("/proc/self/fd/%d", target)
	require.NoError(t, netnamespace.In(target, func() error {
		foreign := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "foreign"}, Table: 902}
		if err := netlink.LinkAdd(foreign); err != nil {
			return err
		}
		for _, table := range []int{901, 902} {
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				guard := configuratorGuardRoute(table, family)
				if err := netlink.RouteAdd(&guard); err != nil {
					return err
				}
			}
		}
		foreignRoute := configuratorGuardRoute(901, netlink.FAMILY_V4)
		foreignRoute.Type, foreignRoute.Priority, foreignRoute.Protocol = unix.RTN_BLACKHOLE, 100, 99
		return netlink.RouteAdd(&foreignRoute)
	}))
	for range 2 {
		require.NoError(t, (&KernelDatapathConfigurator{}).Configure(context.Background(), interfacesConfiguration{targetNamespace: path}))
	}
	require.NoError(t, netnamespace.In(target, func() error {
		for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
			routes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: 901}, netlink.RT_FILTER_TABLE)
			require.NoError(t, err)
			wantForeignRoutes := 0
			if family == netlink.FAMILY_V4 {
				wantForeignRoutes = 1
			}
			require.Len(t, routes, wantForeignRoutes)
			for _, route := range routes {
				require.Equal(t, netlink.RouteProtocol(99), route.Protocol)
			}
			routes, err = netlink.RouteListFiltered(family,
				&netlink.Route{Table: 902, Protocol: 242}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
			require.NoError(t, err)
			require.Len(t, routes, 1, "live foreign VRF must keep its guards")
		}
		_, err := netlink.LinkByName("foreign")
		return err
	}))
}

func TestReconcileRejectsIncompleteLegacyLocatorBeforeFRR(t *testing.T) {
	host, target, config := isolationTransitionFixture(t)
	vrfName := config.L3VPNs[0].Spec.VRF
	config.Underlays[0].Spec.SRV6.EncapsulationSource = new("2001:db8:ffff::99")
	config.L3VPNs[0].Spec.RoutingIsolation.RemoteLocatorPrefixes = []string{"fc00:20::/48", "fc00:30::/48"}
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	var before []netlink.Rule
	var beforeActions map[int]uint8
	var source net.IP
	var missingTerminal netlink.Rule
	require.NoError(t, netnamespace.In(target, func() error {
		rules, err := netlink.RuleList(netlink.FAMILY_V6)
		require.NoError(t, err)
		removed := false
		for _, rule := range rules {
			if rule.Protocol == 242 && rule.IifName == vrfName && rule.Table == 0 && rule.Dst.String() == "fc00:30::/48" {
				rule.Type = unix.FR_ACT_UNREACHABLE
				require.NoError(t, netlink.RuleDel(&rule))
				missingTerminal = rule
				removed = true
			}
		}
		require.True(t, removed)
		before, err = netlink.RuleList(netlink.FAMILY_V6)
		beforeActions = isolationRuleActions(t)
		source = configuratorTunnelSource(t)
		return err
	}))
	config.L3VPNs[0].Spec.RoutingIsolation = nil
	config.Underlays[0].Spec.SRV6.EncapsulationSource = nil
	for range 2 {
		calls := 0
		err := netnamespace.In(host, func() error {
			return Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
				nil, &KernelDatapathConfigurator{}, func(context.Context, frrConfigData) error { calls++; return nil })
		})
		require.ErrorContains(t, err, "guarded legacy VPN red lacks a complete SRv6 exception pair for locator fc00:30::/48")
		require.Zero(t, calls)
		require.NoError(t, netnamespace.In(target, func() error {
			rules, err := netlink.RuleList(netlink.FAMILY_V6)
			require.NoError(t, err)
			require.Equal(t, before, rules)
			require.Equal(t, beforeActions, isolationRuleActions(t))
			require.Equal(t, source, configuratorTunnelSource(t))
			return nil
		}))
	}
	require.NoError(t, netnamespace.In(target, func() error { return netlink.RuleAdd(&missingTerminal) }))
	require.NoError(t, netnamespace.In(host, func() error {
		return Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
			nil, &KernelDatapathConfigurator{}, func(context.Context, frrConfigData) error { return nil })
	}))
	require.NoError(t, netnamespace.In(target, func() error {
		rules, err := netlink.RuleList(netlink.FAMILY_V6)
		require.NoError(t, err)
		for _, rule := range rules {
			require.NotEqual(t, uint8(242), rule.Protocol)
		}
		return nil
	}))
}

func TestReconcileRejectsInsufficientRotationCapacityBeforeFRR(t *testing.T) {
	const blueName = "blue"
	_, target, config := isolationTransitionFixture(t)
	blue := *config.L3VPNs[0].DeepCopy()
	blue.Name, blue.Spec.VRF, blue.Spec.RDAssignedNumber = blueName, blueName, 202
	config.L3VPNs = append(config.L3VPNs, blue)
	for i := range config.L3VPNs {
		config.L3VPNs[i].Spec.RoutingIsolation.RemoteLocatorPrefixes = nil
		for j := range 64 {
			config.L3VPNs[i].Spec.RoutingIsolation.RemoteLocatorPrefixes = append(
				config.L3VPNs[i].Spec.RoutingIsolation.RemoteLocatorPrefixes, fmt.Sprintf("fc00:%x::/48", 32+i*256+j))
		}
	}
	var before []netlink.Rule
	var beforeActions map[int]uint8
	require.NoError(t, netnamespace.In(target, func() error {
		var err error
		before, err = netlink.RuleList(netlink.FAMILY_V6)
		beforeActions = isolationRuleActions(t)
		return err
	}))
	calls := 0
	err := Reconcile(context.Background(), config.APIConfigData, 0, "warning", "", config.targetNamespace,
		nil, &KernelDatapathConfigurator{}, func(context.Context, frrConfigData) error { calls++; return nil })
	require.ErrorContains(t, err, "source rotation")
	require.Zero(t, calls)
	require.NoError(t, netnamespace.In(target, func() error {
		after, err := netlink.RuleList(netlink.FAMILY_V6)
		require.NoError(t, err)
		require.Equal(t, before, after)
		require.Equal(t, beforeActions, isolationRuleActions(t))
		return nil
	}))
}

func TestConfiguratorRemovesTunnelLoopbackWithoutUnderlay(t *testing.T) {
	host, target, config := isolationTransitionFixture(t)
	require.NoError(t, netnamespace.In(target, func() error {
		loopback := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: hostnetwork.TunnelLoopback}}
		require.NoError(t, netlink.LinkAdd(loopback))
		address, err := netlink.ParseAddr("2001:db8:ffff::1/128")
		require.NoError(t, err)
		return netlink.AddrAdd(loopback, address)
	}))
	originalCleanup := cleanupOrphanGuards
	t.Cleanup(func() { cleanupOrphanGuards = originalCleanup })
	failure := errors.New("orphan guard dump interrupted")
	cleanupOrphanGuards = func(string) error { return failure }
	// A failed loopback deletion after the last CNI DEL leaves this inventory.
	require.Empty(t, mustUnderlayInterfaces(t, config.targetNamespace))
	for range 2 {
		err := netnamespace.In(host, func() error {
			return (&KernelDatapathConfigurator{}).Configure(context.Background(), interfacesConfiguration{
				targetNamespace: config.targetNamespace,
			})
		})
		require.ErrorIs(t, err, failure)
		require.NoError(t, netnamespace.In(target, func() error {
			_, err := netlink.LinkByName(hostnetwork.TunnelLoopback)
			var missing netlink.LinkNotFoundError
			require.ErrorAs(t, err, &missing)
			return nil
		}))
	}
}

func TestRestoreUnderlayAfterLiveVRFFailure(t *testing.T) {
	host, target, config := isolationTransitionFixture(t)
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	original := removeAllVRFs
	t.Cleanup(func() { removeAllVRFs = original })
	failure := errors.New("VRF link deletion failed")
	calls := 0
	removeAllVRFs = func(string) error { calls++; return &hostnetwork.VRFRemovalError{Err: failure} }
	empty := interfacesConfiguration{targetNamespace: config.targetNamespace}
	for attempt := range 2 {
		require.NoError(t, netnamespace.In(host, func() error {
			require.ErrorIs(t, (&KernelDatapathConfigurator{}).Configure(context.Background(), empty), failure)
			_, err := netlink.LinkByName("uplink")
			return err
		}))
		require.Equal(t, attempt+1, calls)
		require.Empty(t, mustUnderlayInterfaces(t, config.targetNamespace))
		require.NoError(t, netnamespace.In(target, func() error { _, err := netlink.LinkByName("red"); return err }))
	}
	removeAllVRFs = original
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), empty)
	}))
	require.NoError(t, netnamespace.In(target, func() error {
		_, err := netlink.LinkByName("red")
		var missing netlink.LinkNotFoundError
		require.ErrorAs(t, err, &missing)
		return nil
	}))
}

func TestRestoreUnderlayAfterOrphanGuardFailure(t *testing.T) {
	host, target, config := isolationTransitionFixture(t)
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	original := removeAllVRFs
	t.Cleanup(func() { removeAllVRFs = original })
	failure := errors.New("orphan guard deletion failed")
	removeAllVRFs = func(path string) error {
		if err := original(path); err != nil {
			return err
		}
		err := netnamespace.In(target, func() error {
			return netlink.RouteAdd(&netlink.Route{Family: netlink.FAMILY_V4, Table: 701,
				Dst:  &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
				Type: unix.RTN_UNREACHABLE, Protocol: 242, Priority: 4278198272})
		})
		if err != nil {
			return err
		}
		return &hostnetwork.VRFRemovalError{OnlyOrphanGuards: true, Err: failure}
	}
	empty := interfacesConfiguration{targetNamespace: config.targetNamespace}
	require.NoError(t, netnamespace.In(host, func() error {
		require.ErrorIs(t, (&KernelDatapathConfigurator{}).Configure(context.Background(), empty), failure)
		_, err := netlink.LinkByName("uplink")
		require.NoError(t, err, "orphan guard failure must not strand the uplink")
		removeAllVRFs = original
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), empty)
	}))
	require.NoError(t, netnamespace.In(target, func() error {
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4,
			&netlink.Route{Table: 701, Protocol: 242}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
		require.NoError(t, err)
		require.Empty(t, routes, "reconciliation without underlay must retry orphan cleanup")
		return nil
	}))
}

func TestReconcileReusesObsoleteManagedAttachedTable(t *testing.T) {
	checkObsoleteManagedAttachedTable(t, false)
}

func TestControllerFRRReusesObsoleteManagedAttachedTable(t *testing.T) {
	requireSourceTransitionFRR(t)
	resetFRRConfigCache(t)
	checkObsoleteManagedAttachedTable(t, true)
}

func checkObsoleteManagedAttachedTable(t *testing.T, realFRR bool) {
	t.Helper()
	host, target, config := isolationTransitionFixture(t)
	var updater frr.ConfigUpdater
	applyFRR := func(context.Context, frrConfigData) error { return nil }
	if realFRR {
		updater = sourceTransitionFRR(t, target)
		applyFRR = configureFRR
	}

	oldVRF := config.L3VPNs[0].Spec.VRF
	config.L3VPNs[0].Spec.HostSession = &v1alpha1.HostSession{
		ASN: 65001, HostASN: new(int64(65002)), LocalCIDRs: []string{"192.0.2.0/30"},
	}
	requireReconcileEventually(t, func() error {
		return netnamespace.In(host, func() error {
			return Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
				updater, &KernelDatapathConfigurator{}, applyFRR)
		})
	})

	var table uint32
	require.NoError(t, netnamespace.In(target, func() error {
		link, err := netlink.LinkByName(oldVRF)
		if err != nil {
			return err
		}
		table = link.(*netlink.Vrf).Table
		return nil
	}))
	config.L3VPNs[0].Name = "blue"
	config.L3VPNs[0].Spec.VRF = "blue"
	config.L3VPNs[0].Spec.RDAssignedNumber = 202
	config.L3VPNs[0].Spec.HostSession = nil
	config.L3VPNs[0].Spec.RoutingIsolation.UnderlayTable = int64(table)
	config.Underlays[0].Spec.SRV6.EncapsulationSource = new(transitionNewSource)
	rejected := errors.New("FRR rejected table reuse")
	for _, applyBeforeFailure := range []bool{false, true} {
		require.NoError(t, netnamespace.In(host, func() error {
			err := Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
				updater, &KernelDatapathConfigurator{}, func(ctx context.Context, data frrConfigData) error {
					require.Empty(t, data.L3VPNs, "replacement cannot activate before the table is freed")
					require.NotNil(t, data.srv6EncapsulationSource)
					require.Equal(t, transitionLegacySource, *data.srv6EncapsulationSource)
					if applyBeforeFailure {
						require.NoError(t, applyFRR(ctx, data))
					}
					return rejected
				})
			require.ErrorIs(t, err, rejected)
			_, err = netlink.LinkByName("host-s-201")
			require.NoError(t, err, "FRR rejection must preserve the old host attachment")
			return nil
		}))
		require.NoError(t, netnamespace.In(target, func() error {
			link, err := netlink.LinkByName(oldVRF)
			require.NoError(t, err, "FRR rejection must preserve the old VRF")
			require.Equal(t, table, link.(*netlink.Vrf).Table)
			require.True(t, configuratorTunnelSource(t).Equal(net.ParseIP(transitionLegacySource)))
			rules, err := netlink.RuleList(netlink.FAMILY_V6)
			require.NoError(t, err)
			requireOwnedSourcePairs(t, rules, isolationRuleActions(t), []string{transitionLegacySource})

			port, err := netlink.LinkByName("pe-s-201")
			require.NoError(t, err)
			require.Equal(t, link.Attrs().Index, port.Attrs().MasterIndex)
			return nil
		}))
	}
	for attempt := range 2 {
		calls := 0
		require.NoError(t, netnamespace.In(host, func() error {
			err := Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
				updater, &KernelDatapathConfigurator{}, func(ctx context.Context, data frrConfigData) error {
					calls++
					return applyFRR(ctx, data)
				})
			if err != nil {
				return err
			}
			_, err = netlink.LinkByName("host-s-201")
			var missing netlink.LinkNotFoundError
			require.ErrorAs(t, err, &missing)
			return nil
		}))
		require.Equal(t, 2-attempt, calls)
		require.NoError(t, netnamespace.In(target, func() error {
			_, err := netlink.LinkByName(oldVRF)
			var missing netlink.LinkNotFoundError
			require.ErrorAs(t, err, &missing)
			blue, err := netlink.LinkByName("blue")
			require.NoError(t, err)
			require.NotEqual(t, table, blue.(*netlink.Vrf).Table)
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				guards, err := netlink.RouteListFiltered(family, &netlink.Route{Table: int(table), Protocol: 242},
					netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
				require.NoError(t, err)
				require.Empty(t, guards, "the reused underlay table must not retain tenant guards")
				guards, err = netlink.RouteListFiltered(family, &netlink.Route{Table: int(blue.(*netlink.Vrf).Table), Protocol: 242},
					netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
				require.NoError(t, err)
				require.Len(t, guards, 1)
				requireConfiguratorDefaultGuard(t, guards[0])
			}
			rules, err := netlink.RuleList(netlink.FAMILY_V6)
			require.NoError(t, err)
			require.True(t, configuratorTunnelSource(t).Equal(net.ParseIP(transitionNewSource)))
			actions := isolationRuleActions(t)
			pairs := map[string][]netlink.Rule{}
			for _, rule := range rules {
				if rule.Protocol == 242 {
					require.NotNil(t, rule.Src)
					require.Equal(t, transitionNewSource+"/128", rule.Src.String())
					require.Equal(t, "fc00:20::/48", rule.Dst.String())
					pairs[rule.IifName] = append(pairs[rule.IifName], rule)
				}
			}
			require.Len(t, pairs, 2)
			for _, iface := range []string{"blue", "lo"} {
				require.Len(t, pairs[iface], 2)
				lookup, terminal := pairs[iface][0], pairs[iface][1]
				require.Equal(t, int(table), lookup.Table)
				require.Zero(t, terminal.Table)
				require.Equal(t, uint8(unix.FR_ACT_TO_TBL), actions[lookup.Priority])
				require.Equal(t, uint8(unix.FR_ACT_UNREACHABLE), actions[terminal.Priority])
				require.Equal(t, lookup.Priority+1, terminal.Priority)
			}
			return nil
		}))
	}
}

func isolationTransitionFixture(t *testing.T) (netns.NsHandle, netns.NsHandle, interfacesConfiguration) {
	t.Helper()
	host, target := configuratorTestNamespace(t), configuratorTestNamespace(t)
	require.NoError(t, netnamespace.In(host, func() error {
		port := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "uplink"}}
		if err := netlink.LinkAdd(port); err != nil {
			return err
		}
		if err := netlink.LinkSetUp(port); err != nil {
			return err
		}
		address, err := netlink.ParseAddr("198.18.0.1/24")
		if err != nil {
			return err
		}
		return netlink.AddrAdd(port, address)
	}))
	source := "2001:db8:ffff::1"
	config := interfacesConfiguration{targetNamespace: fmt.Sprintf("/proc/self/fd/%d", target),
		APIConfigData: conversion.APIConfigData{
			Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{
				ASN: 65001, ISIS: &v1alpha1.ISISConfig{BaseNet: "49.0001.0002.0003.0004.00"},
				Neighbors: []v1alpha1.Neighbor{{ASN: new(int64(65002)), Address: new("198.18.0.2")}},
				Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice",
					NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "uplink"}}},
				TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"198.18.1.1/32", source + "/128"}},
				SRV6: &v1alpha1.SRV6Config{EncapsulationSource: &source,
					Locator: v1alpha1.SRV6Locator{BasePrefix: "fc00:10:1::/48", Format: "usid-f3216"}},
			}}},
			L3VPNs: []v1alpha1.L3VPN{{ObjectMeta: metav1.ObjectMeta{Name: "red"},
				Spec: v1alpha1.L3VPNSpec{VRF: "red", RDAssignedNumber: 201,
					ImportRTs: []v1alpha1.RouteTarget{"65001:201"},
					RoutingIsolation: &v1alpha1.SRV6RoutingIsolation{UnderlayTable: 254,
						RemoteLocatorPrefixes: []string{"fc00:20::/48"}}}}},
		}}
	return host, target, config
}

func configuratorTunnelSource(t *testing.T) net.IP {
	t.Helper()
	family, err := netlink.GenlFamilyGet("SEG6")
	require.NoError(t, err)
	req := nl.NewNetlinkRequest(int(family.ID), unix.NLM_F_REQUEST)
	req.AddRawData([]byte{4, 1, 0, 0})
	messages, err := req.Execute(unix.NETLINK_GENERIC, family.ID)
	require.NoError(t, err)
	for _, message := range messages {
		require.GreaterOrEqual(t, len(message), 4)
		attrs, err := nl.ParseRouteAttr(message[4:])
		require.NoError(t, err)
		for _, attr := range attrs {
			if attr.Attr.Type == 1 && len(attr.Value) == net.IPv6len {
				return net.IP(attr.Value)
			}
		}
	}
	t.Fatal("SEG6 source missing from kernel response")
	return nil
}

func configuratorGuardRoute(table, family int) netlink.Route {
	dst := &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)}
	if family == netlink.FAMILY_V6 {
		dst = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}
	}
	return netlink.Route{Table: table, Family: family, Dst: dst, Type: unix.RTN_UNREACHABLE,
		Priority: 4278198272, Protocol: 242}
}

func checkIsolationTableOwner(t *testing.T, retained bool, wantError string) {
	t.Helper()
	_, target, config := isolationTransitionFixture(t)
	config.L3VNIs = []v1alpha1.L3VNI{{ObjectMeta: metav1.ObjectMeta{Name: "evpn"},
		Spec: v1alpha1.L3VNISpec{VRF: "evpn", VNI: 100, VXLanPort: new(int32(4789)),
			ImportRTs: []v1alpha1.RouteTarget{"65001:100"}}}}
	var beforeRules []netlink.Rule
	var beforeActions map[int]uint8
	var beforeSource net.IP
	var beforeIndex int
	require.NoError(t, netnamespace.In(target, func() error {
		vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "evpn"}, Table: 701}
		if err := netlink.LinkAdd(vrf); err != nil {
			return err
		}
		if err := netlink.LinkSetAlias(vrf, "openperouter:tenant-vrf"); err != nil {
			return err
		}
		if err := netlink.LinkSetUp(vrf); err != nil {
			return err
		}
		port := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "evpn-port", MasterIndex: vrf.Index}}
		if err := netlink.LinkAdd(port); err != nil {
			return err
		}
		if err := netlink.LinkSetUp(port); err != nil {
			return err
		}
		for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
			guard := configuratorGuardRoute(701, family)
			if err := netlink.RouteAdd(&guard); err != nil {
				return err
			}
		}
		beforeIndex = vrf.Index
		beforeSource = configuratorTunnelSource(t)
		var err error
		beforeRules, err = netlink.RuleList(netlink.FAMILY_V6)
		beforeActions = isolationRuleActions(t)
		return err
	}))
	if !retained {
		config.L3VNIs = nil
	}
	config.L3VPNs[0].Spec.RoutingIsolation.UnderlayTable = 701
	calls := 0
	err := Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
		nil, &KernelDatapathConfigurator{}, func(context.Context, frrConfigData) error { calls++; return nil })
	require.ErrorContains(t, err, wantError)
	if retained {
		var conflict *hostnetwork.VRFTableOwnershipError
		require.ErrorAs(t, err, &conflict)
		require.Equal(t, "evpn", conflict.VRF)
	}
	require.Zero(t, calls)
	require.NoError(t, netnamespace.In(target, func() error {
		rules, err := netlink.RuleList(netlink.FAMILY_V6)
		require.NoError(t, err)
		require.Equal(t, beforeRules, rules)
		require.Equal(t, beforeActions, isolationRuleActions(t))
		require.Equal(t, beforeSource, configuratorTunnelSource(t))
		vrf, err := netlink.LinkByName("evpn")
		require.NoError(t, err)
		require.Equal(t, beforeIndex, vrf.Attrs().Index)
		require.NotZero(t, vrf.Attrs().Flags&net.FlagUp)
		port, err := netlink.LinkByName("evpn-port")
		require.NoError(t, err)
		require.Equal(t, beforeIndex, port.Attrs().MasterIndex)
		require.NotZero(t, port.Attrs().Flags&net.FlagUp)
		for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
			routes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: 701, Protocol: 242},
				netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
			require.NoError(t, err)
			require.Len(t, routes, 1)
			requireConfiguratorDefaultGuard(t, routes[0])
		}
		return nil
	}))
}

func mustUnderlayInterfaces(t *testing.T, path string) []hostnetwork.UnderlayInterface {
	t.Helper()
	interfaces, err := hostnetwork.UnderlayInterfaces(path)
	require.NoError(t, err)
	return interfaces
}
