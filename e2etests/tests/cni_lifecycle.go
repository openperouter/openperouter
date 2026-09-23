// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/systemd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	lcUnderlayStaticInterface = "lc-static"
	lcUnderlayDHCPInterface   = "lc-dhcp"
	// cniUnderlayInterfaceRenamed is used to exercise the
	// reprovisioning flow by changing the desired CNI interface name.
	cniUnderlayInterfaceRenamed = "lc-rename"
	// controllerLabelSelector selects the controller daemonset pods.
	controllerLabelSelector = "app=controller"
	// routerLabelSelector selects the router daemonset pods.
	routerLabelSelector = "app=router"
	// controllerPodUnit is the systemd unit running the controller pod in
	// systemd mode.
	controllerPodUnit = "controllerpod-pod.service"
	// routerPodUnit is the systemd unit running the router pod in systemd
	// mode.
	routerPodUnit = "routerpod-pod.service"
)

// The CNI underlay lifecycle coverage exercises the behaviors specific to
// CNI provisioned underlay interfaces: the libcni result cache driving the
// reconciliation across controller and router restarts, the in-place
// CNI DEL / ADD reprovisioning on interface changes, CNI CHECK validation
// of cached state on every reconciliation, and the teardown through CNI DEL.
// The traffic coverage runs in the EVPN routes suites, parameterized by
// underlay flavor.
var _ = Describe("CNI underlay lifecycle", Ordered, func() {
	var cs clientset.Interface
	nodes := []corev1.Node{}

	validateSessionUp := func() {
		leafExec := executor.ForContainer(infra.KindLeaf)
		for i, node := range nodes {
			validateSessionWithNeighbor(leafExec, validationParameters{
				fromName:    infra.KindLeaf,
				toName:      node.Name,
				neighborIP:  infra.CNIUnderlayNeighborIP(i),
				established: true,
			})
		}
	}

	validateCNISessionsDown := func() {
		leafExec := executor.ForContainer(infra.KindLeaf)
		for i := range nodes {
			validateSessionDownForNeigh(leafExec, infra.CNIUnderlayNeighborIP(i))
		}
	}

	validateCNIInterfacesPresent := func(ifName string) {
		for _, node := range nodes {
			Eventually(func() bool {
				return openperouter.IsInterfaceInNS(node.Name, ifName, openperouter.NamedNetns)
			}, 3*time.Minute, time.Second).Should(BeTrue(),
				fmt.Sprintf("interface %s should be present in the router netns of %s", ifName, node.Name))
		}
	}

	validateCNIInterfacesGone := func(ifName string) {
		for _, node := range nodes {
			Eventually(func() bool {
				return openperouter.IsInterfaceInNS(node.Name, ifName, openperouter.NamedNetns)
			}, 3*time.Minute, time.Second).Should(BeFalse(),
				fmt.Sprintf("interface %s should be gone from the router netns of %s", ifName, node.Name))
		}
	}

	// restartControllers restarts the controller on every node: deleting the
	// pods in k8s mode, restarting the systemd unit in systemd mode.
	restartControllers := func() {
		if HostMode {
			for _, node := range nodes {
				restartSystemdUnit(node, controllerPodUnit)
			}
			return
		}
		oldPods, err := k8s.DeletePodsByLabel(cs, openperouter.Namespace, controllerLabelSelector)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8s.WaitPodsRolled(cs, openperouter.Namespace, controllerLabelSelector, oldPods)).To(Succeed())
	}

	// restartRouters restarts the router on every node: deleting the pods in
	// k8s mode, restarting the systemd unit in systemd mode. In both modes
	// the router netns is the persistent named one, so the interfaces inside
	// it survive the restart.
	restartRouters := func() {
		if HostMode {
			for _, node := range nodes {
				restartSystemdUnit(node, routerPodUnit)
			}
			return
		}
		oldPods, err := k8s.DeletePodsByLabel(cs, openperouter.Namespace, routerLabelSelector)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8s.WaitPodsRolled(cs, openperouter.Namespace, routerLabelSelector, oldPods)).To(Succeed())
	}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		_, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items
		// The CNI underlay fixtures derive each node's static IP from its
		// position in the slice, so the iteration order must be deterministic.
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

		Expect(infra.ConfigureLeafKind1ForCNIUnderlay(nodes)).To(Succeed())

		err = Updater.Update(config.Resources{Underlays: infra.CNIUnderlaysForNodes(nodes, lcUnderlayStaticInterface)})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the CNI interfaces to be provisioned")
		validateCNIInterfacesPresent(lcUnderlayStaticInterface)
		By("checking the parent device stays in the host netns")
		for _, node := range nodes {
			Expect(openperouter.IsInterfaceInDefaultNetns(node.Name, "toswitch1")).To(BeTrue(),
				fmt.Sprintf("toswitch1 should stay in the host netns of %s", node.Name))
		}
		By("waiting for the sessions to be established")
		validateSessionUp()
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the underlay to be removed from all nodes")
		for _, node := range nodes {
			Eventually(func(g Gomega) {
				isConfigured, err := openperouter.UnderlayConfigured(node.Name)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isConfigured).To(BeFalse())
			}, 2*time.Minute, time.Second).Should(Succeed())
		}
		By("restoring the standard leaf configuration")
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	It("keeps the CNI interfaces when the controller pods are restarted", func() {
		indexesBefore, err := cniInterfaceIndexes(nodes, lcUnderlayStaticInterface)
		Expect(err).NotTo(HaveOccurred())

		By("restarting the controllers")
		restartControllers()

		By("checking the interfaces are not reprovisioned by the fresh controllers")
		Consistently(func() (map[string]string, error) {
			return cniInterfaceIndexes(nodes, lcUnderlayStaticInterface)
		}, 30*time.Second, 5*time.Second).Should(Equal(indexesBefore),
			"the interfaces should survive the controller restart untouched")

		By("checking the sessions stayed established")
		validateSessionUp()
	})

	It("keeps the CNI interfaces when the router pods are restarted", func() {
		indexesBefore, err := cniInterfaceIndexes(nodes, lcUnderlayStaticInterface)
		Expect(err).NotTo(HaveOccurred())

		By("restarting the routers")
		restartRouters()

		By("checking the interfaces survived in the persistent netns")
		indexesAfter, err := cniInterfaceIndexes(nodes, lcUnderlayStaticInterface)
		Expect(err).NotTo(HaveOccurred())
		Expect(indexesAfter).To(Equal(indexesBefore),
			"the interfaces should survive the router restart untouched")

		By("checking the sessions re-establish")
		validateSessionUp()
	})

	It("reprovisions the CNI interface after external drift is caught by the next reconcile", func() {
		indexesBefore, err := cniInterfaceIndexes(nodes, lcUnderlayStaticInterface)
		Expect(err).NotTo(HaveOccurred())

		By("deleting the CNI interface directly, behind the controller's back")
		for _, node := range nodes {
			exec := executor.ForNode(node.Name)
			_, err := exec.Exec("ip", "netns", "exec", openperouter.NamedNetns, "ip", "link", "del", lcUnderlayStaticInterface)
			Expect(err).NotTo(HaveOccurred())
		}

		// Reconciliation is event-driven, not on a timer: the dangling cache
		// entry is only caught on the next reconcile which may or may not have
		// been triggered at this point in time. Restarting the
		// controllers forces a reconcile without touching the Underlay.
		By("forcing a reconcile by restarting the controllers")
		restartControllers()

		By("checking cni check detects the drift and the interface is reprovisioned")
		validateCNIInterfacesPresent(lcUnderlayStaticInterface)
		indexesAfter, err := cniInterfaceIndexes(nodes, lcUnderlayStaticInterface)
		Expect(err).NotTo(HaveOccurred())
		Expect(indexesAfter).NotTo(Equal(indexesBefore),
			"the interface should have been recreated with a new ifindex")

		By("checking the sessions re-establish")
		validateSessionUp()
	})

	It("reprovisions in place when the CNI interface is renamed", func() {
		By("renaming the CNI interface, which deletes the stale one and adds the new one")
		err := Updater.Update(config.Resources{Underlays: infra.CNIUnderlaysForNodes(nodes, cniUnderlayInterfaceRenamed)})
		Expect(err).NotTo(HaveOccurred())

		validateCNIInterfacesPresent(cniUnderlayInterfaceRenamed)
		validateCNIInterfacesGone(lcUnderlayStaticInterface)
		validateSessionUp()
	})

	It("removes the CNI interfaces when the underlay is deleted", func() {
		Expect(Updater.CleanAll()).To(Succeed())

		validateCNIInterfacesGone(cniUnderlayInterfaceRenamed)
		validateCNISessionsDown()
	})
})

