// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Tenant VRF routing isolation", Serial, PodModeOnly, func() {
	const namespace = "test-vrf-isolation"
	var cs clientset.Interface
	var sinkRouter openperouter.RouterExecutor

	BeforeEach(func() {
		cs = k8sclient.New()
		sinkRouter = nil
		nodes, err := k8s.GetNodes(cs)
		Expect(err).To(Succeed())
		for _, node := range nodes {
			Expect(cleanupIsolationClusterSink(executor.ForNode(node.Name))).To(Succeed())
		}
		_, err = cs.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			Expect(err).To(Succeed())
			Expect(k8s.DeleteNamespace(cs, namespace)).To(Succeed())
		}
		Expect(Updater.CleanAll()).To(Succeed())
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafSRV6Config.Reset()).To(Succeed())
		old, err := openperouter.Get(cs, false)
		Expect(err).To(Succeed())
		pods, err := openperouter.RouterPods(cs)
		Expect(err).To(Succeed())
		for _, pod := range pods {
			Expect(cs.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})).To(Succeed())
		}
		Eventually(func() error {
			current, err := openperouter.Get(cs, false)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(old, current)
		}, 3*time.Minute, time.Second).Should(Succeed())
	})
	AfterEach(func() {
		dumpIfFails(cs, namespace)
		var cleanupErrors []error
		if sinkRouter != nil {
			cleanupErrors = append(cleanupErrors,
				cleanupIsolationClusterSink(executor.ForNode(sinkRouter.NodeName())))
		}
		cleanupErrors = append(cleanupErrors, Updater.CleanAll())
		cleanupErrors = append(cleanupErrors, infra.LeafAConfig.Reset())
		cleanupErrors = append(cleanupErrors, infra.LeafSRV6Config.Reset())
		if err := k8s.DeleteNamespace(cs, namespace); !apierrors.IsNotFound(err) {
			cleanupErrors = append(cleanupErrors, err)
		}
		Expect(errors.Join(cleanupErrors...)).To(Succeed())
	})

	DescribeTable("keeps unknown traffic in its table while FRR forwards working routes", func(mode isolationClusterMode) {
		nodes, err := k8s.GetNodes(cs)
		Expect(err).To(Succeed())
		Expect(nodes).NotTo(BeEmpty())
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		underlayKind := infra.Default
		if mode.srv6 {
			underlayKind = infra.EVPNandSRv6
		}
		underlays := isolationClusterUnderlays(nodes, underlayKind, nodes[0].Name)
		Expect(Updater.Update(config.Resources{Underlays: underlays})).To(Succeed())
		resources := isolationClusterResources(mode, nodes[0].Name)
		Expect(Updater.Update(resources)).To(Succeed())

		_, err = k8s.CreateNamespace(cs, namespace)
		Expect(err).To(Succeed())
		nad, err := k8s.CreateMacvlanNad("tenant", namespace, "br-hs-227",
			[]string{"192.171.227.1/24", "2001:db8:227:1::1/64"})
		Expect(err).To(Succeed())
		pod, err := k8s.CreateAgnhostPod(cs, "client", namespace,
			k8s.OnNode(nodes[0].Name), k8s.WithNad(nad.Name, namespace,
				[]string{"192.171.227.2/24", "2001:db8:227:1::2/64"}))
		Expect(err).To(Succeed())
		Expect(removeGatewayFromPod(pod)).To(Succeed())
		client := executor.ForPod(namespace, pod.Name, "agnhost")
		routers, err := openperouter.Get(cs, HostMode)
		Expect(err).To(Succeed())
		router, err := routers.ExecutorForNode(pod.Spec.NodeName)
		Expect(err).To(Succeed())

		By("checking guards in the actual VRF table and real main-table control packets")
		Eventually(func() error { return isolationClusterGuards(router) }, time.Minute, time.Second).Should(Succeed())
		sinkRouter = router
		isolationClusterSink(router)
		checkIsolationClusterUnknown(router, client)

		paths := isolationClusterPaths(mode)
		for _, path := range paths {
			configureIsolationClusterLeaf(path.leaf)
			Expect(path.leaf.ChangePrefixes(nil, path.prefixes, nil)).To(Succeed())
		}
		checkIsolationClusterPaths(router, client, paths)
		checkIsolationClusterExports(paths)

		By("preserving forwarding after a completed reconciliation of unchanged configuration")
		status, err := openperouter.GetNodeStatus(Updater.Client(), pod.Spec.NodeName)
		Expect(err).To(Succeed())
		oldUID := status.UID
		// Status deletion is watched by the node controller without changing routes.
		Expect(Updater.Client().Delete(context.Background(), status)).To(Succeed())
		Eventually(func(g Gomega) {
			current, err := openperouter.GetNodeStatus(Updater.Client(), pod.Spec.NodeName)
			g.Expect(err).To(Succeed())
			g.Expect(current.UID).NotTo(Equal(oldUID))
			g.Expect(current.Status).NotTo(BeNil())
			g.Expect(apimeta.IsStatusConditionTrue(current.Status.Conditions, v1alpha1.ConditionTypeReady)).To(BeTrue())
			g.Expect(current.Status.FailedResources).To(BeEmpty())
		}, time.Minute, time.Second).Should(Succeed())
		checkIsolationClusterPaths(router, client, paths)
		if mode.srv6 {
			By("forwarding after adding and removing an unused locator selector")
			resources.L3VPNs[0].Spec.RoutingIsolation.RemoteLocatorPrefixes = []string{"fd00:0:10::/48", "fd00:ffff::/48"}
			Expect(Updater.Update(resources)).To(Succeed())
			Eventually(func() (string, error) { return router.Exec("ip", "-6", "rule", "show") },
				time.Minute, time.Second).Should(ContainSubstring("fd00:ffff::/48"))
			checkIsolationClusterPaths(router, client, paths)
			resources.L3VPNs[0].Spec.RoutingIsolation.RemoteLocatorPrefixes = []string{"fd00:0:10::/48"}
			Expect(Updater.Update(resources)).To(Succeed())
			Eventually(func() (string, error) { return router.Exec("ip", "-6", "rule", "show") },
				time.Minute, time.Second).ShouldNot(ContainSubstring("fd00:ffff::/48"))
			checkIsolationClusterPaths(router, client, paths)
		}

		checkIsolationClusterWithdrawal(router, client, paths)
		checkIsolationClusterExports(paths)
		checkIsolationClusterUnknown(router, client)
	},
		Entry("EVPN only, including the first tenant VRF", isolationClusterMode{evpn: true}),
		Entry("guarded SRv6", isolationClusterMode{srv6: true}),
		Entry("EVPN and SRv6 in the same tenant VRF", isolationClusterMode{evpn: true, srv6: true}),
	)
})

