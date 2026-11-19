// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

var _ = Describe("EVPN VRF default guards with FRR", func() {
	It("preserves dual-stack isolation across BGP withdrawal and FRR restart without exporting guards", func() {
		for _, binary := range []string{"/usr/lib/frr/mgmtd", "/usr/lib/frr/zebra", "/usr/lib/frr/bgpd", "/usr/bin/vtysh"} {
			if _, err := os.Stat(binary); errors.Is(err, os.ErrNotExist) {
				Skip("real FRR binaries are required: " + binary)
			}
		}
		routerNS, peerNS, clientNS := newGuardNamespace(), newGuardNamespace(), newGuardNamespace()
		router := newGuardFRR(routerNS, guardRouterConfiguration())
		peer := newGuardFRR(peerNS, guardPeerConfig)
		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				output, _ := router.vty("show ip route vrf red", "show bgp vrf red ipv4 unicast",
					"show bgp vrf red summary", "show bgp nexthop", "show vrf", "show running-config")
				GinkgoWriter.Println(output)
			}
		})
		Expect(netnamespace.In(routerNS, func() error {
			lo, err := netlink.LinkByName("lo")
			Expect(err).To(Succeed())
			Expect(netlink.LinkSetUp(lo)).To(Succeed())
			Expect(setupVRF("red", evpnVRF)).To(Succeed())
			vrf, err := lookupVRF("red")
			Expect(err).To(Succeed())
			tenant := guardVeth(peerNS, "tenant", "192.0.2.1/24", "192.0.2.2/24", vrf)
			guardAddress(tenant, "2001:db8:1::1/64")
			main := guardVeth(peerNS, "outside", "198.18.0.1/24", "198.18.0.2/24", nil)
			guardAddress(main, "2001:db8:2::1/64")
			ingress := guardVeth(clientNS, "ingress", "198.19.0.1/24", "198.19.0.2/24", vrf)
			guardAddress(ingress, "2001:db8:3::1/64")
			Expect(sysctl.Ensure(sysctl.IPv4Forwarding(), sysctl.IPv6Forwarding(),
				sysctl.DisableRPFilter("all"), sysctl.DisableRPFilter("ingress"))).To(Succeed())
			Expect(netnamespace.In(clientNS, func() error {
				port, err := netlink.LinkByName("ingress-peer")
				Expect(err).To(Succeed())
				guardAddress(port, "2001:db8:3::2/64")
				for _, route := range []netlink.Route{
					{Dst: guardCIDR("0.0.0.0/0"), Gw: net.ParseIP("198.19.0.1"), LinkIndex: port.Attrs().Index},
					{Dst: guardCIDR("::/0"), Gw: net.ParseIP("2001:db8:3::1"), LinkIndex: port.Attrs().Index},
				} {
					Expect(netlink.RouteAdd(&route)).To(Succeed())
				}
				return nil
			})).To(Succeed())
			Expect(netnamespace.In(peerNS, func() error {
				lo, err := netlink.LinkByName("lo")
				Expect(err).To(Succeed())
				Expect(netlink.LinkSetUp(lo)).To(Succeed())
				guardAddress(lo, "203.0.113.9/32")
				guardAddress(lo, "2001:db8:ffff::9/128")
				tenant, err := netlink.LinkByName("tenant-peer")
				Expect(err).To(Succeed())
				guardAddress(tenant, "2001:db8:1::2/64")
				outside, err := netlink.LinkByName("outside-peer")
				Expect(err).To(Succeed())
				guardAddress(outside, "2001:db8:2::2/64")
				return nil
			})).To(Succeed())
			for _, route := range []netlink.Route{
				{Dst: guardCIDR("203.0.113.9/32"), Gw: net.ParseIP("198.18.0.2"), LinkIndex: main.Attrs().Index},
				{Dst: guardCIDR("2001:db8:ffff::9/128"), Gw: net.ParseIP("2001:db8:2::2"), LinkIndex: main.Attrs().Index},
			} {
				Expect(netlink.RouteAdd(&route)).To(Succeed())
			}

			By("proving the unprotected socket and forwarded paths through main before enabling guards")
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				guards, err := listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))
				Expect(err).To(Succeed())
				Expect(guards).To(HaveLen(1))
				Expect(netlink.RouteDel(&guards[0])).To(Succeed())
			}
			guardCheckDelivery(peerNS, clientNS, true)
			Expect(ensureVRFDefaultGuards(vrf)).To(Succeed())

			By("starting zebra and bgpd with guards already installed")
			router.start()
			peer.start()
			// Match production ordering: configure FRR's EVPN instance before VXLAN appears.
			Eventually(func() error { _, err := router.vty("show bgp l2vpn evpn"); return err },
				20*time.Second, 100*time.Millisecond).Should(Succeed())
			guardL3VNI(vrf, main.Attrs().Index, "198.18.0.1")
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				command := "show ip route vrf red 0.0.0.0/0"
				if family == netlink.FAMILY_V6 {
					command = "show ipv6 route vrf red ::/0"
				}
				Eventually(func() (string, error) { return router.vty(command) },
					20*time.Second, 100*time.Millisecond).Should(ContainSubstring("distance 255, metric 8192"))
			}
			guardCheckDelivery(peerNS, clientNS, false)

			By("learning specific routes from a real BGP peer")
			Expect(peer.configure("address-family ipv4 unicast", "network 203.0.113.9/32")).To(Succeed())
			Expect(peer.configure("address-family ipv6 unicast", "network 2001:db8:ffff::9/128")).To(Succeed())
			guardCheckDelivery(peerNS, clientNS, true)
			Expect(peer.configure("address-family ipv4 unicast", "no network 203.0.113.9/32")).To(Succeed())
			Expect(peer.configure("address-family ipv6 unicast", "no network 2001:db8:ffff::9/128")).To(Succeed())
			guardCheckDelivery(peerNS, clientNS, false)

			By("preferring real BGP defaults and keeping both kernel guards during reconciliation")
			Expect(peer.configure("address-family ipv4 unicast", "network 0.0.0.0/0")).To(Succeed())
			Expect(peer.configure("address-family ipv6 unicast", "network ::/0")).To(Succeed())
			guardCheckDelivery(peerNS, clientNS, true)
			changes := make(chan netlink.RouteUpdate, 1024)
			done := make(chan struct{})
			Expect(netlink.RouteSubscribeAt(routerNS, changes, done)).To(Succeed())
			defer close(done)
			Consistently(func(g Gomega) {
				g.Expect(setupVRF("red", evpnVRF)).To(Succeed())
				for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
					g.Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(HaveLen(1))
					filter := &netlink.Route{Table: int(vrf.Table), Protocol: unix.RTPROT_BGP}
					g.Expect(netlink.RouteListFiltered(family, filter,
						netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST|netlink.RT_FILTER_PROTOCOL)).To(HaveLen(1))
				}
				for {
					select {
					case update, ok := <-changes:
						g.Expect(ok).To(BeTrue(), "route subscription closed")
						if update.Table == int(vrf.Table) && update.Protocol == vrfGuardProtocol {
							g.Expect(update.Priority).NotTo(Equal(vrfGuardPriority), "guard was rewritten during reconciliation")
						}
					default:
						return
					}
				}
			}, 3*time.Second, 100*time.Millisecond).Should(Succeed())

			By("restarting both router daemons and recovering BGP without losing the guards")
			router.stop()
			guardCheckDelivery(peerNS, clientNS, false)
			for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
				Expect(listVRFDefaultGuards(vrfDefaultGuard(vrf.Table, family))).To(HaveLen(1))
			}
			router.start()
			guardCheckDelivery(peerNS, clientNS, true)

			Expect(peer.configure("address-family ipv4 unicast", "no network 0.0.0.0/0")).To(Succeed())
			Expect(peer.configure("address-family ipv6 unicast", "no network ::/0")).To(Succeed())
			guardCheckDelivery(peerNS, clientNS, false)

			By("checking real BGP and EVPN advertisements after withdrawal")
			output, err := router.vty("show evpn vni", "show vrf", "show running-config")
			Expect(err).To(Succeed())
			GinkgoWriter.Println(output)
			for _, command := range []string{
				"show bgp vrf red ipv4 unicast neighbors 192.0.2.2 advertised-routes",
				"show bgp vrf red ipv6 unicast neighbors 192.0.2.2 advertised-routes",
				"show bgp l2vpn evpn neighbors 198.18.0.2 advertised-routes",
			} {
				wanted := "198.51.100.0"
				if strings.Contains(command, "ipv6") {
					wanted = "2001:db8:100::"
				}
				Eventually(func(g Gomega) {
					output, err := router.vty(command)
					g.Expect(err).To(Succeed())
					g.Expect(output).To(ContainSubstring(wanted))
					g.Expect(output).NotTo(ContainSubstring("0.0.0.0/0"))
					g.Expect(output).NotTo(ContainSubstring("::/0"))
					g.Expect(output).NotTo(ContainSubstring("[5]:[0]:[0]:"))
					if strings.Contains(command, "l2vpn evpn") {
						g.Expect(output).To(ContainSubstring("2001:db8:100::"))
					}
				}, 20*time.Second, 100*time.Millisecond).Should(Succeed())
			}
			return nil
		})).To(Succeed())
	})
})