// The DHCP underlay lifecycle coverage exercises the DHCP-specific behaviors:
// DHCP lease acquisition, controller restart with lease re-acquisition, and
// teardown with DHCP release. The CNI lifecycle behaviors (cache, reprovisioning)
// are already covered by the macvlan-static suite above.
var _ = Describe("DHCP underlay lifecycle", Ordered, func() {
	var cs clientset.Interface
	nodes := []corev1.Node{}

	validateDHCPSessionUp := func() {
		leafExec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			var ip string
			Eventually(func(g Gomega) {
				var err error
				ip, err = infra.DHCPNeighborIP(node.Name, lcUnderlayDHCPInterface)
				g.Expect(err).NotTo(HaveOccurred())
			}, 3*time.Minute, time.Second).Should(Succeed())
			validateSessionWithNeighbor(leafExec, validationParameters{
				fromName:    infra.KindLeaf,
				toName:      node.Name,
				neighborIP:  ip,
				established: true,
			})
		}
	}

	dhcpAddresses := func() (map[string]string, error) {
		res := map[string]string{}
		for _, node := range nodes {
			ip, err := infra.DHCPNeighborIP(node.Name, lcUnderlayDHCPInterface)
			if err != nil {
				return nil, err
			}
			res[node.Name] = ip
		}
		return res, nil
	}

	// restartControllers restarts the controller on every node.
	restartControllers := func() {
		if HostMode {
			for _, node := range nodes {
				restartSystemdUnit(node, controllerPodUnit)
			}
			return
		}
		oldPods, err := k8s.DeletePodsByLabel(cs, openperouter.Namespace, controllerLabelSelector)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8s.WaitPodsRolled(cs, openperouter.Namespace, controllerLabelSelector, oldPods)).To(Succeed())
	}

	BeforeAll(func() {
		cs = k8sclient.New()
		_, err := openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	})

	BeforeEach(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		err = Updater.Update(config.Resources{Underlays: infra.DHCPCNIUnderlaysForNodes(nodes, lcUnderlayDHCPInterface)})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for DHCP addresses to be acquired")
		Eventually(func() error {
			_, err := dhcpAddresses()
			return err
		}, 3*time.Minute, time.Second).Should(Succeed())

		By("configuring leafkind1 with the DHCP-assigned addresses")
		Expect(infra.ConfigureLeafKind1ForDHCPUnderlay(nodes, lcUnderlayDHCPInterface)).To(Succeed())

		By("waiting for the sessions to be established")
		validateDHCPSessionUp()
	})

	AfterEach(func() {
		dumpIfFails(cs)

		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the underlay to be removed from all nodes")
		for _, node := range nodes {
			Eventually(func(g Gomega) {
				isConfigured, err := openperouter.UnderlayConfigured(node.Name)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isConfigured).To(BeFalse())
			}, 2*time.Minute, time.Second).Should(Succeed())
		}
		By("restoring the standard leaf configuration")
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
	})

	It("re-acquires DHCP leases after controller restart", func() {
		addrsBefore, err := dhcpAddresses()
		Expect(err).NotTo(HaveOccurred())

		By("restarting the controllers")
		restartControllers()

		By("checking the DHCP addresses are retained after lease re-acquisition")
		Eventually(func() (map[string]string, error) {
			return dhcpAddresses()
		}, 3*time.Minute, time.Second).Should(Equal(addrsBefore),
			"DHCP addresses should be the same after controller restart")

		By("checking the DHCP leases remain valid after daemon restart and CNI CHECK")
		Consistently(func() error {
			for _, ip := range addrsBefore {
				if err := infra.DHCPServerLeaseValid(ip); err != nil {
					return fmt.Errorf("dnsmasq should have a valid lease for %s: %w", ip, err)
				}
			}
			return nil
		}).
			WithTimeout(30*time.Second).
			WithPolling(5*time.Second).
			Should(Succeed(),
				"CNI CHECK after DHCP daemon restart should keep leases renewed")

		By("checking the sessions re-establish")
		validateDHCPSessionUp()
	})

	It("removes the DHCP interfaces when the underlay is deleted", func() {
		By("storing the original router IPs")
		addrsBefore, err := dhcpAddresses()
		Expect(err).NotTo(HaveOccurred())

		By("removing the underlay")
		Expect(Updater.CleanAll()).To(Succeed())

		for nodeName, nodeIP := range addrsBefore {
			By(fmt.Sprintf("checking interface %q is gone from node %q", lcUnderlayDHCPInterface, nodeName))
			Eventually(func() bool {
				return openperouter.IsInterfaceInNS(nodeName, lcUnderlayDHCPInterface, openperouter.NamedNetns)
			}).
				WithTimeout(time.Minute).
				WithPolling(5*time.Second).
				Should(BeFalse(),
					fmt.Sprintf("interface %s should be gone from the router netns of %s",
						lcUnderlayDHCPInterface, nodeName))

			By(fmt.Sprintf("checking lease for IP %q on node %q is already cleaned", nodeIP, nodeName))
			Eventually(func() error {
				return infra.DHCPServerLeaseValid(nodeIP)
			}).
				WithTimeout(time.Minute).
				WithPolling(5 * time.Second).
				Should(MatchError(infra.ErrLeaseNotFound))
		}
	})
})