type isolationClusterMode struct{ evpn, srv6 bool }

type isolationClusterPath struct {
	name     string
	leaf     infra.Leaf
	prefixes []string
	targets  []string
}

func isolationClusterUnderlays(nodes []corev1.Node, kind infra.UnderlayKind, tenantNode string) []v1alpha1.Underlay {
	underlays := make([]v1alpha1.Underlay, 0, len(nodes))
	for _, node := range nodes {
		u := infra.Underlay(kind)
		u.Name = "isolation-" + node.Name
		u.Spec.NodeSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": node.Name}}
		if u.Spec.SRV6 != nil && node.Name == tenantNode {
			// Only the selected tenant PE consumes an explicit isolation source.
			source, err := openperouter.GetVtepIPForNode(u.Spec.TunnelEndpoint, &node, ipfamily.IPv6)
			Expect(err).To(Succeed())
			source = netip.MustParsePrefix(source).Addr().String()
			u.Spec.SRV6.EncapsulationSource = &source
		}
		underlays = append(underlays, u)
	}
	return underlays
}

func isolationClusterResources(mode isolationClusterMode, node string) config.Resources {
	// The tenant LAN is attached to one PE. Advertising the same connected LAN
	// from empty tenant segments on other nodes would create ambiguous return ECMP.
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/hostname": node}}
	domain := l3vniRoutingDomain("red")
	var resources config.Resources
	if mode.evpn {
		resources.L3VNIs = []v1alpha1.L3VNI{{ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
			Spec: v1alpha1.L3VNISpec{NodeSelector: selector, VRF: "red", VNI: 100}}}
	}
	if mode.srv6 {
		domain = l3vpnRoutingDomain("red")
		resources.L3VPNs = []v1alpha1.L3VPN{{ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
			Spec: v1alpha1.L3VPNSpec{NodeSelector: selector, VRF: "red", RDAssignedNumber: 100,
				ExportRTs: []v1alpha1.RouteTarget{"64514:100"}, ImportRTs: []v1alpha1.RouteTarget{"64520:100"},
				RoutingIsolation: &v1alpha1.SRV6RoutingIsolation{UnderlayTable: 254, RemoteLocatorPrefixes: []string{"fd00:0:10::/48"}}}}}
	}
	resources.L2VNIs = []v1alpha1.L2VNI{{ObjectMeta: metav1.ObjectMeta{Name: "tenant", Namespace: openperouter.Namespace},
		Spec: v1alpha1.L2VNISpec{NodeSelector: selector, VNI: 227, RoutingDomain: domain,
			GatewayIPs: []string{"192.171.227.1/24", "2001:db8:227:1::1/64"},
			HostMaster: &v1alpha1.HostMaster{Type: v1alpha1.LinuxBridge,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{Lifecycle: v1alpha1.BridgeLifecycleManaged}}}}}
	return resources
}

