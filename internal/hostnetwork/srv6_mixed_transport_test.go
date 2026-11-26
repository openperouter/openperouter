// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/networklayerprotocol"
	"github.com/openperouter/openperouter/internal/sysctl"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

var _ = Describe("Simultaneous EVPN and SRv6 dataplane", func() {
	It("keeps independent BGP EVPN/VXLAN and VPN/SRv6 directions in one protected VRF", func() {
		requireGuardFRR()
		left, right := newMixedSRv6Pair()
		evpnNS, evpnClient := newGuardNamespace(), newGuardNamespace()
		prepareMixedEVPNPeer(left, right, evpnNS, evpnClient)
		config := srv6TestIsolation(left)
		reconcileSRv6TestNode(left, config)
		leftConfig := strings.ReplaceAll(left.configuration(false, frr.HEncaps), "blue", "red")
		// bgpd reads its BGP sections before the mgmtd-only segment-routing block.
		// Keep the additional peer before that block, as the normal templates do.
		Expect(leftConfig).To(ContainSubstring("\nsegment-routing\n"))
		leftConfig = strings.Replace(leftConfig, "\nsegment-routing\n", mixedEVPNNeighbor+"\nsegment-routing\n", 1)
		routers := []*guardFRR{
			newGuardFRR(left.ns, leftConfig),
			newGuardFRR(right.ns, right.configuration(false, frr.HEncaps)),
			newGuardFRR(evpnNS, mixedEVPNConfiguration()),
		}
		DeferCleanup(func() {
			if !CurrentSpecReport().Failed() {
				return
			}
			for _, router := range routers {
				output, err := router.vty("show bgp summary", "show bgp l2vpn evpn", "show bgp ipv4 vpn", "show bgp ipv6 vpn", "show ip route vrf red", "show ipv6 route vrf red", "show running-config")
				if err != nil {
					GinkgoWriter.Printf("FRR diagnostics %s: %v\n", router.dir, err)
				}
				GinkgoWriter.Printf("FRR diagnostics %s:\n%s\n", router.dir, output)
			}
		})
		for _, router := range routers {
			router.start()
		}
		Expect(netnamespace.In(left.ns, func() error {
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			port, err := netlink.LinkByName(left.transit)
			if err != nil {
				return err
			}
			guardL3VNI(vrf, port.Attrs().Index, "198.18.0.1")
			return nil
		})).To(Succeed())
		paths := []mixedTransportPath{
			{transport: "vxlan", family: netlink.FAMILY_V4, target: "203.0.113.2", prefix: "203.0.113.0/24", receiver: evpnClient},
			{transport: "vxlan", family: netlink.FAMILY_V6, target: "2001:db8:203::2", prefix: "2001:db8:203::/64", receiver: evpnClient},
			{transport: "srv6", family: netlink.FAMILY_V4, target: "198.51.100.2", prefix: "198.51.100.0/24", receiver: right.clientNS},
			{transport: "srv6", family: netlink.FAMILY_V6, target: "2001:db8:102::2", prefix: "2001:db8:102::/64", receiver: right.clientNS},
		}
		checkMixedTransportPaths(left, paths)
		By("idempotent reconciliation preserves both transports")
		for range 3 {
			reconcileSRv6TestNode(left, config)
		}
		checkMixedTransportPaths(left, paths)
		By("both transports forward after adding and removing an unused locator selector")
		config.RemoteLocatorPrefixes = []string{"fc00:10:2::/48", "fc00:99::/48"}
		reconcileSRv6TestNode(left, config)
		checkUnusedLocatorRules(left, config, true)
		checkMixedTransportPaths(left, paths)
		config.RemoteLocatorPrefixes = []string{"fc00:10:2::/48"}
		reconcileSRv6TestNode(left, config)
		checkUnusedLocatorRules(left, config, false)
		checkMixedTransportPaths(left, paths)
		for _, transport := range []string{"vxlan", "srv6"} {
			By("withdrawing only " + transport + " advertisements")
			router, asn := routers[2], 65003
			if transport == "srv6" {
				router, asn = routers[1], 65002
			}
			setMixedTransportNetworks(router, asn, paths, transport, false)
			checkMixedTransportWithdrawal(left, paths, transport)
			setMixedTransportNetworks(router, asn, paths, transport, true)
			checkMixedTransportPaths(left, paths)
		}
		By("full FRR restart recovers both transports without exporting guards")
		for _, router := range routers {
			router.stop()
		}
		for _, path := range paths {
			Expect(path.delivered(left.clientNS)).To(BeFalse())
		}
		reconcileSRv6TestNode(left, config)
		for _, router := range routers {
			router.start()
		}
		checkMixedTransportPaths(left, paths)
		for index, peer := range []string{"2001:db8:ffff::2", "198.18.1.3"} {
			Eventually(func() (string, error) {
				return routers[0].vty("show bgp l2vpn evpn neighbors " + peer + " advertised-routes")
			}, 15*time.Second, 100*time.Millisecond).Should(And(
				ContainSubstring("[5]:[0]:[24]:[192.0.2.0]"), ContainSubstring("[5]:[0]:[64]:[2001:db8:101::]")))
			output, err := routers[0].vty("show bgp l2vpn evpn neighbors " + peer + " advertised-routes")
			Expect(err).To(Succeed())
			Expect(output).NotTo(ContainSubstring("0.0.0.0/0"))
			Expect(output).NotTo(ContainSubstring("::/0"))
			Expect(output).To(ContainSubstring("[5]:[0]:[24]:[192.0.2.0]"))
			Expect(output).To(ContainSubstring("[5]:[0]:[64]:[2001:db8:101::]"))
			Expect(output).NotTo(ContainSubstring("[5]:[0]:[0]:"))
			GinkgoWriter.Printf("EVPN advertisement check peer %d: %s\n", index, output)
		}
		for _, afi := range []string{"ipv4", "ipv6"} {
			Eventually(func() (string, error) {
				return routers[0].vty("show bgp " + afi + " vpn neighbors 2001:db8:ffff::2 advertised-routes")
			}, 15*time.Second, 100*time.Millisecond).Should(ContainSubstring(
				map[string]string{"ipv4": "192.0.2.0/24", "ipv6": "2001:db8:101::/64"}[afi]))
			output, err := routers[0].vty("show bgp " + afi + " vpn neighbors 2001:db8:ffff::2 advertised-routes")
			Expect(err).To(Succeed())
			Expect(output).NotTo(ContainSubstring("0.0.0.0/0"))
			Expect(output).NotTo(ContainSubstring("::/0"))
			Expect(output).To(ContainSubstring(map[string]string{"ipv4": "192.0.2.0/24", "ipv6": "2001:db8:101::/64"}[afi]))
		}
	})
})