// cniInterfaceIndexes returns the ifindex of the CNI provisioned interface in
// the router netns of every node. A changed index after an event means the
// interface was recreated.
func cniInterfaceIndexes(nodes []corev1.Node, ifName string) (map[string]string, error) {
	res := map[string]string{}
	for _, node := range nodes {
		exec := executor.ForNode(node.Name)
		out, err := exec.Exec("ip", "netns", "exec", openperouter.NamedNetns, "ip", "-o", "link", "show", "dev", ifName)
		if err != nil {
			return nil, fmt.Errorf("failed to get %s ifindex on %s: %w", ifName, node.Name, err)
		}
		index, _, found := strings.Cut(strings.TrimSpace(out), ":")
		if !found {
			return nil, fmt.Errorf("unexpected ip link output for %s on %s: %q", ifName, node.Name, out)
		}
		res[node.Name] = index
	}
	return res, nil
}

// restartSystemdUnit restarts the systemd unit on the node and waits for it
// to become active again with a fresh main PID.
func restartSystemdUnit(node corev1.Node, unit string) {
	By(fmt.Sprintf("restarting %s via systemd on node %s", unit, node.Name))
	nodeExec := executor.ForNode(node.Name)
	Expect(systemd.RestartSystemdUnit(nodeExec, unit)).To(Succeed())
}
