// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/url"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const resiliencyNetnsPath = "/var/run/netns/perouter"

var _ = Describe("Alpha: Named netns and kernel objects survive FRR crash", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: 100,
		},
	}

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: ptr.To("red"),
			VNI: 110,
			HostMaster: &v1alpha1.HostMaster{
				Type: "linux-bridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: ptr.To(true),
				},
			},
		},
	}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		Eventually(func() error {
			routers, err = openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		routers.Dump(ginkgo.GinkgoWriter)

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())

		err = Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{l2VniRed},
		})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the controller to provision VRF, bridge, and VXLAN interfaces in the named netns")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		for _, node := range nodes {
			nodeName := node.Name
			for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
				Eventually(func(g Gomega) {
					present, err := openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(present).To(BeTrue(), "interface type %s not yet present in named netns on node %s", ifType, nodeName)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
			}
		}
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	AfterAll(func() {
		dumpUnderlayVeths(cs, "Alpha AfterAll before cleanup")
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafBConfig.Reset()).To(Succeed())
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for all router pods to be ready")
		Eventually(func(g Gomega) {
			pods, err := openperouter.RouterPods(cs)
			g.Expect(err).NotTo(HaveOccurred())
			for _, p := range pods {
				g.Expect(k8s.PodIsReady(p)).To(BeTrue(), "pod %s must be ready", p.Name)
			}
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	It("should preserve named netns at /var/run/netns/perouter when FRR process crashes", func() {
		routerPods, err := openperouter.RouterPodsForNodes(cs, allNodes(cs))
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).NotTo(BeEmpty())
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName

		By("verifying named netns exists before crash")
		exists, err := openperouter.NamedNetnsExists(nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue(), "named netns must exist before FRR crash")

		By("verifying kernel objects exist before crash")
		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			present, err := openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
			Expect(err).NotTo(HaveOccurred())
			Expect(present).To(BeTrue(), "interface type %s must exist before crash", ifType)
		}

		By("killing the FRR container entrypoint process")
		frrExec := executor.ForPod(openperouter.Namespace, routerPod.Name, "frr")
		killFRREntrypoint(frrExec)

		By("immediately asserting named netns and kernel objects survived the crash")
		exists, err = openperouter.NamedNetnsExists(nodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue(), "named netns must survive FRR crash immediately")

		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			present, err := openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
			Expect(err).NotTo(HaveOccurred())
			Expect(present).To(BeTrue(), "interface type %s must survive FRR crash immediately", ifType)
		}

		By("waiting for the FRR container to restart and become ready")
		Eventually(func(g Gomega) []v1.PodCondition {
			pod, err := cs.CoreV1().Pods(openperouter.Namespace).Get(context.Background(), routerPod.Name, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			return pod.Status.Conditions
		}).
			WithTimeout(2*time.Minute).
			WithPolling(time.Second).
			Should(
				ContainElement(
					SatisfyAll(
						HaveField("Type", Equal(v1.PodReady)),
						HaveField("Status", Equal(v1.ConditionTrue)),
					),
				),
				"router pod should become ready after FRR restart",
			)

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)
	})
})

// allNodes returns a map of all Kubernetes node names for use with RouterPodsForNodes.
func allNodes(cs clientset.Interface) map[string]bool {
	nodes, err := k8s.GetNodes(cs)
	Expect(err).NotTo(HaveOccurred())
	result := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		result[n.Name] = true
	}
	return result
}

// killFRREntrypoint kills the tini/docker-start entrypoint process inside the given FRR container.
// A DeferCleanup is NOT registered; callers are responsible for waiting on pod restart.
func killFRREntrypoint(frrExec executor.Executor) {
	GinkgoHelper()
	psOut, err := frrExec.Exec("pgrep", "-f", "/sbin/tini -- /usr/lib/frr/docker-start")
	Expect(err).NotTo(HaveOccurred(), "failed to find FRR entrypoint PID")
	pids := strings.Split(strings.TrimSpace(psOut), "\n")
	Expect(pids).NotTo(BeEmpty(), "FRR entrypoint PID should not be empty")
	frrPID := strings.TrimSpace(pids[0])
	output, err := frrExec.Exec("kill", frrPID)
	Expect(err).NotTo(HaveOccurred(), "failed to kill FRR process %q: %v", frrPID, output)
}