type mixedTransportPath struct {
	transport      string
	family         int
	target, prefix string
	receiver       netns.NsHandle
}

func (p mixedTransportPath) packet(sender netns.NsHandle) error {
	return netnamespace.In(sender, func() error { return guardPacketVia(p.family, p.target, p.receiver, "") })
}

func (p mixedTransportPath) delivered(sender netns.NsHandle) (bool, error) {
	var delivered bool
	err := netnamespace.In(sender, func() error {
		var err error
		delivered, err = guardPacketViaResult(p.family, p.target, p.receiver, "")
		return err
	})
	return delivered, err
}

func requireGuardFRR() {
	for _, name := range []string{"mgmtd", "zebra", "bgpd"} {
		if _, err := os.Stat("/usr/lib/frr/" + name); errors.Is(err, os.ErrNotExist) {
			Skip("real FRR binaries are required: " + name)
		}
	}
}

func newMixedSRv6Pair() (srv6GuardNode, srv6GuardNode) {
	left := srv6GuardNode{ns: newGuardNamespace(), clientNS: newGuardNamespace(), id: 1, peerID: 2, transit: "transit", tenant4: "192.0.2.1/24", client4: "192.0.2.2/24", tenant6: "2001:db8:101::1/64", client6: "2001:db8:101::2/64"}
	right := srv6GuardNode{ns: newGuardNamespace(), clientNS: newGuardNamespace(), id: 2, peerID: 1, transit: "transit-peer", tenant4: "198.51.100.1/24", client4: "198.51.100.2/24", tenant6: "2001:db8:102::1/64", client6: "2001:db8:102::2/64"}
	Expect(netnamespace.In(left.ns, func() error { guardVeth(right.ns, left.transit, "198.18.0.1/24", "198.18.0.2/24", nil); return nil })).To(Succeed())
	left.setup()
	right.setup()
	return left, right
}