type guardFRR struct {
	ns      netns.NsHandle
	dir     string
	process []*exec.Cmd
}

func newGuardFRR(ns netns.NsHandle, config string) *guardFRR {
	dir, err := os.MkdirTemp("", "openperouter-guard-frr-")
	if dir != "" {
		DeferCleanup(func() { Expect(os.RemoveAll(dir)).To(Succeed()) })
	}
	Expect(err).To(Succeed())
	frrUser, err := user.Lookup("frr")
	Expect(err).To(Succeed())
	uid, err := strconv.Atoi(frrUser.Uid)
	Expect(err).To(Succeed())
	gid, err := strconv.Atoi(frrUser.Gid)
	Expect(err).To(Succeed())
	Expect(os.Chown(dir, uid, gid)).To(Succeed())
	Expect(os.Chmod(dir, 0755)).To(Succeed())
	paths := []string{
		filepath.Join("/var/run/frr", filepath.Base(dir)),
		filepath.Join("/var/lib/frr", filepath.Base(dir)),
		filepath.Join("/etc/frr", filepath.Base(dir)),
	}
	for _, path := range paths {
		err := os.MkdirAll(path, 0755)
		DeferCleanup(func() { Expect(os.RemoveAll(path)).To(Succeed()) })
		Expect(err).To(Succeed())
		Expect(os.Chown(path, uid, gid)).To(Succeed())
	}
	Expect(os.WriteFile(filepath.Join(paths[2], "frr.conf"), []byte(config), 0644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(paths[2], "vtysh.conf"), nil, 0644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, "frr.conf"), []byte(config), 0644)).To(Succeed())
	frr := &guardFRR{ns: ns, dir: dir}
	DeferCleanup(func() {
		frr.stop()
		if CurrentSpecReport().Failed() {
			for _, daemon := range []string{"mgmtd", "zebra", "bgpd"} {
				data, _ := os.ReadFile(filepath.Join(dir, daemon+".log"))
				GinkgoWriter.Printf("%s %s:\n%s\n", dir, daemon, data)
			}
		}
	})
	return frr
}