var _ = Describe("Beta: Named netns auto-rebuilds after deletion", Ordered, func() {
	var cs clientset.Interface

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: 100,
		},
	}

	l2VniRed := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red110",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VRF: ptr.To("red"),
			VNI: 110,
			HostMaster: &v1alpha1.HostMaster{
				Type: "linux-bridge",
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					AutoCreate: ptr.To(true),
				},
			},
		},
	}

	BeforeAll(func() {
		cs = k8sclient.New()

		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for all router pods to be ready after cleanup")
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		underlayWithGR := infra.Underlay.DeepCopy()
		underlayWithGR.Spec.GracefulRestart = &v1alpha1.GracefulRestartConfig{}

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{*underlayWithGR},
		})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for all router pods to be ready")
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())
		By("configuring leafkind with BGP graceful-restart and next-hop-self")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		neighbors := []string{}
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			neighbors = append(neighbors, neighborIP)
		}
		leafConfig := infra.LeafKindConfiguration{
			PERouterASN: 64514,
			NextHopSelf: true,
			Neighbors:   neighbors,
		}
		configString, err := infra.LeafKindConfigToFRR(leafConfig)
		Expect(err).NotTo(HaveOccurred())
		err = infra.LeafKindConfig.ReloadConfig(configString)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for BGP sessions to establish after underlay creation")
		leafExec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				infra.KindLeaf,
				node.Name,
				leafExec,
				neighborIP,
				Established,
			)
		}
	})

	AfterAll(func() {
		dumpUnderlayVeths(cs, "Beta AfterAll before cleanup")
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafBConfig.Reset()).To(Succeed())

		By("resetting leafkind config to defaults")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		resetLeafKindConfig(nodes)

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for all router pods to be ready after cleanup")
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	const testNamespace = "test-namespace-rebuild"

	AfterEach(func() {
		dumpIfFails(cs)
		dumpUnderlayVeths(cs, "Beta AfterEach before cleanup")
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
		if err := k8s.DeleteNamespace(cs, testNamespace); err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		By("waiting for all router pods to be ready")
		Eventually(func(g Gomega) {
			pods, err := openperouter.RouterPods(cs)
			g.Expect(err).NotTo(HaveOccurred())
			for _, p := range pods {
				g.Expect(k8s.PodIsReady(p)).To(BeTrue(), "pod %s must be ready", p.Name)
			}
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	It("should auto-recover when the named netns is deleted via ip netns delete", func() {
		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.L2GatewayIPs = []string{"192.171.24.1/24"}

		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{*l2VniRedWithGateway},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", []string{"192.171.24.1/24"})
		Expect(err).NotTo(HaveOccurred())

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 1))

		By("creating the client pod")
		clientPod, err := k8s.CreateAgnhostPod(cs, "pod1", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{"192.171.24.2/24"}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("removing the default gateway via the primary interface")
		Expect(removeGatewayFromPod(clientPod)).To(Succeed())

		hostARedExecutor := executor.ForContainer("clab-kind-hostA_red")
		firstPodIP := "192.171.24.2"
		const port = "8090"
		hostPort := net.JoinHostPort(firstPodIP, port)
		urlStr := url.Format("http://%s/clientip", hostPort)

		By("waiting for BGP sessions to establish before traffic check")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodes[0].Name)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodes[0].Name,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)

		By("waiting for Type-5 prefix route to appear on the fabric before traffic check")
		waitForType5Route(executor.ForContainer(infra.KindLeaf), "192.171.24.0/24")

		By("verifying traffic works before netns deletion")
		Eventually(func() error {
			_, err := hostARedExecutor.Exec("curl", "-sS", "--max-time", "3", urlStr)
			return err
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())

		By("identifying the router pod on clientPod's node")
		routerPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{clientPod.Spec.NodeName: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).To(HaveLen(1))
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName
		oldPodUID := routerPod.UID

		t0 := time.Now()

		DeferCleanup(func() {
			if !ginkgo.CurrentSpecReport().Failed() {
				return
			}
			ginkgo.GinkgoWriter.Printf("=== FAILURE-TIME DIAGNOSTICS (at %v) ===\n", time.Since(t0))
			dumpNetnsState(cs, nodeName)
		})

		By("capturing the initial perouter netns inode and container process tree before deletion")
		nodeExec := executor.ForContainer(nodeName)
		oldNetnsInode, err := nodeExec.Exec("bash", "-c",
			"cat /proc/self/mountinfo | grep '/run/netns/perouter' | awk '{print $4}'")
		if err != nil {
			ginkgo.GinkgoWriter.Printf("DIAG: failed to get initial netns inode: %v\n", err)
		} else {
			ginkgo.GinkgoWriter.Printf("DIAG: initial (OLD) perouter netns inode: %s\n", strings.TrimSpace(oldNetnsInode))
		}

		var oldMntNS, oldShimPID string
		if strings.TrimSpace(oldNetnsInode) != "" {
			containerInfo, infoErr := nodeExec.Exec("bash", "-c", fmt.Sprintf(`inode='%s'
for p in /proc/[0-9]*/ns/net; do
  pid=${p#/proc/}; pid=${pid%%%%/*}
  if [ "$(readlink "$p" 2>/dev/null)" = "$inode" ]; then
    mntns=$(readlink /proc/$pid/ns/mnt 2>/dev/null)
    echo "OLD_MNTNS=$mntns"
    current=$pid
    for i in 1 2 3 4 5; do
      ppid=$(awk '/PPid/{print $2}' /proc/$current/status 2>/dev/null)
      cmd=$(cat /proc/$current/cmdline 2>/dev/null | tr '\0' ' ' | head -c 120)
      echo "CHAIN: pid=$current ppid=$ppid cmd=$cmd"
      case "$cmd" in *containerd-shim*) echo "OLD_SHIM_PID=$current" ;; esac
      [ -z "$ppid" ] || [ "$ppid" = "0" ] && break
      current=$ppid
    done
    break
  fi
done`, strings.TrimSpace(oldNetnsInode)))
			if infoErr != nil {
				ginkgo.GinkgoWriter.Printf("DIAG: failed to get old container info: %v\n", infoErr)
			} else {
				ginkgo.GinkgoWriter.Printf("DIAG: old container process tree:\n%s\n", containerInfo)
				for _, line := range strings.Split(containerInfo, "\n") {
					if strings.HasPrefix(line, "OLD_MNTNS=") {
						oldMntNS = strings.TrimPrefix(line, "OLD_MNTNS=")
					}
					if strings.HasPrefix(line, "OLD_SHIM_PID=") {
						oldShimPID = strings.TrimPrefix(line, "OLD_SHIM_PID=")
					}
				}
			}
		}

		By("deleting the named netns bind mount while the router pod is still running")
		Expect(openperouter.DeleteNamedNetns(nodeName)).To(Succeed())
		ginkgo.GinkgoWriter.Printf("TIMING: T0 netns deleted at %v\n", time.Since(t0))

		monitorCtx, monitorCancel := context.WithCancel(context.Background())
		defer monitorCancel()
		go func() {
			for {
				select {
				case <-monitorCtx.Done():
					return
				case <-time.After(time.Second):
					_, err := executor.Host.Exec("ip", "link", "show", "kindctrlpl")
					if err != nil {
						ginkgo.GinkgoWriter.Printf("TIMING: kindctrlpl disappeared at %v (old netns dead)\n", time.Since(t0))
						return
					}
				}
			}
		}()

		By("deleting the router pod so FRR exits and the netns is truly destroyed")
		err = cs.CoreV1().Pods(openperouter.Namespace).Delete(context.Background(), routerPod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
		ginkgo.GinkgoWriter.Printf("TIMING: T1 pod delete API call at %v\n", time.Since(t0))

		By("waiting for the old router pod to be fully terminated")
		Eventually(func() error {
			_, err := cs.CoreV1().Pods(openperouter.Namespace).Get(context.Background(), routerPod.Name, metav1.GetOptions{})
			return err
		}).WithTimeout(2*time.Minute).WithPolling(2*time.Second).Should(
			MatchError(apierrors.IsNotFound, "NOT FOUND"),
			"the router pod must be gone from the API",
		)
		ginkgo.GinkgoWriter.Printf("TIMING: T2 old pod 404 at %v\n", time.Since(t0))

		By("checking if old container's containerd-shim and mount namespace survived pod termination")
		if oldShimPID != "" {
			shimOut, shimErr := nodeExec.Exec("bash", "-c", fmt.Sprintf(`shim_pid='%s'
if [ -d /proc/$shim_pid ]; then
  echo "SHIM ALIVE: pid=$shim_pid cmd=$(cat /proc/$shim_pid/cmdline 2>/dev/null | tr '\0' ' ' | head -c 120)"
  echo "SHIM FDS:"
  for fd in /proc/$shim_pid/fd/*; do
    target=$(readlink "$fd" 2>/dev/null)
    case "$target" in *nsfs*|*net:*|*mnt:*|*":[[]"*) echo "  fd=$(basename $fd) -> $target" ;; esac
  done
else
  echo "SHIM GONE: pid=$shim_pid"
fi`, strings.TrimSpace(oldShimPID)))
			if shimErr != nil {
				ginkgo.GinkgoWriter.Printf("DIAG: shim check error: %v\n", shimErr)
			} else {
				ginkgo.GinkgoWriter.Printf("DIAG: old containerd-shim status:\n%s\n", shimOut)
			}
		}
		if oldMntNS != "" {
			mntOut, mntErr := nodeExec.Exec("bash", "-c", fmt.Sprintf(`old_mntns='%s'
echo "searching for processes still in old FRR mount namespace $old_mntns:"
found=0
for p in /proc/[0-9]*/ns/mnt; do
  pid=${p#/proc/}; pid=${pid%%%%/*}
  if [ "$(readlink "$p" 2>/dev/null)" = "$old_mntns" ]; then
    cmd=$(cat /proc/$pid/cmdline 2>/dev/null | tr '\0' ' ' | head -c 100)
    netns=$(readlink /proc/$pid/ns/net 2>/dev/null)
    echo "  HOLDER: pid=$pid netns=$netns cmd=$cmd"
    found=1
  fi
done
[ $found -eq 0 ] && echo "  none found (mount namespace fully cleaned up)"`, strings.TrimSpace(oldMntNS)))
			if mntErr != nil {
				ginkgo.GinkgoWriter.Printf("DIAG: mount namespace survivor check error: %v\n", mntErr)
			} else {
				ginkgo.GinkgoWriter.Printf("DIAG: old mount namespace survivors:\n%s\n", mntOut)
			}
		}

		By("collecting diagnostic state to identify what keeps the old netns alive")
		dumpNetnsState(cs, nodeName)
		ginkgo.GinkgoWriter.Printf("TIMING: T3 diagnostics done at %v\n", time.Since(t0))

		By("waiting for check_veths to recreate the underlay veth destroyed by netns deletion")
		Eventually(func() bool {
			return openperouter.UnderlayVethExists(nodeName)
		}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(BeTrue())
		ginkgo.GinkgoWriter.Printf("TIMING: T4 toswitch found at %v\n", time.Since(t0))
		monitorCancel()

		By("waiting for the controller to recreate the named netns")
		Eventually(func() (bool, error) {
			return openperouter.NamedNetnsExists(nodeName)
		}, 2*time.Minute, 2*time.Second).Should(BeTrue(), "controller must recreate named netns")

		By("waiting for all interface types to be recreated in the new netns")
		for _, ifType := range []string{"vrf", "bridge", "vxlan"} {
			Eventually(func() (bool, error) {
				return openperouter.NamedNetnsHasInterfaceType(nodeName, ifType)
			}, 2*time.Minute, 2*time.Second).Should(BeTrue(), "interface type %s must be recreated", ifType)
		}

		By("waiting for a new router pod to come up and become ready")
		Eventually(func(g Gomega) {
			newRouterPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{nodeName: true})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newRouterPods).To(HaveLen(1))
			newPod := newRouterPods[0]
			g.Expect(newPod.UID).NotTo(Equal(oldPodUID), "a new router pod must be created after netns deletion")
			g.Expect(k8s.PodIsReady(newPod)).To(BeTrue(), "new router pod must be ready")
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("waiting for BGP sessions to re-establish")
		neighborIP, err = infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)

		By("waiting for Type-5 prefix route to appear on the fabric")
		waitForType5Route(executor.ForContainer(infra.KindLeaf), "192.171.24.0/24")

		By("capturing pre-traffic diagnostic snapshot (rebuilt PE + leafkind state)")
		dumpPreTrafficState(cs, nodeName)

		By("waiting for bridge FDB entries on rebuilt PE before traffic check")
		rebuiltRouterPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{nodeName: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(rebuiltRouterPods).To(HaveLen(1))
		rebuiltPEExec := executor.ForPodInNamedNetns(rebuiltRouterPods[0].Namespace, rebuiltRouterPods[0].Name, "frr", resiliencyNetnsPath)
		Eventually(func(g Gomega) {
			out, err := rebuiltPEExec.Exec("bash", "-c", "bridge fdb show dev vni110")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(out).NotTo(BeEmpty(), "no FDB entries on vni110 — zebra has not programmed any remote MACs")
			g.Expect(out).To(ContainSubstring("dst"), "FDB entries exist but none have a remote VTEP destination")
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("verifying traffic works again after rebuild")
		Eventually(func() error {
			_, err := hostARedExecutor.Exec("curl", "-sS", "--max-time", "3", urlStr)
			return err
		}).WithTimeout(3 * time.Minute).WithPolling(time.Second).Should(Succeed())
	})

	It("should maintain stretched L2 traffic across nodes with minimal disruption when a router pod is deleted", func() {
		l2VniRedWithGateway := l2VniRed.DeepCopy()
		l2VniRedWithGateway.Spec.L2GatewayIPs = []string{"192.171.24.1/24"}

		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{vniRed},
			L2VNIs: []v1alpha1.L2VNI{*l2VniRedWithGateway},
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = k8s.CreateNamespace(cs, testNamespace)
		Expect(err).NotTo(HaveOccurred())

		nad, err := k8s.CreateMacvlanNad("110", testNamespace, "br-hs-110", nil)
		Expect(err).NotTo(HaveOccurred())

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "stretched L2 test requires at least 2 nodes")

		const (
			serverIP = "192.171.24.2"
			clientIP = "192.171.24.3"
			subnet   = "/24"
			port     = "8090"
		)

		By("creating the server pod on node 0")
		serverPod, err := k8s.CreateAgnhostPod(cs, "server", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{serverIP + subnet}),
			k8s.OnNode(nodes[0].Name))
		Expect(err).NotTo(HaveOccurred())

		By("creating the client pod on node 1")
		clientPod, err := k8s.CreateAgnhostPod(cs, "client", testNamespace,
			k8s.WithNad(nad.Name, testNamespace, []string{clientIP + subnet}),
			k8s.OnNode(nodes[1].Name))
		Expect(err).NotTo(HaveOccurred())

		clientExec := executor.ForPod(clientPod.Namespace, clientPod.Name, "agnhost")
		hostPort := net.JoinHostPort(serverIP, port)
		urlStr := url.Format("http://%s/clientip", hostPort)

		DeferCleanup(func() {
			if !ginkgo.CurrentSpecReport().Failed() {
				return
			}
			leafExec := executor.ForContainer(infra.KindLeaf)
			out, err := leafExec.Exec("vtysh", "-c", "show bgp l2vpn evpn route type macip")
			if err != nil {
				ginkgo.GinkgoWriter.Printf("failed to dump leafkind Type-2 routes: %v\n", err)
			} else {
				ginkgo.GinkgoWriter.Printf("=== leafkind Type-2 EVPN routes ===\n%s\n", out)
			}
			out, err = leafExec.Exec("vtysh", "-c", "show evpn mac vni all")
			if err != nil {
				ginkgo.GinkgoWriter.Printf("failed to dump leafkind EVPN MACs: %v\n", err)
			} else {
				ginkgo.GinkgoWriter.Printf("=== leafkind EVPN MACs ===\n%s\n", out)
			}
		})

		dumpUnderlayVeths(cs, "stretched-L2 before traffic check")

		By("waiting for BGP sessions to establish on both nodes before traffic check")
		leafExec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				infra.KindLeaf,
				node.Name,
				leafExec,
				neighborIP,
				Established,
			)
		}

		By("verifying stretched L2 traffic works before router pod deletion")
		Eventually(func() error {
			_, err := clientExec.Exec("curl", "-sS", "--max-time", "2", urlStr)
			return err
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())

		By("identifying the router pod on the server's node")
		routerPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{serverPod.Spec.NodeName: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).To(HaveLen(1))
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName

		By("starting continuous traffic measurement")
		stopAndCount := measureTrafficLoss(clientExec, urlStr)

		By("deleting the router pod on the server's node")
		err = cs.CoreV1().Pods(openperouter.Namespace).Delete(context.Background(), routerPod.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for a new router pod to become ready")
		Eventually(func(g Gomega) {
			newRouterPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{nodeName: true})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(newRouterPods).To(HaveLen(1))
			newPod := newRouterPods[0]
			g.Expect(newPod.Name).NotTo(Equal(routerPod.Name), "a new router pod must be created")
			g.Expect(k8s.PodIsReady(newPod)).To(BeTrue(), "new router pod must be ready")
		}).WithTimeout(3 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("waiting for BGP sessions to re-establish")
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validateSessionWithNeighbor(
			infra.KindLeaf,
			nodeName,
			executor.ForContainer(infra.KindLeaf),
			neighborIP,
			Established,
		)

		By("asserting stretched L2 disruption is within acceptable bounds during router pod deletion and recovery")
		result := stopAndCount()
		By(fmt.Sprintf("==> %s", result.String()))
		Expect(result.eval()).To(
			Succeed(),
			"curl failures exceeded threshold during router pod deletion and recovery (%d/%d failed). Failed timestamps: %+v",
			result.failCount,
			result.totalCount,
			result.failedTimestamps,
		)
	})
})

type trafficTestResult struct {
	failCount        int
	totalCount       int
	failedTimestamps []time.Time
}

func measureTrafficLoss(exec executor.Executor, urlStr string) func() trafficTestResult {
	var mu sync.Mutex
	var trafficTestCount trafficTestResult
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_, err := exec.Exec("curl", "-sS", "--max-time", "2", urlStr)
			mu.Lock()
			if err != nil {
				trafficTestCount.failCount++
				trafficTestCount.failedTimestamps = append(trafficTestCount.failedTimestamps, time.Now())
			}
			trafficTestCount.totalCount++
			mu.Unlock()
			time.Sleep(300 * time.Millisecond)
		}
	}()
	return func() trafficTestResult {
		cancel()
		mu.Lock()
		defer mu.Unlock()
		return trafficTestCount
	}
}

func (tr trafficTestResult) eval() error {
	const maxAllowedFailures = 5
	if tr.totalCount == 0 {
		return fmt.Errorf("no traffic was measured")
	}
	if tr.failCount > maxAllowedFailures {
		return fmt.Errorf(tr.String())
	}
	return nil
}

func (tr trafficTestResult) String() string {
	return fmt.Sprintf("failed %d/%d times", tr.failCount, tr.totalCount)
}

func dumpUnderlayVeths(cs clientset.Interface, label string) {
	w := ginkgo.GinkgoWriter
	w.Printf("=== DIAG [%s]: underlay veth state ===\n", label)

	nodes, err := k8s.GetNodes(cs)
	if err != nil {
		w.Printf("DIAG [%s]: failed to list nodes: %v\n", label, err)
		return
	}

	for _, node := range nodes {
		nodeExec := executor.ForContainer(node.Name)

		for _, loc := range []struct {
			desc string
			args []string
		}{
			{"default netns", []string{"ip", "-d", "link", "show", "toswitch"}},
			{"perouter netns", []string{"ip", "netns", "exec", "perouter", "ip", "-d", "link", "show", "toswitch"}},
		} {
			out, err := nodeExec.Exec(loc.args[0], loc.args[1:]...)
			if err != nil {
				w.Printf("DIAG [%s]: %s toswitch in %s: not found\n", label, node.Name, loc.desc)
			} else {
				w.Printf("DIAG [%s]: %s toswitch in %s:\n%s\n", label, node.Name, loc.desc, out)
			}
		}
	}

	for _, port := range []string{"kindctrlpl", "kindworker"} {
		out, err := executor.Host.Exec("ip", "-d", "link", "show", port)
		if err != nil {
			w.Printf("DIAG [%s]: bridge port %s: not found\n", label, port)
		} else {
			w.Printf("DIAG [%s]: bridge port %s:\n%s\n", label, port, out)
		}
	}
}

func dumpNetnsState(cs clientset.Interface, nodeName string) {
	w := ginkgo.GinkgoWriter
	w.Printf("=== DIAG: netns state after pod termination (node %s) ===\n", nodeName)

	controllerPods, err := k8s.PodsForLabel(cs, openperouter.Namespace, "app=controller")
	if err != nil {
		w.Printf("DIAG: failed to list controller pods: %v\n", err)
		return
	}
	var controllerPod *v1.Pod
	for _, p := range controllerPods {
		if p.Spec.NodeName == nodeName {
			controllerPod = p
			break
		}
	}

	if controllerPod != nil {
		ctrlExec := executor.ForPod(controllerPod.Namespace, controllerPod.Name, "controller")
		for _, cmd := range []struct {
			label string
			args  []string
		}{
			{"controller mountinfo (perouter)", []string{"bash", "-c", "cat /proc/self/mountinfo | grep perouter || echo 'no perouter mount found'"}},
			{"controller perouter mount count", []string{"bash", "-c", "echo -n 'count: '; cat /proc/self/mountinfo | grep -c perouter || echo 0"}},
			{"controller nsfs fd count", []string{"bash", "-c", "echo -n 'count: '; ls -la /proc/1/fd/ 2>/dev/null | grep -c nsfs || echo 0"}},
			{"controller ls /var/run/netns/", []string{"ls", "/var/run/netns/"}},
			{"controller ip netns list", []string{"ip", "netns", "list"}},
		} {
			out, err := ctrlExec.Exec(cmd.args[0], cmd.args[1:]...)
			if err != nil {
				w.Printf("DIAG: %s: error: %v (output: %s)\n", cmd.label, err, out)
			} else {
				w.Printf("DIAG: %s:\n%s\n", cmd.label, out)
			}
		}
	} else {
		w.Printf("DIAG: no controller pod found on node %s\n", nodeName)
	}

	nodeExec := executor.ForContainer(nodeName)
	for _, cmd := range []struct {
		label string
		args  []string
	}{
		{"KIND node mountinfo (perouter)", []string{"bash", "-c", "cat /proc/self/mountinfo | grep perouter || echo 'no perouter mount found'"}},
		{"KIND node perouter mount count", []string{"bash", "-c", "echo -n 'count: '; cat /proc/self/mountinfo | grep -c perouter || echo 0"}},
		{"KIND node ls /var/run/netns/", []string{"ls", "/var/run/netns/"}},
		{"KIND node ip netns list", []string{"ip", "netns", "list"}},
		{"KIND node lsns -t net", []string{"lsns", "-t", "net"}},
		{"KIND node netns holders", []string{"bash", "-c", "for p in /proc/[0-9]*/ns/net; do readlink $p 2>/dev/null; done | sort | uniq -c | sort -rn | head -10"}},
	} {
		out, err := nodeExec.Exec(cmd.args[0], cmd.args[1:]...)
		if err != nil {
			w.Printf("DIAG: %s: error: %v (output: %s)\n", cmd.label, err, out)
		} else {
			w.Printf("DIAG: %s:\n%s\n", cmd.label, out)
		}
	}

	out, err := executor.Host.Exec("ip", "link", "show", "kindctrlpl")
	if err != nil {
		w.Printf("DIAG: CI host kindctrlpl: not found (old netns likely dead)\n")
	} else {
		w.Printf("DIAG: CI host kindctrlpl (still alive = old netns held open):\n%s\n", out)
	}

	w.Printf("=== END netns diagnostics ===\n")
}

func dumpPreTrafficState(cs clientset.Interface, nodeName string) {
	w := ginkgo.GinkgoWriter

	routerPods, err := openperouter.RouterPodsForNodes(cs, map[string]bool{nodeName: true})
	if err != nil || len(routerPods) == 0 {
		w.Printf("DIAG: cannot get rebuilt router pod on %s: %v\n", nodeName, err)
		return
	}
	peExec := executor.ForPodInNamedNetns(routerPods[0].Namespace, routerPods[0].Name, "frr", resiliencyNetnsPath)

	cfg, err := frr.RunningConfig(peExec)
	if err != nil {
		w.Printf("DIAG: rebuilt PE running-config error: %v\n", err)
	} else {
		w.Printf("=== DIAG: rebuilt PE (%s) running-config ===\n%s\n", routerPods[0].Name, cfg)
	}

	for _, cmd := range []struct {
		label string
		exec  executor.Executor
		args  []string
	}{
		{"rebuilt PE Type-5 routes", peExec, []string{"vtysh", "-c", "show bgp l2vpn evpn route type prefix"}},
		{"rebuilt PE Type-2 routes", peExec, []string{"vtysh", "-c", "show bgp l2vpn evpn route type macip"}},
		{"rebuilt PE ip neigh", peExec, []string{"bash", "-c", "ip neigh"}},
		{"leafkind Type-5 routes", executor.ForContainer(infra.KindLeaf), []string{"vtysh", "-c", "show bgp l2vpn evpn route type prefix"}},
		{"leafkind Type-2 routes", executor.ForContainer(infra.KindLeaf), []string{"vtysh", "-c", "show bgp l2vpn evpn route type macip"}},
		{"leafkind BGP neighbors", executor.ForContainer(infra.KindLeaf), []string{"vtysh", "-c", "show bgp neighbors"}},
		{"rebuilt PE bridge fdb (vni110)", peExec, []string{"bash", "-c", "bridge fdb show dev vni110"}},
		{"rebuilt PE bridge fdb (br-pe-110)", peExec, []string{"bash", "-c", "bridge fdb show dev br-pe-110"}},
	} {
		out, err := cmd.exec.Exec(cmd.args[0], cmd.args[1:]...)
		if err != nil {
			w.Printf("DIAG: %s error: %v\n", cmd.label, err)
		} else {
			w.Printf("=== DIAG: %s ===\n%s\n", cmd.label, out)
		}
	}
}