func prepareMixedEVPNPeer(left, right srv6GuardNode, pe, client netns.NsHandle) {
	Expect(netnamespace.In(right.ns, func() error {
		port := guardVeth(pe, "evpnlink", "198.18.1.2/24", "198.18.1.3/24", nil)
		guardAddress(port, "2001:db8:23::2/64")
		return nil
	})).To(Succeed())
	Expect(netnamespace.In(left.ns, func() error {
		port, err := netlink.LinkByName(left.transit)
		if err != nil {
			return err
		}
		return netlink.RouteAdd(&netlink.Route{Dst: guardCIDR("198.18.1.3/32"), Gw: net.ParseIP("198.18.0.2"), LinkIndex: port.Attrs().Index})
	})).To(Succeed())
	Expect(netnamespace.In(pe, func() error {
		Expect(sysctl.Ensure(sysctl.IPv4Forwarding(), sysctl.IPv6Forwarding(), sysctl.DisableRPFilter("all"))).To(Succeed())
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return err
		}
		Expect(netlink.LinkSetUp(lo)).To(Succeed())
		port, err := netlink.LinkByName("evpnlink-peer")
		if err != nil {
			return err
		}
		guardAddress(port, "2001:db8:23::3/64")
		Expect(netlink.RouteAdd(&netlink.Route{Dst: guardCIDR("198.18.0.1/32"), Gw: net.ParseIP("198.18.1.2"), LinkIndex: port.Attrs().Index})).To(Succeed())
		Expect(setupVRF("red", evpnVRF)).To(Succeed())
		vrf, err := lookupVRF("red")
		if err != nil {
			return err
		}
		guardL3VNI(vrf, port.Attrs().Index, "198.18.1.3")
		bridge, err := netlink.LinkByName("br100")
		if err != nil {
			return err
		}
		Expect(netlink.LinkSetHardwareAddr(bridge, net.HardwareAddr{2, 0, 0, 0, 3, 0})).To(Succeed())
		tenant := guardVeth(client, "evtenant", "203.0.113.1/24", "203.0.113.2/24", vrf)
		guardAddress(tenant, "2001:db8:203::1/64")
		return netnamespace.In(client, func() error {
			port, err := netlink.LinkByName("evtenant-peer")
			if err != nil {
				return err
			}
			guardAddress(port, "2001:db8:203::2/64")
			return nil
		})
	})).To(Succeed())
}

func mixedEVPNConfiguration() string {
	asn, err := frr.NewPeerASN(new(int64(65001)), nil)
	Expect(err).To(Succeed())
	config := frr.Config{Loglevel: "warning", Underlay: frr.UnderlayConfig{MyASN: 65003, RouterID: "198.18.1.3", TunnelEndpoint: &frr.TunnelEndpoint{IPv4CIDR: "198.18.1.3/32"}, Neighbors: []frr.NeighborConfig{{ID: "198.18.0.1", Addr: "198.18.0.1", ASN: asn, EBGPMultiHop: true, ConnectTime: new(int64(1)), NetworkLayerProtocols: []networklayerprotocol.NLP{{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN}}}}}, L3VNIs: []frr.L3VNIConfig{{VRF: "red", ASN: 65003, VNI: 100, RouterID: "198.18.1.3", ExportRTs: []string{"65000:100"}, ImportRTs: []string{"65000:100"}, ToAdvertiseIPv4: []string{"203.0.113.0/24"}, ToAdvertiseIPv6: []string{"2001:db8:203::/64"}}}}
	var rendered string
	Expect(frr.ApplyConfig(context.Background(), config, func(_ context.Context, value string) error { rendered = value; return nil })).To(Succeed())
	return rendered
}