func (f *guardFRR) start() {
	for _, daemon := range []string{"mgmtd", "zebra", "bgpd"} {
		f.startDaemon(daemon)
	}
	Eventually(func(g Gomega) {
		for _, daemon := range []string{"mgmtd", "zebra", "bgpd"} {
			_, err := os.Stat(filepath.Join("/var/run/frr", filepath.Base(f.dir), daemon+".vty"))
			g.Expect(err).To(Succeed())
		}
	}, 20*time.Second, 100*time.Millisecond).Should(Succeed())
	// VRF/VNI commands in FRR 10.6 are managed by mgmtd. Apply the integrated
	// configuration after all backends connect, as FRR's normal startup does.
	Eventually(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "/usr/bin/vtysh", "-N", filepath.Base(f.dir),
			"-f", filepath.Join(f.dir, "frr.conf")).CombinedOutput()
		if err != nil {
			return fmt.Errorf("load FRR configuration: %w: %s", errors.Join(err, ctx.Err()), output)
		}
		return nil
	}, 20*time.Second, 100*time.Millisecond).Should(Succeed())
}

func (f *guardFRR) stop() {
	var stopErrors []error
	for i := len(f.process) - 1; i >= 0; i-- {
		stopErrors = append(stopErrors, f.stopDaemon(i))
	}
	f.process = nil
	Expect(errors.Join(stopErrors...)).To(Succeed())
}