func isolationClusterPaths(mode isolationClusterMode) []isolationClusterPath {
	var paths []isolationClusterPath
	if mode.evpn {
		paths = append(paths, isolationClusterPath{name: "EVPN", leaf: infra.LeafAConfig,
			prefixes: []string{"192.168.20.0/24", "2001:db8:20::/64"},
			targets:  []string{infra.HostARedIPv4, infra.HostARedIPv6}})
	}
	if mode.srv6 {
		paths = append(paths, isolationClusterPath{name: "SRv6", leaf: infra.LeafSRV6Config,
			prefixes: []string{"192.170.20.0/24", "2001:db8:170:20::/64"},
			targets:  []string{infra.HostSRV6RedIPv4, infra.HostSRV6RedIPv6}})
	}
	return paths
}

func configureIsolationClusterLeaf(leaf infra.Leaf) {
	GinkgoHelper()
	// These disposable fabric routers must accept asymmetric VPN traffic.
	// Docker defaults can enable strict IPv4 RPF on both all and the VRF,
	// dropping decapsulated packets before they reach the attached host.
	exec := executor.ForContainer(leaf.Name)
	for _, key := range []string{"net.ipv4.conf.all.rp_filter", "net.ipv4.conf.red.rp_filter"} {
		previous, err := exec.Exec("sysctl", "-n", key)
		Expect(err).To(Succeed())
		_, err = exec.Exec("sysctl", "-w", key+"=0")
		Expect(err).To(Succeed())
		DeferCleanup(func() {
			_, err := exec.Exec("sysctl", "-w", key+"="+strings.TrimSpace(previous))
			Expect(err).To(Succeed())
		})
	}
}