func checkMixedTransportPaths(left srv6GuardNode, paths []mixedTransportPath) {
	for _, path := range paths {
		Eventually(func() error { return path.packet(left.clientNS) }, 30*time.Second, 100*time.Millisecond).Should(Succeed(), "%s %s", path.transport, path.target)
		var fd int
		var closeCapture func()
		Expect(netnamespace.In(left.ns, func() error {
			vrf, err := lookupVRF("red")
			if err != nil {
				return err
			}
			routes, err := netlink.RouteListFiltered(path.family, &netlink.Route{Table: int(vrf.Table), Dst: guardCIDR(path.prefix)}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
			if err != nil {
				return err
			}
			Expect(routes).To(HaveLen(1))
			Expect(routes[0].Protocol).To(Equal(netlink.RouteProtocol(unix.RTPROT_BGP)))
			encap := routes[0].Encap
			if len(routes[0].MultiPath) > 0 {
				encap = routes[0].MultiPath[0].Encap
			}
			if path.transport == "srv6" {
				Expect(encap).To(BeAssignableToTypeOf(&netlink.SEG6Encap{}))
			} else {
				Expect(encap).To(BeNil())
			}
			port, err := netlink.LinkByName(left.transit)
			if err != nil {
				return err
			}
			fd, closeCapture = transportCaptureSocket(port.Attrs().Index)
			return nil
		})).To(Succeed())
		Expect(path.packet(left.clientNS)).To(Succeed())
		Expect(capturedTransportTarget(fd, path.transport, net.ParseIP(path.target))).To(BeTrue(), "%s wire capture for %s", path.transport, path.target)
		closeCapture()
	}
}

func checkMixedTransportWithdrawal(left srv6GuardNode, paths []mixedTransportPath, withdrawn string) {
	for _, path := range paths {
		if path.transport != withdrawn {
			continue
		}
		Eventually(func() (bool, error) { return mixedTransportRoutePresent(left, path) },
			15*time.Second, 100*time.Millisecond).Should(BeFalse())
	}

	for _, path := range paths {
		if path.transport != withdrawn {
			Eventually(func() error { return path.packet(left.clientNS) }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
			continue
		}
		Eventually(func() (bool, error) { return mixedTransportRoutePresent(left, path) },
			15*time.Second, 100*time.Millisecond).Should(BeFalse())
		Eventually(func() (bool, error) { return path.delivered(left.clientNS) },
			15*time.Second, 100*time.Millisecond).Should(BeFalse())
		Consistently(func() (bool, error) { return path.delivered(left.clientNS) },
			time.Second, 100*time.Millisecond).Should(BeFalse())
	}
}

func mixedTransportRoutePresent(left srv6GuardNode, path mixedTransportPath) (bool, error) {
	var present bool
	err := netnamespace.In(left.ns, func() error {
		vrf, err := lookupVRF("red")
		if err != nil {
			return err
		}
		routes, err := netlink.RouteListFiltered(path.family,
			&netlink.Route{Table: int(vrf.Table), Dst: guardCIDR(path.prefix)},
			netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
		if err != nil {
			return err
		}
		for _, route := range routes {
			present = present || route.Protocol == netlink.RouteProtocol(unix.RTPROT_BGP)
		}
		return nil
	})
	return present, err
}

func setMixedTransportNetworks(router *guardFRR, asn int, paths []mixedTransportPath, transport string, present bool) {
	for _, path := range paths {
		if path.transport != transport {
			continue
		}
		afi := "ipv4"
		if path.family == netlink.FAMILY_V6 {
			afi = "ipv6"
		}
		command := "network " + path.prefix
		if !present {
			command = "no " + command
		}
		_, err := router.vty("configure terminal", fmt.Sprintf("router bgp %d vrf red", asn), "address-family "+afi+" unicast", command)
		Expect(err).To(Succeed())
	}
}

func transportCaptureSocket(ifindex int) (int, func()) {
	protocol := uint16(unix.ETH_P_ALL)
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], protocol)
	networkProtocol := binary.NativeEndian.Uint16(encoded[:])
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, int(networkProtocol))
	Expect(err).To(Succeed())
	open := true
	closeCapture := func() {
		if !open {
			return
		}
		open = false
		Expect(unix.Close(fd)).To(Succeed())
	}
	DeferCleanup(closeCapture)
	Expect(unix.Bind(fd, &unix.SockaddrLinklayer{Ifindex: ifindex, Protocol: networkProtocol})).To(Succeed())
	return fd, closeCapture
}