func (f *guardFRR) startDaemon(daemon string) {
	Expect(netnamespace.In(f.ns, func() error {
		logFile, err := os.OpenFile(filepath.Join(f.dir, daemon+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		Expect(err).To(Succeed())
		defer func() { Expect(logFile.Close()).To(Succeed()) }()
		args := []string{"-N", filepath.Base(f.dir), "-i", filepath.Join(f.dir, daemon+".pid"), "--log", "stdout"}
		if daemon == "bgpd" {
			args = append(args, "-f", filepath.Join(f.dir, "frr.conf"))
		}
		cmd := exec.Command("/usr/lib/frr/"+daemon, args...)
		cmd.Stdout, cmd.Stderr = logFile, logFile
		Expect(cmd.Start()).To(Succeed())
		f.process = append(f.process, cmd)
		return nil
	})).To(Succeed())
}

func (f *guardFRR) stopDaemon(index int) error {
	cmd := f.process[index]
	var stopErrors []error
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		stopErrors = append(stopErrors, fmt.Errorf("signal FRR: %w", err))
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	var waitErr error
	killed := false
	select {
	case waitErr = <-waited:
	case <-timer.C:
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			stopErrors = append(stopErrors, fmt.Errorf("kill FRR: %w", err))
		}
		killed = true
		select {
		case waitErr = <-waited:
		case <-time.After(3 * time.Second):
			stopErrors = append(stopErrors, fmt.Errorf("FRR process %d did not exit after kill", cmd.Process.Pid))
			return errors.Join(stopErrors...)
		}
	}
	if !expectedFRRExit(waitErr, killed) {
		stopErrors = append(stopErrors, fmt.Errorf("wait FRR: %w", waitErr))
	}
	return errors.Join(stopErrors...)
}

func expectedFRRExit(err error, killed bool) bool {
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return false
	}
	if status.Signal() == syscall.SIGTERM {
		return true
	}
	return killed && status.Signal() == syscall.SIGKILL
}

func (f *guardFRR) vty(commands ...string) (string, error) {
	args := make([]string, 0, 2+2*len(commands))
	args = append(args, "-N", filepath.Base(f.dir))
	for _, command := range commands {
		args = append(args, "-c", command)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/bin/vtysh", args...).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("vtysh: %w: %s", errors.Join(err, ctx.Err()), output)
	}
	return string(output), nil
}

func (f *guardFRR) configure(family, network string) error {
	_, err := f.vty("configure terminal", "router bgp 65002", family, network)
	return err
}

func guardCIDR(cidr string) *net.IPNet {
	_, prefix, err := net.ParseCIDR(cidr)
	Expect(err).To(Succeed())
	return prefix
}

