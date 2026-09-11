// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package routerconfiguration

import (
	"net"
	"runtime"
	"testing"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

func TestHostNetworkLifecycle(t *testing.T) {
	// Emulate a dedicated VNF host inside a disposable namespace. Cleanup must
	// never operate on the test runner's real host namespace.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	original, err := netns.Get()
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	ns, err := netns.NewNamed("vnf-poc-test")
	if err != nil {
		t.Fatal(err)
	}
	defer ns.Close()
	defer netns.DeleteNamed("vnf-poc-test")
	defer func() {
		if err := netns.Set(original); err != nil {
			t.Fatalf("restore runner namespace: %v", err)
		}
	}()
	const target = "/run/netns/vnf-poc-test"
	uplink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "uplink", MTU: 9000, Group: 17}}
	if err := netlink.LinkAdd(uplink); err != nil {
		t.Fatal(err)
	}
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	if err := hostnetwork.AssignIPToInterface(lo, "192.0.2.99/32"); err != nil {
		t.Fatal(err)
	}
	config := interfacesConfiguration{
		targetNamespace: target,
		APIConfigData: conversion.APIConfigData{
			Underlays: []v1alpha1.Underlay{{Spec: v1alpha1.UnderlaySpec{
				ASN:            64514,
				Neighbors:      []v1alpha1.Neighbor{{ASN: new(int64(64512)), Address: new("192.0.2.1")}},
				TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{CIDRs: []string{"100.65.0.0/24"}},
			}}},
			L3VNIs: []v1alpha1.L3VNI{{Spec: v1alpha1.L3VNISpec{
				VRF: "red", VNI: 100, VXLanPort: new(int32(4789)),
			}}},
		},
	}
	k := &KernelDatapathConfigurator{}
	for range 2 {
		if err := k.Configure(t.Context(), config); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := netlink.LinkByName("red"); err != nil {
		t.Fatalf("overlay VRF missing: %v", err)
	}
	// Listed uplinks are also externally owned, not just omitted ones.
	config.Underlays[0].Spec.Interfaces = []v1alpha1.UnderlayInterface{{
		Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
		NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "uplink"},
	}}
	if err := k.Configure(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	if mtu, err := hostnetwork.FindUnderlayMTU(ns); err != nil || mtu != 0 {
		t.Fatalf("host MTU must be externally managed: %d, %v", mtu, err)
	}
	if err := k.Configure(t.Context(), interfacesConfiguration{targetNamespace: target}); err != nil {
		t.Fatal(err)
	}
	links, err := netlink.LinkList()
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range links {
		if link.Type() == "vrf" || link.Type() == "vxlan" {
			t.Errorf("overlay link left after removal: %s", link.Attrs().Name)
		}
	}
	got, err := netlink.LinkByName("uplink")
	if err != nil {
		t.Fatal(err)
	}
	if got.Attrs().Group != 17 || got.Attrs().MTU != 9000 || got.Attrs().Flags&net.FlagUp != 0 {
		t.Fatalf("uplink was modified: %+v", got.Attrs())
	}
	addresses, err := netlink.AddrList(lo, netlink.FAMILY_V4)
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addresses {
		if addr.IP.String() == "192.0.2.99" {
			return
		}
	}
	t.Fatal("VM-provisioned loopback address was removed")
}