func capturedTransportTarget(fd int, transport string, target net.IP) (bool, error) {
	var packet [4096]byte
	for {
		n, _, err := unix.Recvfrom(fd, packet[:], unix.MSG_DONTWAIT)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("receive %s transport capture: %w", transport, err)
		}
		if n < 64 {
			continue
		}
		offset := capturedTransportInnerOffset(packet[:n], transport)
		if offset < 0 {
			continue
		}
		if n < offset+20 {
			continue
		}
		if packet[offset]>>4 == 4 && net.IP(packet[offset+16:offset+20]).Equal(target) {
			return true, nil
		}
		if n >= offset+40 && packet[offset]>>4 == 6 && net.IP(packet[offset+24:offset+40]).Equal(target) {
			return true, nil
		}
	}
}

const mixedEVPNNeighbor = `
router bgp 65001
 neighbor 198.18.1.3 remote-as 65003
 neighbor 198.18.1.3 ebgp-multihop 10
 neighbor 198.18.1.3 timers connect 1
 address-family l2vpn evpn
  neighbor 198.18.1.3 activate
 exit-address-family
exit
router bgp 65001 vrf red
 address-family l2vpn evpn
  route-target import 65000:100
  route-target export 65000:100
 exit-address-family
exit
`

func capturedTransportInnerOffset(packet []byte, transport string) int {
	if transport == "vxlan" {
		if packet[14]>>4 != 4 || packet[23] != unix.IPPROTO_UDP {
			return -1
		}
		udp := 14 + int(packet[14]&15)*4
		if len(packet) < udp+4 || binary.BigEndian.Uint16(packet[udp+2:udp+4]) != 4789 {
			return -1
		}
		return udp + 8 + 8 + 14
	}
	if packet[14]>>4 != 6 {
		return -1
	}
	if packet[20] == unix.IPPROTO_ROUTING {
		return 54 + (int(packet[55])+1)*8
	}
	if packet[20] == unix.IPPROTO_IPV6 || packet[20] == unix.IPPROTO_IPIP {
		return 54
	}
	return -1
}

func checkUnusedLocatorRules(node srv6GuardNode, config *SRv6Isolation, present bool) {
	Expect(netnamespace.In(node.ns, func() error {
		current, err := listIsolationPolicyRules()
		if err != nil {
			return err
		}
		var actual []netlink.Rule
		for _, entry := range current {
			if ownedIsolationPolicyRule(entry) && entry.rule.Dst.String() == "fc00:99::/48" {
				actual = append(actual, entry.rule)
			}
		}
		if !present {
			Expect(actual).To(BeEmpty())
			return nil
		}
		Expect(actual).To(HaveLen(4))
		for _, iface := range []string{"red", "lo"} {
			var lookup, terminal *netlink.Rule
			for i := range actual {
				rule := &actual[i]
				Expect(rule.Src.IP.Equal(net.ParseIP(config.Source))).To(BeTrue())
				if rule.IifName != iface {
					continue
				}
				switch rule.Type {
				case unix.FR_ACT_TO_TBL:
					lookup = rule
				case unix.FR_ACT_UNREACHABLE:
					terminal = rule
				}
			}
			Expect(lookup).NotTo(BeNil())
			Expect(terminal).NotTo(BeNil())
			Expect(lookup.Table).To(Equal(int(config.UnderlayTable)))
			Expect(terminal.Table).To(BeZero())
			Expect(terminal.Priority).To(Equal(lookup.Priority + 1))
		}
		return nil
	})).To(Succeed())
}
