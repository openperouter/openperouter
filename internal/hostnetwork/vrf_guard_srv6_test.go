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

var _ = Describe("SRv6 dataplane with EVPN guards", func() {
	DescribeTable("forwards IPv4 and IPv6 through real BGP VPN/SRv6 routes",
		func(perFamilySID bool, encapBehavior string, mixed bool) {
			for _, binary := range []string{"/usr/lib/frr/mgmtd", "/usr/lib/frr/zebra", "/usr/lib/frr/bgpd"} {
				if _, err := os.Stat(binary); errors.Is(err, os.ErrNotExist) {
					Skip("real FRR binaries are required: " + binary)
				}
			}
			left := srv6GuardNode{
				ns: newGuardNamespace(), clientNS: newGuardNamespace(), id: 1, peerID: 2, transit: "transit",
				tenant4: "192.0.2.1/24", client4: "192.0.2.2/24",
				tenant6: "2001:db8:101::1/64", client6: "2001:db8:101::2/64",
			}
			right := srv6GuardNode{
				ns: newGuardNamespace(), clientNS: newGuardNamespace(), id: 2, peerID: 1, transit: "transit-peer",
				tenant4: "198.51.100.1/24", client4: "198.51.100.2/24",
				tenant6: "2001:db8:102::1/64", client6: "2001:db8:102::2/64",
			}
			Expect(netnamespace.In(left.ns, func() error {
				guardVeth(right.ns, left.transit, "198.18.0.1/24", "198.18.0.2/24", nil)
				return nil
			})).To(Succeed())
			left.setup()
			right.setup()
			if mixed {
				for _, node := range []srv6GuardNode{left, right} {
					reconcileSRv6TestNode(node, srv6TestIsolation(node))
				}
			}
			leftConfig := left.configuration(perFamilySID, encapBehavior)
			rightConfig := right.configuration(perFamilySID, encapBehavior)
			if mixed {
				leftConfig = strings.ReplaceAll(leftConfig, "blue", "red")
				rightConfig = strings.ReplaceAll(rightConfig, "blue", "red")
			}
			leftFRR := newGuardFRR(left.ns, leftConfig)
			rightFRR := newGuardFRR(right.ns, rightConfig)
			DeferCleanup(func() {
				if !CurrentSpecReport().Failed() {
					return
				}
				for _, router := range []*guardFRR{leftFRR, rightFRR} {
					output, _ := router.vty("show bgp ipv4 vpn", "show bgp ipv6 vpn",
						"show bgp vrf red ipv6 unicast", "show bgp nexthop",
						"show bgp neighbors",
						"show ip route vrf red", "show ipv6 route vrf red", "show ipv6 route",
						"show segment-routing srv6 locator", "show running-config")
					GinkgoWriter.Println(output)
				}
			})
			leftFRR.start()
			rightFRR.start()

			for _, node := range []*srv6GuardNode{&left, &right} {
				Expect(netnamespace.In(node.ns, func() error {
					transit, err := netlink.LinkByName(node.transit)
					Expect(err).To(Succeed())
					vniVRF := "blue"
					if mixed {
						vniVRF = "red"
					}
					blue, err := lookupVRF(vniVRF)
					Expect(err).To(Succeed())
					guardL3VNI(blue, transit.Attrs().Index, fmt.Sprintf("198.18.0.%d", node.id))
					var isolation *SRv6Isolation
					if mixed {
						isolation = srv6TestIsolation(*node)
					}
					for range 3 {
						Expect(setupVRF("blue", evpnVRF)).To(Succeed())
						Expect((&VNIConfigurator{ns: node.ns}).SetupL3VPN(context.Background(), L3VPNParams{
							VRF: "red", TargetNS: fmt.Sprintf("/proc/self/fd/%d", node.ns), RoutingIsolation: isolation,
						})).To(Succeed())
					}
					red, err := lookupVRF("red")
					Expect(err).To(Succeed())
					for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
						Expect(listVRFDefaultGuards(vrfDefaultGuard(blue.Table, family))).To(HaveLen(1))
						guards, err := listVRFDefaultGuards(vrfDefaultGuard(red.Table, family))
						Expect(err).To(Succeed())
						if mixed {
							Expect(guards).To(HaveLen(1))
						} else {
							Expect(guards).To(BeEmpty())
						}
					}
					return nil
				})).To(Succeed())
			}
			var packetFD int
			Expect(netnamespace.In(left.ns, func() error {
				transit, err := netlink.LinkByName(left.transit)
				Expect(err).To(Succeed())
				packetFD = srv6CaptureSocket(transit.Attrs().Index)
				return nil
			})).To(Succeed())
			DeferCleanup(func() { Expect(unix.Close(packetFD)).To(Succeed()) })
			seen := map[string]struct{}{}
			for _, path := range []struct {
				source   netns.NsHandle
				receiver netns.NsHandle
				family   int
				target   string
				sourceIP string
			}{
				{source: left.clientNS, receiver: right.clientNS, family: netlink.FAMILY_V4, target: "198.51.100.2", sourceIP: "192.0.2.2"},
				{source: left.clientNS, receiver: right.clientNS, family: netlink.FAMILY_V6, target: "2001:db8:102::2", sourceIP: "2001:db8:101::2"},
				{source: right.clientNS, receiver: left.clientNS, family: netlink.FAMILY_V4, target: "192.0.2.2", sourceIP: "198.51.100.2"},
				{source: right.clientNS, receiver: left.clientNS, family: netlink.FAMILY_V6, target: "2001:db8:101::2", sourceIP: "2001:db8:102::2"},
			} {
				Eventually(func() error {
					return netnamespace.In(path.source, func() error {
						return guardPacketVia(path.family, path.target, path.receiver, "")
					})
				}, 30*time.Second, 100*time.Millisecond).Should(Succeed())
				Eventually(func(g Gomega) {
					readSRv6Packets(g, packetFD, seen, encapBehavior)
					g.Expect(seen).To(HaveKey(path.sourceIP+">"+path.target),
						"directed UDP probe must be captured inside SRv6")
				}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			}

			if mixed {
				checkManagedSRv6Dataplane(left, right, leftFRR, rightFRR, perFamilySID, encapBehavior, mixed)
			}
		},
		Entry("per-VRF SID, H.Encaps", false, frr.HEncaps, false),
		Entry("per-family SIDs, H.Encaps", true, frr.HEncaps, false),
		Entry("per-VRF SID, H.Encaps.Red", false, frr.HEncapsRed, false),
		Entry("per-family SIDs, H.Encaps.Red", true, frr.HEncapsRed, false),
		Entry("mixed guarded VRF, H.Encaps", false, frr.HEncaps, true),
		Entry("mixed guarded VRF, H.Encaps.Red", false, frr.HEncapsRed, true),
		Entry("mixed guarded VRF, per-family SIDs, H.Encaps", true, frr.HEncaps, true),
		Entry("mixed guarded VRF, per-family SIDs, H.Encaps.Red", true, frr.HEncapsRed, true),
	)
})