func guardCheckDelivery(peerNS, clientNS netns.NsHandle, allowed bool) {
	for _, probe := range []struct {
		family int
		target string
	}{{netlink.FAMILY_V4, "203.0.113.9"}, {netlink.FAMILY_V6, "2001:db8:ffff::9"}} {
		paths := []func() (bool, error){
			func() (bool, error) { return guardPacketViaResult(probe.family, probe.target, peerNS, "red") },
			func() (bool, error) {
				var delivered bool
				err := netnamespace.In(clientNS, func() error {
					var probeErr error
					delivered, probeErr = guardPacketViaResult(probe.family, probe.target, peerNS, "")
					return probeErr
				})
				return delivered, err
			},
		}
		for _, path := range paths {
			if allowed {
				Eventually(path, 20*time.Second, 100*time.Millisecond).Should(BeTrue())
				continue
			}
			Eventually(path, 20*time.Second, 100*time.Millisecond).Should(BeFalse())
			Consistently(path, time.Second, 100*time.Millisecond).Should(BeFalse())
		}
	}
}

func guardL3VNI(vrf *netlink.Vrf, underlayIndex int, source string) {
	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{
		Name: "br100", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 1, 0},
	}}
	Expect(netlink.LinkAdd(bridge)).To(Succeed())
	Expect(netlink.LinkSetMaster(bridge, vrf)).To(Succeed())
	Expect(setAddrGenModeNone(bridge)).To(Succeed())
	Expect(netlink.LinkSetUp(bridge)).To(Succeed())
	vxlan := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{Name: "vx100"}, VxlanId: 100,
		VtepDevIndex: underlayIndex, SrcAddr: net.ParseIP(source), Port: 4789,
	}
	Expect(netlink.LinkAdd(vxlan)).To(Succeed())
	Expect(netlink.LinkSetMaster(vxlan, bridge)).To(Succeed())
	Expect(setAddrGenModeNone(vxlan)).To(Succeed())
	Expect(netlink.LinkSetUp(vxlan)).To(Succeed())
}

func guardRouterConfiguration() string {
	peerASN, err := frr.NewPeerASN(new(int64(65002)), nil)
	Expect(err).To(Succeed())
	config := frr.Config{
		Loglevel: "warning",
		Underlay: frr.UnderlayConfig{
			MyASN: 65001, RouterID: "198.18.0.1",
			TunnelEndpoint: &frr.TunnelEndpoint{IPv4CIDR: "198.18.0.1/32"},
			Neighbors: []frr.NeighborConfig{{
				ID: "198.18.0.2", Addr: "198.18.0.2", ASN: peerASN,
				NetworkLayerProtocols: []networklayerprotocol.NLP{
					{AFI: networklayerprotocol.L2VPN, SAFI: networklayerprotocol.EVPN},
				},
			}},
		},
		L3VNIs: []frr.L3VNIConfig{{
			VRF: "red", VNI: 100, ASN: 65001, RouterID: "192.0.2.1",
			LocalNeighbor:   &frr.NeighborConfig{ID: "192.0.2.2", Addr: "192.0.2.2", ASN: peerASN},
			ToAdvertiseIPv4: []string{"198.51.100.0/24"},
			ToAdvertiseIPv6: []string{"2001:db8:100::/64"},
		}},
	}
	var rendered string
	Expect(frr.ApplyConfig(context.Background(), config, func(_ context.Context, value string) error {
		rendered = value
		return nil
	})).To(Succeed())
	return rendered
}

const guardPeerConfig = `
hostname guard-peer
router bgp 65002
 bgp router-id 198.18.0.2
 no bgp ebgp-requires-policy
 no bgp network import-check
 no bgp default ipv4-unicast
 neighbor 198.18.0.1 remote-as 65001
 neighbor 198.18.0.1 timers connect 1
 neighbor 192.0.2.1 remote-as 65001
 neighbor 192.0.2.1 timers connect 1
 address-family ipv4 unicast
  neighbor 192.0.2.1 activate
 exit-address-family
 address-family ipv6 unicast
  neighbor 192.0.2.1 activate
 exit-address-family
 address-family l2vpn evpn
  neighbor 198.18.0.1 activate
 exit-address-family
exit
`
