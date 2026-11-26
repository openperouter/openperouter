// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// netlink v1.3.1 does not name Linux SEG6_LOCAL_ACTION_END_DT46.
const seg6ActionEndDT46 = 16

func checkManagedSRv6Dataplane(left, right srv6GuardNode, leftFRR, rightFRR *guardFRR,
	perFamilySID bool, encapBehavior string, mixed bool,
) {
	configs := prepareManagedSRv6Nodes(left, right, perFamilySID)
	packet := func(family int, target string) error {
		return netnamespace.In(left.clientNS, func() error { return guardPacketVia(family, target, right.clientNS, "") })
	}
	By("managed reconciliation preserves IPv4 and IPv6 SRv6 forwarding")
	Eventually(func() error { return packet(netlink.FAMILY_V4, "198.51.100.2") }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(func() error { return packet(netlink.FAMILY_V6, "2001:db8:102::2") }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	for range 3 {
		reconcileSRv6TestNode(left, configs[left.id])
	}
	_, err := rightFRR.vty("configure terminal", "router bgp 65002 vrf red", "address-family ipv6 unicast", "network ::/0")
	Expect(err).To(Succeed())
	Eventually(func() (string, error) { return leftFRR.vty("show ipv6 route vrf red ::/0") },
		10*time.Second, 100*time.Millisecond).Should(ContainSubstring("Known via \"bgp\""))
	ingress := newSRv6TransitIngress(left, "2001:db8:102::2")
	transitPacket := func() error {
		return netnamespace.In(ingress, func() error {
			return guardPacketVia(netlink.FAMILY_V6, "2001:db8:102::2", right.clientNS, "", "2001:db8:feed::2")
		})
	}
	Eventually(transitPacket, 5*time.Second, 100*time.Millisecond).Should(Succeed(), "SRv6 decap/re-encap control must deliver")
	var mainControl func() error
	if !perFamilySID {
		mainControl = srv6MainLocatorControl(left, right)
		Expect(mainControl()).To(Succeed())
	}
	By("locator loss stops repeated traffic")
	var blockedCapture int
	Expect(netnamespace.In(right.ns, func() error {
		port, err := netlink.LinkByName(right.transit)
		if err != nil {
			return err
		}
		blockedCapture = srv6CaptureSocket(port.Attrs().Index)
		return nil
	})).To(Succeed())
	defer func() { Expect(unix.Close(blockedCapture)).To(Succeed()) }()
	var underlay netlink.Route
	Expect(netnamespace.In(left.ns, func() error {
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: int(configs[left.id].UnderlayTable), Dst: guardCIDR("fc00:10:2::/48")}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
		if err != nil {
			return err
		}
		Expect(routes).To(HaveLen(1))
		underlay = routes[0]
		return netlink.RouteDel(&underlay)
	})).To(Succeed())
	for range 3 {
		Expect(srv6PacketDelivered(left.clientNS, netlink.FAMILY_V4, "198.51.100.2", right.clientNS)).To(BeFalse())
		Expect(srv6PacketDelivered(left.clientNS, netlink.FAMILY_V6, "2001:db8:102::2", right.clientNS)).To(BeFalse())
		Expect(srv6PacketDelivered(ingress, netlink.FAMILY_V6, "2001:db8:102::2", right.clientNS, "2001:db8:feed::2")).To(BeFalse())
	}
	if mainControl != nil {
		Expect(mainControl()).To(Succeed(), "main locator must remain usable while the dedicated route is absent")
		Expect(capturedSRv6Source(blockedCapture, net.ParseIP(configs[left.id].Source))).To(BeFalse(),
			"missing dedicated-table locator must not fall through to the still-usable main locator")
	}
	By("VPN reannouncement remains isolated without the underlay locator")
	const remoteIPv4 = "198.51.100.0/24"
	kernelLWT := func() (bool, error) { return managedSRv6KernelLWT(left.ns, remoteIPv4) }

	_, err = rightFRR.vty("configure terminal", "router bgp 65002 vrf red", "address-family ipv4 unicast",
		"no network "+remoteIPv4)
	Expect(err).To(Succeed())
	Eventually(func() (string, error) { return leftFRR.vty("show ip route vrf red " + remoteIPv4) },
		10*time.Second, 100*time.Millisecond).ShouldNot(ContainSubstring("Known via \"bgp\""))
	Eventually(kernelLWT, 10*time.Second, 100*time.Millisecond).Should(BeFalse(), "withdrawal removes the old kernel LWT")
	_, err = rightFRR.vty("configure terminal", "router bgp 65002 vrf red", "address-family ipv4 unicast",
		"network "+remoteIPv4)
	Expect(err).To(Succeed())
	Eventually(func() (string, error) { return leftFRR.vty("show bgp ipv4 vpn") },
		10*time.Second, 100*time.Millisecond).Should(ContainSubstring(remoteIPv4))
	if perFamilySID && mixed {
		// FRR keeps the VPN path in BGP, but zebra cannot install its LWT
		// when the only locator route in main has been removed.
		Consistently(kernelLWT, time.Second, 100*time.Millisecond).Should(BeFalse())
	} else {
		Eventually(kernelLWT, 10*time.Second, 100*time.Millisecond).Should(BeTrue(),
			"FRR must reinstall a kernel LWT before checking the cold route")
	}
	Expect(srv6PacketDelivered(left.clientNS, netlink.FAMILY_V4, "198.51.100.2", right.clientNS)).To(BeFalse())
	Expect(netnamespace.In(left.ns, func() error { return netlink.RouteAdd(&underlay) })).To(Succeed())
	Eventually(func() error { return packet(netlink.FAMILY_V4, "198.51.100.2") }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(func() error { return packet(netlink.FAMILY_V6, "2001:db8:102::2") }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	_, err = rightFRR.vty("configure terminal", "router bgp 65002 vrf red", "address-family ipv6 unicast", "no network ::/0")
	Expect(err).To(Succeed())
	Eventually(func() (string, error) { return leftFRR.vty("show ipv6 route vrf red ::/0") },
		10*time.Second, 100*time.Millisecond).ShouldNot(ContainSubstring("Known via \"bgp\""))
	checkSRv6Defaults(left, right, rightFRR, packet)

	By("a full FRR restart preserves guards and recovers VPN routes")
	leftFRR.stop()
	Expect(srv6PacketDelivered(left.clientNS, netlink.FAMILY_V6, "2001:db8:102::2", right.clientNS)).To(BeFalse())
	reconcileSRv6TestNode(left, configs[left.id])
	leftFRR.start()
	Eventually(func() error { return packet(netlink.FAMILY_V4, "198.51.100.2") }, 15*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(func() error { return packet(netlink.FAMILY_V6, "2001:db8:102::2") }, 15*time.Second, 100*time.Millisecond).Should(Succeed())
	By("changing the explicit encapsulation source preserves warmed SRv6 forwarding")
	var captureFD int
	Expect(netnamespace.In(right.ns, func() error {
		transit, err := netlink.LinkByName(right.transit)
		if err != nil {
			return err
		}
		captureFD = srv6CaptureSocket(transit.Attrs().Index)
		return nil
	})).To(Succeed())
	defer func() { Expect(unix.Close(captureFD)).To(Succeed()) }()
	config := configs[left.id]
	config.Source = "2001:db8:ffff::99"
	Expect(netnamespace.In(left.ns, func() error {
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return err
		}
		guardAddress(lo, config.Source+"/128")
		return nil
	})).To(Succeed())
	reconcileSRv6TestNode(left, config)
	Eventually(func() error { return packet(netlink.FAMILY_V4, "198.51.100.2") }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(func() error { return packet(netlink.FAMILY_V6, "2001:db8:102::2") }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	Eventually(func() (bool, error) { return capturedSRv6Source(captureFD, net.ParseIP(config.Source)) },
		5*time.Second, 100*time.Millisecond).Should(BeTrue(), "wire source must match the updated configuration")
	// Persist the same desired source in FRR: zebra configures it at startup.
	updatedFRR := strings.ReplaceAll(left.configuration(perFamilySID, encapBehavior), "blue", "red")
	updatedFRR = strings.ReplaceAll(updatedFRR,
		"source-address 2001:db8:ffff::1", "source-address "+config.Source)
	for _, path := range []string{leftFRR.dir, filepath.Join("/etc/frr", filepath.Base(leftFRR.dir))} {
		Expect(os.WriteFile(filepath.Join(path, "frr.conf"), []byte(updatedFRR), 0644)).To(Succeed())
	}
	By("FRR restart retains the updated kernel encapsulation source")
	leftFRR.stop()
	leftFRR.start()
	Expect(netnamespace.In(left.ns, func() error {
		actual, err := getSRv6TunnelSource()
		if err != nil {
			return err
		}
		Expect(actual.Equal(net.ParseIP(config.Source))).To(BeTrue())
		return nil
	})).To(Succeed())
	Eventually(func() error { return packet(netlink.FAMILY_V6, "2001:db8:102::2") },
		15*time.Second, 100*time.Millisecond).Should(Succeed())
}

func reconcileSRv6TestNode(node srv6GuardNode, config *SRv6Isolation) {
	targetNS := fmt.Sprintf("/proc/self/fd/%d", node.ns)
	params := L3VPNParams{VRF: "red", TargetNS: targetNS, RoutingIsolation: config}
	Expect(ReconcileSRv6Isolation(targetNS, []L3VPNParams{params}, "blue")).To(Succeed())
	Expect((&VNIConfigurator{ns: node.ns}).SetupL3VPN(context.Background(), params)).To(Succeed())
}

func checkSRv6Defaults(left, right srv6GuardNode, rightFRR *guardFRR, packet func(int, string) error) {
	By("FRR defaults override the guards, and withdrawal blocks main fallback")
	for _, family := range []struct {
		afi, target, local string
		family             int
	}{
		{afi: "ipv4", target: "198.19.99.2", local: "198.19.99.1/24", family: netlink.FAMILY_V4},
		{afi: "ipv6", target: "2001:db8:199::2", local: "2001:db8:199::1/64", family: netlink.FAMILY_V6},
	} {
		Expect(netnamespace.In(right.ns, func() error {
			port, err := netlink.LinkByName("tenant")
			if err != nil {
				return err
			}
			guardAddress(port, family.local)
			return nil
		})).To(Succeed())
		Expect(netnamespace.In(right.clientNS, func() error {
			port, err := netlink.LinkByName("tenant-peer")
			if err != nil {
				return err
			}
			_, prefix, err := net.ParseCIDR(family.local)
			if err != nil {
				return err
			}
			ones, _ := prefix.Mask.Size()
			guardAddress(port, fmt.Sprintf("%s/%d", family.target, ones))
			return nil
		})).To(Succeed())
		Expect(netnamespace.In(right.ns, func() error {
			port, err := netlink.LinkByName("tenant")
			if err != nil {
				return err
			}
			bits := 128
			if family.family == netlink.FAMILY_V4 {
				bits = 32
			}
			route := netlink.Route{Table: unix.RT_TABLE_MAIN, LinkIndex: port.Attrs().Index,
				Dst: guardCIDR(fmt.Sprintf("%s/%d", family.target, bits)), Scope: netlink.SCOPE_LINK}
			return netlink.RouteAdd(&route)
		})).To(Succeed())
		// The target is deliberately absent from tenant BGP advertisements. The
		// underlay control route would deliver it if the tenant lookup fell through.
		Expect(netnamespace.In(left.ns, func() error {
			transit, err := netlink.LinkByName(left.transit)
			if err != nil {
				return err
			}
			bits := 128
			gateway := "2001:db8:12::2"
			if family.family == netlink.FAMILY_V4 {
				bits = 32
				gateway = "198.18.0.2"
			}
			route := netlink.Route{Table: unix.RT_TABLE_MAIN, LinkIndex: transit.Attrs().Index, Gw: net.ParseIP(gateway), Dst: guardCIDR(fmt.Sprintf("%s/%d", family.target, bits))}
			return netlink.RouteAdd(&route)
		})).To(Succeed())
		Eventually(func() error {
			return netnamespace.In(left.ns, func() error {
				return guardPacketVia(family.family, family.target, right.clientNS, "")
			})
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed(), "main-table control must deliver real packets")
		Expect(srv6PacketDelivered(left.clientNS, family.family, family.target, right.clientNS)).To(BeFalse())
		prefix := "::/0"
		if family.family == netlink.FAMILY_V4 {
			prefix = "0.0.0.0/0"
		}
		_, err := rightFRR.vty("configure terminal", "router bgp 65002 vrf red", "address-family "+family.afi+" unicast", "network "+prefix)
		Expect(err).To(Succeed())
		Eventually(func() error { return packet(family.family, family.target) }, 15*time.Second, 100*time.Millisecond).Should(Succeed())
		_, err = rightFRR.vty("configure terminal", "router bgp 65002 vrf red", "address-family "+family.afi+" unicast", "no network "+prefix)
		Expect(err).To(Succeed())
		Eventually(func() (bool, error) {
			return srv6PacketDelivered(left.clientNS, family.family, family.target, right.clientNS)
		}, 10*time.Second, 100*time.Millisecond).Should(BeFalse())
	}
}

func srv6TestIsolation(node srv6GuardNode) *SRv6Isolation {
	return &SRv6Isolation{Source: fmt.Sprintf("2001:db8:ffff::%d", node.id), UnderlayTable: unix.RT_TABLE_MAIN,
		RemoteLocatorPrefixes: []string{fmt.Sprintf("fc00:10:%d::/48", node.peerID)}}
}

func capturedSRv6Source(fd int, source net.IP) (bool, error) {
	var buffer [4096]byte
	for {
		n, _, err := unix.Recvfrom(fd, buffer[:], unix.MSG_DONTWAIT)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("receive SRv6 capture: %w", err)
		}
		if n < 54 || buffer[14]>>4 != 6 {
			continue
		}
		if !net.IP(buffer[22:38]).Equal(source) {
			continue
		}
		protocol := buffer[20]
		if protocol == unix.IPPROTO_ROUTING || protocol == unix.IPPROTO_IPIP || protocol == unix.IPPROTO_IPV6 {
			return true, nil
		}
	}
}

func newSRv6TransitIngress(node srv6GuardNode, target string) netns.NsHandle {
	ingress := newGuardNamespace()
	var sid net.IP
	Expect(netnamespace.In(node.ns, func() error {
		red, err := lookupVRF("red")
		if err != nil {
			return err
		}
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: unix.RT_TABLE_UNSPEC}, netlink.RT_FILTER_TABLE)
		if err != nil {
			return err
		}
		for _, route := range routes {
			local, ok := route.Encap.(*netlink.SEG6LocalEncap)
			if !ok {
				continue
			}
			table := local.Table
			if local.Flags[nl.SEG6_LOCAL_VRFTABLE] {
				table = local.VrfTable
			}
			if table == int(red.Table) && (local.Action == nl.SEG6_LOCAL_ACTION_END_DT6 || local.Action == seg6ActionEndDT46) {
				sid = route.Dst.IP
				break
			}
		}
		Expect(sid).NotTo(BeNil(), "managed decapsulation SID is required for the transit control")
		guardVeth(ingress, "sginject", "2001:db8:feed::1/64", "2001:db8:feed::2/64", nil)
		return nil
	})).To(Succeed())
	Expect(netnamespace.In(ingress, func() error {
		port, err := netlink.LinkByName("sginject-peer")
		if err != nil {
			return err
		}
		Expect(setSRv6TunnelSource(net.ParseIP("2001:db8:feed::2"))).To(Succeed())
		for _, route := range []netlink.Route{
			{LinkIndex: port.Attrs().Index, Gw: net.ParseIP("2001:db8:feed::1"), Dst: &net.IPNet{IP: sid, Mask: net.CIDRMask(128, 128)}},
			{LinkIndex: port.Attrs().Index, Gw: net.ParseIP("2001:db8:feed::1"), Dst: guardCIDR(target + "/128"),
				Encap: &netlink.SEG6Encap{Mode: nl.SEG6_IPTUN_MODE_ENCAP, Segments: []net.IP{sid}}},
		} {
			r := route
			Expect(netlink.RouteAdd(&r)).To(Succeed())
		}
		return nil
	})).To(Succeed())
	return ingress
}

func prepareManagedSRv6Nodes(left, right srv6GuardNode, perFamilySID bool) map[int]*SRv6Isolation {
	configs := map[int]*SRv6Isolation{}
	for _, node := range []srv6GuardNode{left, right} {
		config := srv6TestIsolation(node)
		if !perFamilySID {
			config.UnderlayTable = 701
		}
		if !perFamilySID {
			copySRv6TestUnderlay(node, 701)
		}
		reconcileSRv6TestNode(node, config)
		configs[node.id] = config
	}
	return configs
}

func copySRv6TestUnderlay(node srv6GuardNode, table uint32) {
	Expect(netnamespace.In(node.ns, func() error {
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{
			Table: unix.RT_TABLE_MAIN, Dst: guardCIDR(fmt.Sprintf("fc00:10:%d::/48", node.peerID)),
		}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
		if err != nil {
			return err
		}
		Expect(routes).To(HaveLen(1))
		route := routes[0]
		route.Table = int(table)
		return netlink.RouteAdd(&route)
	})).To(Succeed())
}

func srv6MainLocatorControl(left, right srv6GuardNode) func() error {
	const target = "fc00:10:2:ffff::99"
	Expect(netnamespace.In(right.ns, func() error {
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return err
		}
		guardAddress(lo, target+"/128")
		return nil
	})).To(Succeed())
	return func() error {
		return netnamespace.In(left.ns, func() error {
			return guardPacketVia(netlink.FAMILY_V6, target, right.ns, "", fmt.Sprintf("2001:db8:12::%d", left.id))
		})
	}
}

func srv6PacketDelivered(sender netns.NsHandle, family int, target string, receiver netns.NsHandle,
	source ...string,
) (bool, error) {
	var delivered bool
	err := netnamespace.In(sender, func() error {
		var err error
		delivered, err = guardPacketViaResult(family, target, receiver, "", source...)
		return err
	})
	return delivered, err
}

func managedSRv6KernelLWT(ns netns.NsHandle, prefix string) (bool, error) {
	var installed bool
	err := netnamespace.In(ns, func() error {
		vrf, err := lookupVRF("red")
		if err != nil {
			return err
		}
		routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4,
			&netlink.Route{Table: int(vrf.Table), Dst: guardCIDR(prefix)}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
		if err != nil {
			return err
		}
		for _, route := range routes {
			if _, ok := route.Encap.(*netlink.SEG6Encap); ok {
				installed = true
			}
			for _, hop := range route.MultiPath {
				if _, ok := hop.Encap.(*netlink.SEG6Encap); ok {
					installed = true
				}
			}
		}
		return nil
	})
	return installed, err
}
