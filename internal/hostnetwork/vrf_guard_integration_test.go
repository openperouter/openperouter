// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

var _ = Describe("EVPN VRF default guards", func() {
	var ns netns.NsHandle

	BeforeEach(func() {
		ns = newGuardNamespace()
	})

	It("repairs deletion, preserves foreign defaults and uses a recreated VRF's actual table", func() {
		Expect(netnamespace.In(ns, func() error {
			vrf := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: "red"}, Table: 901}
			Expect(netlink.LinkAdd(vrf)).To(Succeed())
			Expect(setupVRF("red", evpnVRF)).To(Succeed())
			foreign := netlink.Route{
				Family: netlink.FAMILY_V4, Table: 901, Type: unix.RTN_BLACKHOLE,
				Dst:      vrfDefaultGuard(901, netlink.FAMILY_V4).Dst,
				Protocol: unix.RTPROT_BGP, Priority: 20,
			}
			Expect(netlink.RouteAdd(&foreign)).To(Succeed())
			before, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &foreign, netlink.RT_FILTER_TABLE)
			Expect(err).To(Succeed())
			for range 3 {
				Expect(setupVRF("red", evpnVRF)).To(Succeed())
			}
			Expect(netlink.RouteListFiltered(netlink.FAMILY_V4, &foreign, netlink.RT_FILTER_TABLE)).To(ConsistOf(before))
			guards, err := listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4))
			Expect(err).To(Succeed())
			Expect(guards).To(HaveLen(1))
			Expect(netlink.RouteDel(&guards[0])).To(Succeed())
			Expect(setupVRF("red", evpnVRF)).To(Succeed())
			Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4))).To(HaveLen(1))

			Expect(netlink.LinkDel(vrf)).To(Succeed())
			// Linux VRF deletion need not flush unreachable routes without an output device.
			vrf.Table = 902
			vrf.Index = 0
			Expect(netlink.LinkAdd(vrf)).To(Succeed())
			Expect(setupVRF("red", evpnVRF)).To(Succeed())
			Expect(listVRFDefaultGuards(vrfDefaultGuard(902, netlink.FAMILY_V4))).To(HaveLen(1))
			return nil
		})).To(Succeed())
	})

	It("reports a default collision without changing foreign routes or port state", func() {
		Expect(netnamespace.In(ns, func() error {
			Expect(setupVRF("red")).To(Succeed())
			vrf, err := lookupVRF("red")
			Expect(err).To(Succeed())
			port := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "tenant"}}
			Expect(netlink.LinkAdd(port)).To(Succeed())
			Expect(netlink.LinkSetMaster(port, vrf)).To(Succeed())
			Expect(netlink.LinkSetUp(port)).To(Succeed())
			underlay := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "underlay"}}
			Expect(netlink.LinkAdd(underlay)).To(Succeed())
			Expect(netlink.LinkSetUp(underlay)).To(Succeed())
			foreign := vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4)
			foreign.Type = unix.RTN_BLACKHOLE
			foreign.Protocol = unix.RTPROT_BGP
			Expect(netlink.RouteAdd(&foreign)).To(Succeed())
			Expect(errors.Is(setupVRF("red", evpnVRF), unix.EEXIST)).To(BeTrue())
			observed, err := netlink.LinkByName("tenant")
			Expect(err).To(Succeed())
			Expect(observed.Attrs().MasterIndex).To(Equal(vrf.Index))
			Expect(observed.Attrs().Flags & net.FlagUp).To(Equal(net.FlagUp))
			underlayLink, err := netlink.LinkByName("underlay")
			Expect(err).To(Succeed())
			Expect(underlayLink.Attrs().Flags & net.FlagUp).To(Equal(net.FlagUp))
			Expect(netlink.RouteListFiltered(netlink.FAMILY_V4, &foreign,
				netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL|netlink.RT_FILTER_TYPE)).To(HaveLen(1))
			return nil
		})).To(Succeed())
	})

	It("keeps IPv4 working when IPv6 is disabled and removes only owned guards for SRv6", func() {
		Expect(netnamespace.In(ns, func() error {
			Expect(sysctl.Ensure(
				sysctl.Sysctl{Path: "net/ipv6/conf/all/disable_ipv6", Value: "1"},
				sysctl.Sysctl{Path: "net/ipv6/conf/default/disable_ipv6", Value: "1"},
			)).To(Succeed())
			Expect(setupVRF("red", evpnVRF)).To(Succeed())
			vrf, err := lookupVRF("red")
			Expect(err).To(Succeed())
			Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4))).To(HaveLen(1))
			Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V6))).To(BeEmpty())
			foreign := vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4)
			foreign.Protocol = unix.RTPROT_BGP
			foreign.Priority = 20
			Expect(netlink.RouteAdd(&foreign)).To(Succeed())
			Expect(setupVRF("red", srv6VRF)).To(Succeed())
			Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4))).To(BeEmpty())
			Expect(netlink.RouteListFiltered(netlink.FAMILY_V4, &foreign,
				netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)).To(HaveLen(1))
			Expect(setupVRF("red", evpnVRF, srv6VRF)).To(MatchError("VRF red cannot use both EVPN isolation and SRv6"))
			return nil
		})).To(Succeed())
	})

	DescribeTable("protects IPv6 enabled despite all.disable_ipv6",
		func(device string) {
			Expect(netnamespace.In(ns, func() error {
				Expect(sysctl.Ensure(
					sysctl.Sysctl{Path: "net/ipv6/conf/all/disable_ipv6", Value: "1"},
					sysctl.Sysctl{Path: "net/ipv6/conf/default/disable_ipv6", Value: "1"},
				)).To(Succeed())
				Expect(setupVRF("red")).To(Succeed())
				if device == "underlay" {
					underlay := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: device}}
					Expect(netlink.LinkAdd(underlay)).To(Succeed())
					Expect(netlink.LinkSetUp(underlay)).To(Succeed())
				}
				Expect(sysctl.Ensure(sysctl.Sysctl{
					Path: "net/ipv6/conf/" + device + "/disable_ipv6", Value: "0",
				})).To(Succeed())
				Expect(setupVRF("red", evpnVRF)).To(Succeed())
				vrf, err := lookupVRF("red")
				Expect(err).To(Succeed())
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V4))).To(HaveLen(1))
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, netlink.FAMILY_V6))).To(HaveLen(1))
				return nil
			})).To(Succeed())
		},
		Entry("future interfaces", "default"),
		Entry("VRF interface", "red"),
		Entry("another interface in the namespace", "underlay"),
	)

	DescribeTable("blocks actual packets from main and yields to working kernel routes",
		func(family int, localCIDR, peerCIDR, mainCIDR, mainPeerCIDR, targetCIDR string) {
			peerNS := newGuardNamespace()
			Expect(netnamespace.In(ns, func() error {
				Expect(setupVRF("red")).To(Succeed())
				vrf, err := lookupVRF("red")
				Expect(err).To(Succeed())
				tenant := guardVeth(peerNS, "tenant", localCIDR, peerCIDR, vrf)
				main := guardVeth(peerNS, "outside", mainCIDR, mainPeerCIDR, nil)
				Expect(netnamespace.In(peerNS, func() error {
					lo, err := netlink.LinkByName("lo")
					Expect(err).To(Succeed())
					Expect(netlink.LinkSetUp(lo)).To(Succeed())
					guardAddress(lo, targetCIDR)
					return nil
				})).To(Succeed())
				target, prefix, err := net.ParseCIDR(targetCIDR)
				Expect(err).To(Succeed())
				mainPeer, _, err := net.ParseCIDR(mainPeerCIDR)
				Expect(err).To(Succeed())
				fallback := netlink.Route{Family: family, Table: unix.RT_TABLE_MAIN,
					Dst: prefix, Gw: mainPeer, LinkIndex: main.Attrs().Index}
				Expect(netlink.RouteAdd(&fallback)).To(Succeed())
				// Establish the leaking path first; a missing/broken main path must not pass this test.
				Eventually(func() error { return guardPacket(family, target.String(), peerNS) }, 5*time.Second, 100*time.Millisecond).Should(Succeed())
				Expect(setupVRF("red", evpnVRF)).To(Succeed())
				Expect(guardPacket(family, target.String(), peerNS)).NotTo(Succeed())

				peer, _, err := net.ParseCIDR(peerCIDR)
				Expect(err).To(Succeed())
				working := netlink.Route{Family: family, Table: int(vrf.Table), Dst: prefix,
					Gw: peer, LinkIndex: tenant.Attrs().Index, Protocol: unix.RTPROT_BGP, Priority: 20}
				Expect(netlink.RouteAdd(&working)).To(Succeed())
				Eventually(func() error { return guardPacket(family, target.String(), peerNS) }, 5*time.Second, 100*time.Millisecond).Should(Succeed())
				Expect(netlink.RouteDel(&working)).To(Succeed())
				Expect(guardPacket(family, target.String(), peerNS)).NotTo(Succeed())

				working.Dst = vrfDefaultGuard(vrf.Table, family).Dst
				// Zebra uses replacement for its own defaults. A different priority
				// must leave the guard installed, including on subsequent replacement.
				Expect(netlink.RouteReplace(&working)).To(Succeed())
				Expect(netlink.RouteReplace(&working)).To(Succeed())
				Expect(setupVRF("red", evpnVRF)).To(Succeed())
				Eventually(func() error { return guardPacket(family, target.String(), peerNS) }, 5*time.Second, 100*time.Millisecond).Should(Succeed())
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(HaveLen(1))
				Expect(netlink.RouteDel(&working)).To(Succeed())
				Expect(guardPacket(family, target.String(), peerNS)).NotTo(Succeed())
				return nil
			})).To(Succeed())
		},
		Entry("IPv4", netlink.FAMILY_V4, "192.0.2.1/24", "192.0.2.2/24",
			"198.18.0.1/24", "198.18.0.2/24", "203.0.113.9/32"),
		Entry("IPv6", netlink.FAMILY_V6, "2001:db8:1::1/64", "2001:db8:1::2/64",
			"2001:db8:2::1/64", "2001:db8:2::2/64", "2001:db8:ffff::9/128"),
	)
})