type srv6GuardNode struct {
	ns, clientNS     netns.NsHandle
	id, peerID       int
	transit          string
	tenant4, client4 string
	tenant6, client6 string
}

func (n srv6GuardNode) setup() {
	Expect(netnamespace.In(n.ns, func() error {
		Expect(sysctl.Ensure(sysctl.IPv4Forwarding(), sysctl.IPv6Forwarding(),
			sysctl.EnableSeg6All(), sysctl.Seg6MakeFlowLabel(),
			sysctl.DisableRPFilter("all"), sysctl.DisableRPFilter("default"))).To(Succeed())
		lo, err := netlink.LinkByName("lo")
		Expect(err).To(Succeed())
		Expect(netlink.LinkSetUp(lo)).To(Succeed())
		guardAddress(lo, fmt.Sprintf("2001:db8:ffff::%d/128", n.id))
		transit, err := netlink.LinkByName(n.transit)
		Expect(err).To(Succeed())
		guardAddress(transit, fmt.Sprintf("2001:db8:12::%d/64", n.id))
		for _, destination := range []string{
			fmt.Sprintf("fc00:10:%d::/48", n.peerID),
			fmt.Sprintf("2001:db8:ffff::%d/128", n.peerID),
		} {
			route := netlink.Route{Table: unix.RT_TABLE_MAIN, Dst: guardCIDR(destination),
				Gw: net.ParseIP(fmt.Sprintf("2001:db8:12::%d", n.peerID)), LinkIndex: transit.Attrs().Index}
			Expect(netlink.RouteAdd(&route)).To(Succeed())
		}
		Expect(setupVRF("blue", evpnVRF)).To(Succeed())
		Expect((&VNIConfigurator{ns: n.ns}).SetupL3VPN(context.Background(), L3VPNParams{
			VRF: "red", TargetNS: fmt.Sprintf("/proc/self/fd/%d", n.ns),
		})).To(Succeed())
		vrf, err := lookupVRF("red")
		Expect(err).To(Succeed())
		tenant := guardVeth(n.clientNS, "tenant", n.tenant4, n.client4, vrf)
		guardAddress(tenant, n.tenant6)
		Expect(netnamespace.In(n.clientNS, func() error {
			port, err := netlink.LinkByName("tenant-peer")
			Expect(err).To(Succeed())
			guardAddress(port, n.client6)
			for _, family := range []struct {
				gateway string
				prefix  string
			}{{gateway: n.tenant4, prefix: "0.0.0.0/0"}, {gateway: n.tenant6, prefix: "::/0"}} {
				gateway, _, err := net.ParseCIDR(family.gateway)
				Expect(err).To(Succeed())
				route := netlink.Route{Dst: guardCIDR(family.prefix), Gw: gateway, LinkIndex: port.Attrs().Index}
				Expect(netlink.RouteAdd(&route)).To(Succeed())
			}
			return nil
		})).To(Succeed())
		return nil
	})).To(Succeed())
}

