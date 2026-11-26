// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package routerconfiguration

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The privileged test runner starts binaries from the checkout root, while the
// package's existing fixtures and envtest assets use go test's package cwd.
func TestMain(m *testing.M) {
	if _, err := os.Stat("internal/controller/routerconfiguration/host_config.go"); err == nil {
		if err := os.Chdir("internal/controller/routerconfiguration"); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

func TestKernelConfiguratorFirstEVPNVRF(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("IPv6_disabled_%t", disabled), func(t *testing.T) {
			host, target := configuratorTestNamespace(t), configuratorTestNamespace(t)
			var config interfacesConfiguration
			path := fmt.Sprintf("/proc/self/fd/%d", target)
			require.NoError(t, netnamespace.In(target, func() error {
				links, err := netlink.LinkList()
				require.NoError(t, err)
				for _, link := range links {
					require.NotEqual(t, "vrf", link.Type())
				}
				if disabled {
					return sysctl.Ensure(sysctl.Sysctl{Path: "net/ipv6/conf/all/disable_ipv6", Value: "1"}, sysctl.Sysctl{Path: "net/ipv6/conf/default/disable_ipv6", Value: "1"})
				}
				return nil
			}))
			require.NoError(t, netnamespace.In(host, func() error {
				port := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "uplink"}}
				require.NoError(t, netlink.LinkAdd(port))
				require.NoError(t, netlink.LinkSetIP6AddrGenMode(port, 1))
				require.NoError(t, netlink.LinkSetUp(port))
				address, err := netlink.ParseAddr("198.18.0.1/24")
				require.NoError(t, err)
				require.NoError(t, netlink.AddrAdd(port, address))
				config = interfacesConfiguration{targetNamespace: path, APIConfigData: conversion.APIConfigData{
					Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{ASN: 65001,
						Interfaces:     []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "uplink"}}},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"198.18.1.1/32"}},
					}}},
					L3VNIs: []v1alpha1.L3VNI{{ObjectMeta: metav1.ObjectMeta{Name: "red"}, Spec: v1alpha1.L3VNISpec{VRF: "red", VNI: 100, VXLanPort: new(int32(4789))}}},
				}}
				configurator := &KernelDatapathConfigurator{}
				for range 2 {
					require.NoError(t, configurator.Configure(context.Background(), config))
				}
				return nil
			}))
			checkConfiguratorGuards(t, target, disabled)
			if !disabled {
				t.Run("SRv6 guard conflict preserves red and configures blue", func(t *testing.T) {
					checkKernelConfiguratorGuardConflict(t, host, target, config)
				})
			}
		})
	}
}

func TestKernelConfiguratorKeepsLegacyTransitionOnSetupFailure(t *testing.T) {
	host, target := configuratorTestNamespace(t), configuratorTestNamespace(t)
	path := fmt.Sprintf("/proc/self/fd/%d", target)
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
	isolation := &v1alpha1.SRV6RoutingIsolation{UnderlayTable: 254,
		RemoteLocatorPrefixes: []string{"fc00:20::/48"}}
	config := interfacesConfiguration{targetNamespace: path, APIConfigData: conversion.APIConfigData{
		Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{
			ASN: 65001, ISIS: &v1alpha1.ISISConfig{BaseNet: "49.0001.0002.0003.0004.00"},
			Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice",
				NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "uplink"}}},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"198.18.1.1/32", source + "/128"}},
			SRV6: &v1alpha1.SRV6Config{EncapsulationSource: &source,
				Locator: v1alpha1.SRV6Locator{BasePrefix: "fc00:10::/32", Format: "usid-f3216"}},
		}}},
		L3VPNs: []v1alpha1.L3VPN{{ObjectMeta: metav1.ObjectMeta{Name: "blue"},
			Spec: v1alpha1.L3VPNSpec{VRF: "blue", RDAssignedNumber: 201, RoutingIsolation: isolation}}},
	}}
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	check := func(guarded bool) {
		t.Helper()
		require.NoError(t, netnamespace.In(target, func() error {
			link, err := netlink.LinkByName("blue")
			require.NoError(t, err)
			vrf := link.(*netlink.Vrf)
			routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4,
				&netlink.Route{Table: int(vrf.Table), Protocol: 242},
				netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
			require.NoError(t, err)
			if guarded {
				require.Len(t, routes, 1)
				requireConfiguratorDefaultGuard(t, routes[0])
			} else {
				require.Empty(t, routes)
			}
			rules, err := netlink.RuleList(netlink.FAMILY_V6)
			require.NoError(t, err)
			owned := 0
			for _, rule := range rules {
				if rule.Protocol == 242 {
					owned++
				}
			}
			if guarded {
				require.Equal(t, 4, owned)
			} else {
				require.Zero(t, owned)
			}
			return nil
		}))
	}
	check(true)
	config.L3VPNs[0].Spec.RoutingIsolation = nil
	config.Underlays[0].Spec.SRV6.EncapsulationSource = nil
	original := newVNIConfigurator
	t.Cleanup(func() { newVNIConfigurator = original })
	newVNIConfigurator = func(netns.NsHandle, *hostnetwork.OVSClient) (*hostnetwork.VNIConfigurator, error) {
		return nil, fmt.Errorf("injected configurator failure")
	}
	require.NoError(t, netnamespace.In(host, func() error {
		returnErr := (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
		require.ErrorContains(t, returnErr, "injected configurator failure")
		return nil
	}))
	check(true)
	newVNIConfigurator = original
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	check(false)
	config.L3VPNs[0].Spec.RoutingIsolation = isolation
	config.Underlays[0].Spec.SRV6.EncapsulationSource = &source
	require.NoError(t, netnamespace.In(host, func() error {
		return (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
	}))
	check(true)
}

func checkConfiguratorGuards(t *testing.T, target netns.NsHandle, disabled bool) {
	t.Helper()
	require.NoError(t, netnamespace.In(target, func() error {
		link, err := netlink.LinkByName("red")
		require.NoError(t, err)
		vrf, ok := link.(*netlink.Vrf)
		require.True(t, ok)
		require.NotContains(t, []uint32{0, 253, 254, 255}, vrf.Table)
		for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
			routes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: int(vrf.Table), Type: unix.RTN_UNREACHABLE, Priority: 4278198272}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_TYPE|netlink.RT_FILTER_PRIORITY)
			require.NoError(t, err)
			expected := 1
			if disabled && family == netlink.FAMILY_V6 {
				expected = 0
			}
			require.Len(t, routes, expected)
			for _, route := range routes {
				requireConfiguratorDefaultGuard(t, route)
			}
		}
		return nil
	}))
}