func newGuardNamespace() netns.NsHandle {
	runtime.LockOSThread()
	original, err := netns.Get()
	defer func() {
		if !original.IsOpen() {
			runtime.UnlockOSThread()
			return
		}
		restoreErr := netns.Set(original)
		closeErr := original.Close()
		// Fail before unlocking if restoration fails, so Go discards the
		// switched OS thread when this goroutine exits.
		Expect(restoreErr).To(Succeed(), "restore original namespace")
		runtime.UnlockOSThread()
		Expect(closeErr).To(Succeed())
	}()
	Expect(err).To(Succeed())
	ns, err := netns.New()
	if ns.IsOpen() {
		DeferCleanup(func() { Expect(ns.Close()).To(Succeed()) })
	}
	Expect(err).To(Succeed())
	return ns
}

func guardVeth(peerNS netns.NsHandle, name, localCIDR, peerCIDR string, vrf *netlink.Vrf) netlink.Link {
	veth := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: name}, PeerName: name + "-peer"}
	Expect(netlink.LinkAdd(veth)).To(Succeed())
	if vrf != nil {
		Expect(netlink.LinkSetMaster(veth, vrf)).To(Succeed())
	}
	guardAddress(veth, localCIDR)
	Expect(netlink.LinkSetUp(veth)).To(Succeed())
	peer, err := netlink.LinkByName(name + "-peer")
	Expect(err).To(Succeed())
	Expect(netlink.LinkSetNsFd(peer, int(peerNS))).To(Succeed())
	Expect(netnamespace.In(peerNS, func() error {
		peer, err := netlink.LinkByName(name + "-peer")
		Expect(err).To(Succeed())
		guardAddress(peer, peerCIDR)
		Expect(netlink.LinkSetUp(peer)).To(Succeed())
		Expect(sysctl.Ensure(sysctl.DisableRPFilter("all"), sysctl.DisableRPFilter(name+"-peer"))).To(Succeed())
		return nil
	})).To(Succeed())
	return veth
}