func isolationClusterGuards(router executor.Executor) error {
	for _, family := range []string{"-4", "-6"} {
		out, err := router.Exec("ip", "-j", family, "route", "show", "vrf", "red", "exact", "default")
		if err != nil {
			return err
		}
		var routes []struct {
			Type   string `json:"type"`
			Metric uint64 `json:"metric"`
		}
		if err := json.Unmarshal([]byte(out), &routes); err != nil {
			return err
		}
		count := 0
		for _, route := range routes {
			if route.Type == "unreachable" && route.Metric == 4278198272 {
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("want one %s guard in actual red table, got %s", family, out)
		}
	}
	return nil
}

func isolationClusterBGPRoute(router executor.Executor, prefix string, present bool) error {
	family := "ip"
	if netip.MustParsePrefix(prefix).Addr().Is6() {
		family = "ipv6"
	}
	out, err := router.Exec("vtysh", "-c", "show "+family+" route vrf red "+prefix+" json")
	if err != nil {
		return err
	}
	var routes map[string][]struct {
		Protocol  string `json:"protocol"`
		Installed bool   `json:"installed"`
	}
	if err := json.Unmarshal([]byte(out), &routes); err != nil {
		return err
	}
	found := false
	for _, route := range routes[prefix] {
		found = found || route.Protocol == "bgp" && route.Installed
	}
	if found != present {
		return fmt.Errorf("BGP FIB presence for %s got %t, want %t: %s", prefix, found, present, out)
	}
	return nil
}

func isolationClusterPing(exec executor.Executor, target string) (bool, error) {
	// Keep kubectl/exec failures separate from ping's expected unreachable status.
	out, err := exec.Exec("sh", "-c", `ping -c 1 -W 1 -I net1 "$1"; rc=$?; case "$rc" in 0|1|2) printf '\nPING_RESULT=%s\n' "$rc";; *) exit "$rc";; esac`, "sh", target)
	if err != nil {
		return false, fmt.Errorf("execute tenant ping %s: %w: %s", target, err, out)
	}
	switch {
	case strings.HasSuffix(out, "\nPING_RESULT=0\n"):
		return true, nil
	case strings.HasSuffix(out, "\nPING_RESULT=1\n"), strings.HasSuffix(out, "\nPING_RESULT=2\n"):
		return false, nil
	default:
		return false, fmt.Errorf("missing tenant ping result for %s: %s", target, out)
	}
}

func checkIsolationClusterPaths(router, client executor.Executor, paths []isolationClusterPath) {
	GinkgoHelper()
	for _, path := range paths {
		for i, prefix := range path.prefixes {
			Eventually(func() error { return isolationClusterBGPRoute(router, prefix, true) },
				time.Minute, time.Second).Should(Succeed())
			var vxlanBefore uint64
			if path.name == "EVPN" {
				vxlanBefore = isolationClusterTXPackets(router, "vni100")
			}
			Eventually(func(g Gomega) bool {
				passed, err := isolationClusterPing(client, path.targets[i])
				g.Expect(err).To(Succeed())
				return passed
			},
				time.Minute, time.Second).Should(BeTrue(), path.name)
			if path.name == "EVPN" {
				Expect(isolationClusterTXPackets(router, "vni100")).To(BeNumerically(">", vxlanBefore),
					"EVPN delivery must use the VXLAN device")
			}
			if path.name == "SRv6" {
				family := "-4"
				if i == 1 {
					family = "-6"
				}
				out, err := router.Exec("ip", family, "route", "show", "vrf", "red", "exact", prefix)
				Expect(err).To(Succeed())
				Expect(out).To(ContainSubstring("encap seg6"))
			}
		}
	}
}

func checkIsolationClusterExports(paths []isolationClusterPath) {
	GinkgoHelper()
	for _, path := range paths {
		leaf := executor.ForContainer(path.leaf.Name)
		for _, family := range []string{"ipv4", "ipv6"} {
			Eventually(func() (string, error) {
				return leaf.Exec("vtysh", "-c", "show bgp vrf red "+family+" unicast")
			}, time.Minute, time.Second).Should(ContainSubstring(map[string]string{
				"ipv4": "192.171.227.0/24", "ipv6": "2001:db8:227:1::/64",
			}[family]), "legitimate gateway prefix must still be exported")
			out, err := leaf.Exec("vtysh", "-c", "show bgp vrf red "+family+" unicast")
			Expect(err).To(Succeed())
			Expect(out).NotTo(ContainSubstring("0.0.0.0/0"))
			Expect(out).NotTo(ContainSubstring("::/0"))
		}
	}
}

func isolationClusterSink(router openperouter.RouterExecutor) {
	GinkgoHelper()
	// The FRR pod's netns directory is read-only. Create the fixture from the
	// Kind node, keeping the pod's existing mount and capability restrictions.
	node := executor.ForNode(router.NodeName())
	out, err := node.Exec("ip", "netns", "add", "guard-sink")
	Expect(err).To(Succeed(), out)
	for _, args := range [][]string{
		{"link", "add", "guard-main", "type", "veth", "peer", "name", "guard-peer"},
		{"link", "set", "guard-main", "addrgenmode", "none"},
		{"link", "set", "guard-peer", "addrgenmode", "none"},
		{"link", "set", "guard-peer", "netns", "guard-sink"},
		{"addr", "add", "198.18.227.1/30", "dev", "guard-main"},
		{"-6", "addr", "add", "2001:db8:227:2::1/64", "dev", "guard-main", "nodad"},
		{"link", "set", "guard-main", "up"},
	} {
		out, err := router.Exec("ip", args...)
		Expect(err).To(Succeed(), out)
	}
	for _, args := range [][]string{
		{"addr", "add", "198.18.227.2/30", "dev", "guard-peer"},
		{"-6", "addr", "add", "2001:db8:227:2::2/64", "dev", "guard-peer", "nodad"},
		{"link", "set", "guard-peer", "up"},
	} {
		out, err := node.Exec("ip", append([]string{"netns", "exec", "guard-sink", "ip"}, args...)...)
		Expect(err).To(Succeed(), out)
	}
	// Count only the probe destinations. Unsolicited IPv6 multicast reports
	// can increment the link's total TX counter after address configuration.
	out, err = node.Exec("ip", "netns", "exec", openperouter.NamedNetns,
		"tc", "qdisc", "add", "dev", "guard-main", "clsact")
	Expect(err).To(Succeed(), out)
	for _, probe := range []struct{ protocol, priority, target, icmp string }{
		{protocol: "ip", priority: "1", target: "198.18.227.2", icmp: "icmp"},
		{protocol: "ipv6", priority: "2", target: "2001:db8:227:2::2", icmp: "icmpv6"},
	} {
		out, err := node.Exec("ip", "netns", "exec", openperouter.NamedNetns,
			"tc", "filter", "add", "dev", "guard-main", "egress", "protocol", probe.protocol,
			"pref", probe.priority, "flower", "dst_ip", probe.target, "ip_proto", probe.icmp, "action", "gact", "pass")
		Expect(err).To(Succeed(), out)
	}
}

func checkIsolationClusterUnknown(router openperouter.RouterExecutor, client executor.Executor) {
	GinkgoHelper()
	for _, target := range []string{"198.18.227.2", "2001:db8:227:2::2"} {
		By("main control reaches " + target + ", but tenant packets never leave via main")
		beforeControl := isolationClusterProbePackets(router, target)
		out, err := router.Exec("ping", "-c", "1", "-W", "2", target)
		Expect(err).To(Succeed(), out)
		before := isolationClusterProbePackets(router, target)
		Expect(before).To(BeNumerically(">", beforeControl), "main control packets must increment the counter")
		for range 3 {
			passed, err := isolationClusterPing(client, target)
			Expect(err).To(Succeed())
			Expect(passed).To(BeFalse())
		}
		Expect(isolationClusterProbePackets(router, target)).To(Equal(before), "tenant traffic must not reach the main-table veth")
	}
}

func isolationClusterTXPackets(router executor.Executor, device string) uint64 {
	GinkgoHelper()
	out, err := router.Exec("ip", "-j", "-s", "link", "show", "dev", device)
	Expect(err).To(Succeed())
	var links []struct {
		Stats *struct {
			TX struct {
				Packets uint64 `json:"packets"`
			} `json:"tx"`
		} `json:"stats64"`
	}
	Expect(json.Unmarshal([]byte(out), &links)).To(Succeed())
	Expect(links).To(HaveLen(1))
	Expect(links[0].Stats).NotTo(BeNil(), "ip must expose real interface counters")
	GinkgoWriter.Println(device, "TX packets:", links[0].Stats.TX.Packets)
	return links[0].Stats.TX.Packets
}

func checkIsolationClusterWithdrawal(router openperouter.RouterExecutor, client executor.Executor, paths []isolationClusterPath) {
	GinkgoHelper()
	for _, path := range paths {
		By("withdrawing " + path.name + " and checking the independent destination afterward")
		Expect(path.leaf.ChangePrefixes(nil, nil, nil)).To(Succeed())
		for i, prefix := range path.prefixes {
			Eventually(func() error { return isolationClusterBGPRoute(router, prefix, false) },
				time.Minute, time.Second).Should(Succeed())
			Consistently(func(g Gomega) bool {
				passed, err := isolationClusterPing(client, path.targets[i])
				g.Expect(err).To(Succeed())
				return passed
			},
				3*time.Second, time.Second).Should(BeFalse())
		}
		for _, other := range paths {
			if other.name != path.name {
				checkIsolationClusterPaths(router, client, []isolationClusterPath{other})
			}
		}
		checkIsolationClusterUnknown(router, client)
		By("preferring working FRR defaults over unreachable defaults for " + path.name)
		Expect(path.leaf.ChangePrefixes(nil, []string{"0.0.0.0/0", "::/0"}, nil)).To(Succeed())
		_, err := executor.ForContainer(path.leaf.Name).Exec("vtysh", "-c", "configure terminal",
			"-c", "router bgp 64520 vrf red", "-c", "no bgp network import-check")
		Expect(err).To(Succeed())
		for i, prefix := range []string{"0.0.0.0/0", "::/0"} {
			Eventually(func() error { return isolationClusterBGPRoute(router, prefix, true) },
				time.Minute, time.Second).Should(Succeed())
			Eventually(func(g Gomega) bool {
				passed, err := isolationClusterPing(client, path.targets[i])
				g.Expect(err).To(Succeed())
				return passed
			},
				time.Minute, time.Second).Should(BeTrue())
		}
		Expect(isolationClusterGuards(router)).To(Succeed())
		Expect(path.leaf.ChangePrefixes(nil, nil, nil)).To(Succeed())
		for i, prefix := range []string{"0.0.0.0/0", "::/0"} {
			Eventually(func() error { return isolationClusterBGPRoute(router, prefix, false) },
				time.Minute, time.Second).Should(Succeed())
			passed, err := isolationClusterPing(client, path.targets[i])
			Expect(err).To(Succeed())
			Expect(passed).To(BeFalse())
		}
		Expect(path.leaf.ChangePrefixes(nil, path.prefixes, nil)).To(Succeed())
		checkIsolationClusterPaths(router, client, paths)
	}
}

func isolationClusterProbePackets(router openperouter.RouterExecutor, target string) uint64 {
	GinkgoHelper()
	out, err := executor.ForNode(router.NodeName()).Exec("ip", "netns", "exec", openperouter.NamedNetns,
		"tc", "-j", "-s", "filter", "show", "dev", "guard-main", "egress")
	Expect(err).To(Succeed(), out)
	var filters []struct {
		Options *struct {
			Keys struct {
				Destination string `json:"dst_ip"`
			} `json:"keys"`
			Actions []struct {
				Stats struct {
					Packets uint64 `json:"packets"`
				} `json:"stats"`
			} `json:"actions"`
		} `json:"options"`
	}
	Expect(json.Unmarshal([]byte(out), &filters)).To(Succeed())
	for _, filter := range filters {
		if filter.Options == nil || filter.Options.Keys.Destination != target {
			continue
		}
		Expect(filter.Options.Actions).To(HaveLen(1))
		packets := filter.Options.Actions[0].Stats.Packets
		GinkgoWriter.Println("main-table probe", target, "packets:", packets)
		return packets
	}
	Fail("missing pass-through probe counter for " + target + ": " + out)
	return 0
}

func cleanupIsolationClusterSink(node executor.Executor) error {
	// A previous interrupted run can leave the fixture behind. Explicitly remove
	// its veth before deleting the namespace to avoid asynchronous netns teardown.
	out, err := node.Exec("sh", "-ec", `
if [ -e /var/run/netns/perouter ] && ip netns exec perouter ip link show guard-main >/dev/null 2>&1; then
  ip netns exec perouter ip link del guard-main
fi
if [ -e /var/run/netns/guard-sink ]; then
  ip netns del guard-sink
fi`)
	if err != nil {
		return fmt.Errorf("clean main-table probe fixture: %s: %w", out, err)
	}
	return nil
}