func checkKernelConfiguratorGuardConflict(t *testing.T, host, target netns.NsHandle, config interfacesConfiguration) {
	t.Helper()

	var redIndex, bridgeIndex int
	require.NoError(t, netnamespace.In(target, func() error {
		red, err := netlink.LinkByName("red")
		require.NoError(t, err)
		bridge, err := netlink.LinkByName(hostnetwork.BridgeName(100))
		require.NoError(t, err)
		redIndex, bridgeIndex = red.Attrs().Index, bridge.Attrs().Index
		table := int(red.(*netlink.Vrf).Table)
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: table, Protocol: 242}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
		require.NoError(t, err)
		require.Len(t, routes, 1)
		require.NoError(t, netlink.RouteDel(&routes[0]))
		return netlink.RouteAdd(&netlink.Route{Family: netlink.FAMILY_V4, Table: table, Dst: routes[0].Dst, Type: unix.RTN_BLACKHOLE, Priority: 4278198272, Protocol: 99})
	}))
	config.Underlays[0].Spec.ISIS = &v1alpha1.ISISConfig{BaseNet: "49.0001.0002.0003.0004.00"}
	config.Underlays[0].Spec.SRV6 = &v1alpha1.SRV6Config{EncapsulationSource: new("2001:db8:ffff::1"), Locator: v1alpha1.SRV6Locator{BasePrefix: "fc00:10::/32", Format: "usid-f3216"}}
	config.Underlays[0].Spec.TunnelEndpoint.CIDRs = []string{"198.18.1.1/32", "2001:db8:ffff::1/128"}
	for i, name := range []string{"red", "blue"} {
		config.L3VPNs = append(config.L3VPNs, v1alpha1.L3VPN{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: v1alpha1.L3VPNSpec{VRF: name, RDAssignedNumber: int32(200 + i), ImportRTs: []v1alpha1.RouteTarget{"65001:100"}, RoutingIsolation: &v1alpha1.SRV6RoutingIsolation{UnderlayTable: 701, RemoteLocatorPrefixes: []string{"fc00:20::/48"}}}})
	}
	config.Underlays[0].Spec.Neighbors = []v1alpha1.Neighbor{{ASN: new(int64(65002)), Address: new("198.18.0.2")}}
	require.NoError(t, netnamespace.In(target, func() error {
		foreign := netlink.NewRule()
		foreign.Family, foreign.Priority, foreign.Protocol = netlink.FAMILY_V6, 1, unix.RTPROT_STATIC
		foreign.Table = unix.RT_TABLE_MAIN
		return netlink.RuleAdd(foreign)
	}))
	frrCalls := 0
	err := Reconcile(context.Background(), config.APIConfigData, 0, "info", "", config.targetNamespace,
		nil, &KernelDatapathConfigurator{}, func(context.Context, frrConfigData) error {
			frrCalls++
			return nil
		})
	require.ErrorContains(t, err, "foreign rule conflicts with isolation priority 1")
	require.Zero(t, frrCalls, "FRR must not change the source before a known priority conflict")
	require.NoError(t, netnamespace.In(target, func() error {
		foreign := netlink.NewRule()
		foreign.Family, foreign.Priority, foreign.Protocol = netlink.FAMILY_V6, 1, unix.RTPROT_STATIC
		foreign.Table = unix.RT_TABLE_MAIN
		return netlink.RuleDel(foreign)
	}))
	require.NoError(t, netnamespace.In(host, func() error {
		err := (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
		failures := openpeerrors.CollectFailures(err)
		require.NotEmpty(t, failures, "expected per-VRF guard error, got %v", err)
		for _, failure := range failures {
			require.Equal(t, "red", failure.Name)
		}
		return nil
	}))
	require.NoError(t, netnamespace.In(target, func() error {
		red, err := netlink.LinkByName("red")
		require.NoError(t, err)
		bridge, err := netlink.LinkByName(hostnetwork.BridgeName(100))
		require.NoError(t, err)
		require.Equal(t, redIndex, red.Attrs().Index)
		require.Equal(t, bridgeIndex, bridge.Attrs().Index)
		blue, err := netlink.LinkByName("blue")
		require.NoError(t, err)
		require.NotZero(t, blue.Attrs().Flags&net.FlagUp)
		for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
			routes, err := netlink.RouteListFiltered(family,
				&netlink.Route{Table: int(blue.(*netlink.Vrf).Table), Protocol: 242},
				netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
			require.NoError(t, err)
			require.Len(t, routes, 1)
			requireConfiguratorDefaultGuard(t, routes[0])
			if family == netlink.FAMILY_V4 {
				require.NoError(t, netlink.RouteDel(&routes[0]))
			}
		}
		return nil
	}))
	require.NoError(t, netnamespace.In(target, func() error {
		obsolete := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "obsolete"}, Table: 702}
		require.NoError(t, netlink.LinkAdd(obsolete))
		return netlink.LinkSetAlias(obsolete, "openperouter:tenant-vrf")
	}))
	for i := range config.L3VPNs {
		config.L3VPNs[i].Spec.RoutingIsolation.UnderlayTable = 702
	}
	require.NoError(t, netnamespace.In(host, func() error {
		err := (&KernelDatapathConfigurator{}).Configure(context.Background(), config)
		failures := openpeerrors.CollectFailures(err)
		require.NotEmpty(t, failures, "expected red guard conflict after obsolete table cleanup: %v", err)
		for _, failure := range failures {
			require.Equal(t, "red", failure.Name)
		}
		return nil
	}))
	require.NoError(t, netnamespace.In(target, func() error {
		_, err := netlink.LinkByName("obsolete")
		var missing netlink.LinkNotFoundError
		require.ErrorAs(t, err, &missing)
		blue, err := netlink.LinkByName("blue")
		require.NoError(t, err)
		require.NotZero(t, blue.Attrs().Flags&net.FlagUp)
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4,
			&netlink.Route{Table: int(blue.(*netlink.Vrf).Table), Protocol: 242},
			netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
		require.NoError(t, err)
		require.Len(t, routes, 1)
		requireConfiguratorDefaultGuard(t, routes[0])
		return nil
	}))

}