func guardAddress(link netlink.Link, cidr string) {
	addr, err := netlink.ParseAddr(cidr)
	Expect(err).To(Succeed())
	addr.Flags = unix.IFA_F_NODAD
	Expect(netlink.AddrAdd(link, addr)).To(Succeed())
}

// guardPacket checks delivery in a second namespace, including the unprotected
// control path through main. A failed ping alone would not prove isolation.
func guardPacket(family int, target string, peerNS netns.NsHandle) error {
	return guardPacketVia(family, target, peerNS, "red")
}

func guardPacketVia(family int, target string, peerNS netns.NsHandle, vrf string, source ...string) error {
	network := "udp4"
	if family == netlink.FAMILY_V6 {
		network = "udp6"
	}
	address := net.JoinHostPort(target, "17001")
	var receiver net.PacketConn
	if err := netnamespace.In(peerNS, func() error {
		var err error
		receiver, err = net.ListenPacket(network, address)
		return err
	}); err != nil {
		return err
	}
	defer func() { Expect(receiver.Close()).To(Succeed()) }()
	dialer := net.Dialer{
		Control: func(_, _ string, raw syscall.RawConn) error {
			if vrf == "" {
				return nil
			}
			var bindErr error
			err := raw.Control(func(fd uintptr) {
				bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, vrf)
			})
			return errors.Join(err, bindErr)
		},
	}
	if len(source) != 0 {
		dialer.LocalAddr = &net.UDPAddr{IP: net.ParseIP(source[0])}
	}
	conn, err := dialer.DialContext(context.Background(), network, address)
	if err != nil {
		return err
	}
	defer func() { Expect(conn.Close()).To(Succeed()) }()
	if _, err := conn.Write([]byte("guard-probe")); err != nil {
		return err
	}
	Expect(receiver.SetReadDeadline(time.Now().Add(200 * time.Millisecond))).To(Succeed())
	buf := make([]byte, 128)
	n, _, err := receiver.ReadFrom(buf)
	if err != nil {
		return err
	}
	if string(buf[:n]) != "guard-probe" {
		return fmt.Errorf("unexpected packet %q", buf[:n])
	}
	return nil
}