func (n srv6GuardNode) configuration(perFamilySID bool, encapBehavior string) string {
	peerASN, err := frr.NewPeerASN(new(int64(65000+n.peerID)), nil)
	Expect(err).To(Succeed())
	peerIP := fmt.Sprintf("2001:db8:ffff::%d", n.peerID)
	asn := int64(65000 + n.id)
	config := frr.Config{
		Loglevel: "warning",
		Underlay: frr.UnderlayConfig{
			MyASN: asn, RouterID: fmt.Sprintf("198.18.0.%d", n.id),
			TunnelEndpoint: &frr.TunnelEndpoint{
				IPv4CIDR: fmt.Sprintf("198.18.0.%d/32", n.id), IPv6CIDR: fmt.Sprintf("2001:db8:ffff::%d/128", n.id),
			},
			Neighbors: []frr.NeighborConfig{{
				ID: peerIP, Addr: peerIP, ASN: peerASN, ExtendedNexthop: true, ConnectTime: new(int64(1)),
				// Loopback multihop avoids advertising an unusable link-local VPN
				// nexthop when the peer has no directly connected interface.
				EBGPMultiHop: true,
				UpdateSource: fmt.Sprintf("2001:db8:ffff::%d", n.id),
				NetworkLayerProtocols: []networklayerprotocol.NLP{
					{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN},
					{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN},
					{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
				},
			}},
			SegmentRouting: &frr.UnderlaySegmentRouting{
				SourceAddress: fmt.Sprintf("2001:db8:ffff::%d", n.id), EncapBehavior: encapBehavior,
				EncapsulationSource: fmt.Sprintf("2001:db8:ffff::%d", n.id),
				Locator: frr.SRV6Locator{Name: "MAIN", Prefix: fmt.Sprintf("fc00:10:%d::/48", n.id),
					BlockLen: 32, NodeLen: 16, Behavior: "usid", Format: "usid-f3216"},
			},
		},
		L3VNIs: []frr.L3VNIConfig{{VRF: "blue", VNI: 100, ASN: asn, RouterID: fmt.Sprintf("198.18.0.%d", n.id)}},
		VPNs: []frr.L3VPNConfig{{
			VRF: "red", ASN: asn, RouterID: fmt.Sprintf("198.18.0.%d", n.id), UDT4UDT6: perFamilySID,
			RouteDistinguisher: fmt.Sprintf("%d:200", asn), ExportRTs: []string{"65000:200"},
			ImportRTs: []string{"65000:200"}, ToAdvertiseIPv4: []string{guardCIDR(n.tenant4).String()},
			ToAdvertiseIPv6: []string{guardCIDR(n.tenant6).String()},
		}},
	}
	var rendered string
	Expect(frr.ApplyConfig(context.Background(), config, func(_ context.Context, value string) error {
		rendered = value
		return nil
	})).To(Succeed())
	return rendered
}

func srv6CaptureSocket(ifindex int) int {
	var protocol [2]byte
	// IPv4 LWT encapsulation can retain IPv4 skb protocol metadata on egress.
	// Capture all protocols and validate the outer IPv6 header in the parser.
	binary.BigEndian.PutUint16(protocol[:], unix.ETH_P_ALL)
	etherType := binary.NativeEndian.Uint16(protocol[:])
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, int(etherType))
	Expect(err).To(Succeed())
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Ifindex: ifindex, Protocol: etherType}); err != nil {
		Expect(unix.Close(fd)).To(Succeed())
		Expect(err).To(Succeed())
	}
	return fd
}