func requireConfiguratorDefaultGuard(t *testing.T, route netlink.Route) {
	t.Helper()
	require.Equal(t, netlink.RouteProtocol(242), route.Protocol)
	require.Equal(t, 4278198272, route.Priority)
	require.Equal(t, unix.RTN_UNREACHABLE, route.Type)
	// The kernel/netlink may encode /0 as a nil destination.
	if route.Dst == nil {
		return
	}
	ones, bits := route.Dst.Mask.Size()
	require.Zero(t, ones)
	require.Contains(t, []int{32, 128}, bits)
	require.True(t, route.Dst.IP.IsUnspecified())
}

func configuratorTestNamespace(t *testing.T) netns.NsHandle {
	t.Helper()
	runtime.LockOSThread()
	original, err := netns.Get()
	defer func() {
		if !original.IsOpen() {
			runtime.UnlockOSThread()
			return
		}
		restoreErr := netns.Set(original)
		closeErr := original.Close()
		// A failed restore leaves this goroutine locked; Go discards its OS
		// thread on exit rather than running other code in the test namespace.
		require.NoError(t, restoreErr, "restore original namespace")
		runtime.UnlockOSThread()
		require.NoError(t, closeErr)
	}()
	require.NoError(t, err)
	ns, err := netns.New()
	if ns.IsOpen() {
		t.Cleanup(func() { require.NoError(t, ns.Close()) })
	}
	require.NoError(t, err)
	return ns
}