func readSRv6Packets(g Gomega, fd int, seen map[string]struct{}, encapBehavior string) {
	buf := make([]byte, 4096)
	for {
		n, _, err := unix.Recvfrom(fd, buf, unix.MSG_DONTWAIT)
		if errors.Is(err, unix.EAGAIN) {
			return
		}
		g.Expect(err).To(Succeed())
		if probe := capturedSRv6Probe(buf[:n], encapBehavior); probe != "" {
			seen[probe] = struct{}{}
		}
	}
}

func capturedSRv6Probe(packet []byte, encapBehavior string) string {
	if len(packet) < 62 || packet[14]>>4 != 6 {
		return ""
	}
	offset := 54
	switch packet[20] {
	case unix.IPPROTO_ROUTING:
		if packet[56] != 4 {
			return ""
		}
		offset += (int(packet[55]) + 1) * 8
	case unix.IPPROTO_IPIP, unix.IPPROTO_IPV6:
		if encapBehavior != frr.HEncapsRed {
			return ""
		}
	default:
		return ""
	}
	if len(packet) < offset+20 {
		return ""
	}
	var source, destination net.IP
	udp := offset
	switch packet[offset] >> 4 {
	case 4:
		if packet[offset+9] != unix.IPPROTO_UDP {
			return ""
		}
		source, destination = packet[offset+12:offset+16], packet[offset+16:offset+20]
		udp += int(packet[offset]&15) * 4
	case 6:
		if len(packet) < offset+40 || packet[offset+6] != unix.IPPROTO_UDP {
			return ""
		}
		source, destination = packet[offset+8:offset+24], packet[offset+24:offset+40]
		udp += 40
	default:
		return ""
	}
	if len(packet) < udp+8 || binary.BigEndian.Uint16(packet[udp+2:udp+4]) != 17001 {
		return ""
	}
	if string(packet[udp+8:]) != "guard-probe" {
		return ""
	}
	return source.String() + ">" + destination.String()
}
