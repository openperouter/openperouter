// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	nad "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	trunkTestNamespace = "test-l2trunk"
	trunkTestName      = "workloads"
	trunkTestBridge    = "br-trunk"

	trunkRedTag   = int32(100)
	trunkBlueTag  = int32(200)
	trunkIdleTag  = int32(350)
	trunkRedVNI   = int32(10100)
	trunkBlueVNI  = int32(10200)
	trunkL3VNIVNI = int32(100)

	trunkRedGateway  = "192.171.100.1/24"
	trunkBlueGateway = "192.171.200.1/24"
	trunkRedPodA     = "192.171.100.2"
	trunkRedPodB     = "192.171.100.3"
	trunkBluePodA    = "192.171.200.2"
	trunkBluePodB    = "192.171.200.3"
	trunkIdlePod     = "192.171.100.99"

	trunkUntaggedIP = "192.171.100.200"

	trunkScaleTagBase = int32(400)
	trunkScaleTags    = 20
)

// These mirror internal/hostnetwork.TrunkID so the suite derives the device
// names the controller creates without importing the implementation under test.
const (
	trunkIDLength = 8
	trunkIDBase   = 62
)

var trunkIDModulus = new(big.Int).Exp(big.NewInt(trunkIDBase), big.NewInt(trunkIDLength), nil)

var _ = Describe("Tagged L2VNIs carried by an L2Trunk", Ordered, func() {
	var (
		cs      clientset.Interface
		routers openperouter.Routers
		nodes   []corev1.Node

		trunk   v1alpha1.L2Trunk
		l2vnis  []v1alpha1.L2VNI
		trunkID string
		tags    []int32

		hostVethIndexBeforeTags map[string]string

		redPodAExecutor  executor.Executor
		redPodBExecutor  executor.Executor
		bluePodAExecutor executor.Executor
		bluePodBExecutor executor.Executor
		idlePodExecutor  executor.Executor
	)

	l3vni := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: trunkL3VNIVNI,
		},
	}

	trunkedL2VNI := func(name string, vni, tag int32, gatewayIPs []string,
		routingDomain *v1alpha1.RoutingDomain) v1alpha1.L2VNI {
		return v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L2VNISpec{
				VNI:           vni,
				GatewayIPs:    gatewayIPs,
				RoutingDomain: routingDomain,
				Trunk: &v1alpha1.L2TrunkReference{
					Name: trunkTestName,
					VLAN: v1alpha1.VLANConfig{Tag: tag},
				},
			},
		}
	}

	allResources := func(vnis []v1alpha1.L2VNI) config.Resources {
		return config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay(infra.Default),
			},
			L2Trunks: []v1alpha1.L2Trunk{*trunk.DeepCopy()},
			L3VNIs:   []v1alpha1.L3VNI{l3vni},
			L2VNIs:   vnis,
		}
	}

	createPod := func(name, nodeName string, nadObj nad.NetworkAttachmentDefinition, ip string) *corev1.Pod {
		GinkgoHelper()
		pod, err := k8s.CreateAgnhostPod(cs, name, trunkTestNamespace,
			k8s.WithNad(nadObj.Name, trunkTestNamespace, []string{ip}),
			k8s.OnNode(nodeName))
		Expect(err).NotTo(HaveOccurred())
		return pod
	}

	createNad := func(name, bridgeName string, vlan int32, gateways []string) nad.NetworkAttachmentDefinition {
		GinkgoHelper()
		nadObj, err := k8s.CreateBridgeNad(name, trunkTestNamespace, bridgeName, int(vlan), gateways)
		Expect(err).NotTo(HaveOccurred())
		return nadObj
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		cs = k8sclient.New()
		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routers.Dump(ginkgo.GinkgoWriter)

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay(infra.Default),
			},
		})).To(Succeed())
		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		Expect(infra.LeafBConfig.RedistributeConnected()).To(Succeed())

		allNodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		nodes, err = k8s.NodesWithLabel(allNodes, openperouter.InstrumentedNodesLabel)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "the trunk scenario needs two instrumented nodes")

		trunk = v1alpha1.L2Trunk{
			ObjectMeta: metav1.ObjectMeta{
				Name:      trunkTestName,
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L2TrunkSpec{
				Type: v1alpha1.L2TrunkTypeHost,
				HostMaster: &v1alpha1.HostMaster{
					Type: v1alpha1.LinuxBridge,
					LinuxBridge: &v1alpha1.LinuxBridgeConfig{
						Lifecycle: v1alpha1.BridgeLifecycleExternal,
						Name:      new(trunkTestBridge),
					},
				},
			},
		}
		trunkID = trunkIDFor(openperouter.Namespace, trunkTestName)
		hostVeth := "host-t-" + trunkID

		tags = []int32{trunkRedTag, trunkBlueTag}
		tags = append(tags, trunkScaleTagRange()...)
		for _, node := range nodes {
			Expect(setupTrunkHostBridge(executor.ForNode(node.Name))).To(Succeed())
		}

		By("creating the trunk before any L2VNI references it")
		Expect(Updater.Update(config.Resources{
			L2Trunks: []v1alpha1.L2Trunk{trunk},
		})).To(Succeed())
		hostVethIndexBeforeTags = make(map[string]string, len(nodes))
		for _, node := range nodes {
			nodeExec := executor.ForNode(node.Name)
			Eventually(func() error {
				return runOnNode(nodeExec, "ip", "link", "show", hostVeth)
			}).WithTimeout(2*time.Minute).Should(Succeed(),
				"trunk host leg should exist on %s before any L2VNI", node.Name)
			hostVethIndexBeforeTags[node.Name] = linkIndex(nodeExec, hostVeth)
		}

		l2vnis = []v1alpha1.L2VNI{
			trunkedL2VNI("red-100", trunkRedVNI, trunkRedTag,
				[]string{trunkRedGateway}, l3vniRoutingDomain("red")),
			trunkedL2VNI("blue-200", trunkBlueVNI, trunkBlueTag,
				[]string{trunkBlueGateway}, l3vniRoutingDomain("red")),
		}
		for i := range trunkScaleTags {
			l2vnis = append(l2vnis, trunkedL2VNI(
				fmt.Sprintf("scale-%d", trunkScaleTagBase+int32(i)),
				int32(10400+i),
				trunkScaleTagBase+int32(i),
				nil,
				nil,
			))
		}
		Expect(Updater.Update(allResources(l2vnis))).To(Succeed())

		By("admitting the configured tags, and one unmapped tag, on the trunk host leg")
		allowedTags := append(slices.Clone(tags), trunkIdleTag)
		for _, node := range nodes {
			Eventually(func() error {
				return allowTrunkTags(executor.ForNode(node.Name), hostVeth, allowedTags)
			}).WithTimeout(2*time.Minute).Should(Succeed(),
				"host leg should accept the trunk tags on %s", node.Name)
		}

		Eventually(func() error {
			routers, err = openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())

		_, err = k8s.CreateNamespace(cs, trunkTestNamespace)
		Expect(err).NotTo(HaveOccurred())

		By("creating the pods on the configured tags")
		redNad := createNad("trunk-100", trunkTestBridge, trunkRedTag, []string{trunkRedGateway})
		blueNad := createNad("trunk-200", trunkTestBridge, trunkBlueTag, []string{trunkBlueGateway})
		idleNad := createNad("trunk-unmapped", trunkTestBridge, trunkIdleTag, nil)
		scaleNad := createNad("trunk-400", trunkTestBridge, trunkScaleTagBase, nil)

		redPodA := createPod("red-a", nodes[0].Name, redNad, trunkRedPodA+"/24")
		redPodB := createPod("red-b", nodes[1].Name, redNad, trunkRedPodB+"/24")
		bluePodA := createPod("blue-a", nodes[0].Name, blueNad, trunkBluePodA+"/24")
		bluePodB := createPod("blue-b", nodes[1].Name, blueNad, trunkBluePodB+"/24")
		idlePod := createPod("idle-a", nodes[0].Name, idleNad, trunkIdlePod+"/24")
		_ = createPod("scale-a", nodes[0].Name, scaleNad, trunkScalePodIP(0, 2))
		_ = createPod("scale-b", nodes[1].Name, scaleNad, trunkScalePodIP(0, 3))

		for _, pod := range []*corev1.Pod{redPodA, redPodB, bluePodA, bluePodB, idlePod} {
			Expect(removeGatewayFromPod(pod)).To(Succeed())
		}

		redPodAExecutor = executor.ForPod(redPodA.Namespace, redPodA.Name, "agnhost")
		redPodBExecutor = executor.ForPod(redPodB.Namespace, redPodB.Name, "agnhost")
		bluePodAExecutor = executor.ForPod(bluePodA.Namespace, bluePodA.Name, "agnhost")
		bluePodBExecutor = executor.ForPod(bluePodB.Namespace, bluePodB.Name, "agnhost")
		idlePodExecutor = executor.ForPod(idlePod.Namespace, idlePod.Name, "agnhost")

		By("waiting for the BGP sessions to establish on every node")
		leafExec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validate.SessionWithNeighbor(leafExec, validate.SessionParameters{
				FromName:    infra.KindLeaf,
				ToName:      node.Name,
				NeighborIP:  neighborIP,
				Established: Established,
			})
		}
	})

	AfterAll(func() {
		dumpIfFails(cs, trunkTestNamespace)
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafBConfig.Reset()).To(Succeed())
		Expect(Updater.CleanAll()).To(Succeed())
		for _, node := range nodes {
			_ = teardownTrunkHostBridge(executor.ForNode(node.Name))
		}
		Expect(k8s.DeleteNamespace(cs, trunkTestNamespace)).To(Succeed())
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	It("creates the trunk devices before a tag is added and leaves them in place", func() {
		for _, node := range nodes {
			nodeExec := executor.ForNode(node.Name)
			Expect(runOnNode(nodeExec, "ip", "link", "show",
				"host-t-"+trunkID)).To(Succeed(),
				"trunk host leg should exist on %s", node.Name)
			Expect(linkIndex(nodeExec, "host-t-"+trunkID)).
				To(Equal(hostVethIndexBeforeTags[node.Name]),
					"adding the first L2VNI must not recreate the trunk veth on %s", node.Name)

			routerExec, err := openperouter.ExecutorForNode(routers, node.Name)
			Expect(err).NotTo(HaveOccurred())
			for _, device := range []string{
				"pe-t-" + trunkID,
				"br-t-" + trunkID,
				"vxt-" + trunkID,
			} {
				Expect(runOnNode(routerExec, "ip", "link", "show", device)).To(Succeed(),
					"trunk device %s should exist in the router netns on %s", device, node.Name)
			}
		}
	})

	It("establishes pod to pod connectivity on two tags and on their gateways", func() {
		checkPodIsReachable(redPodAExecutor, trunkRedPodA, trunkRedPodB)
		checkPodIsReachable(redPodBExecutor, trunkRedPodB, trunkRedPodA)
		checkPodIsReachable(bluePodAExecutor, trunkBluePodA, trunkBluePodB)
		checkPodIsReachable(bluePodBExecutor, trunkBluePodB, trunkBluePodA)

		canPingFromPod(redPodAExecutor, discardAddressLength(trunkRedGateway))
		canPingFromPod(bluePodBExecutor, discardAddressLength(trunkBlueGateway))
	})

	It("routes between two tags through the shared VRF", func() {
		checkPodIsReachable(redPodAExecutor, trunkRedPodA, trunkBluePodB)
		checkPodIsReachable(bluePodAExecutor, trunkBluePodA, trunkRedPodB)
	})

	It("reaches the trunk tags from the fabric", func() {
		checkPodIsReachableWithExpected(
			executor.ForContainer("clab-kind-hostA_red"),
			infra.HostARedIPv4, trunkRedPodA, infra.HostARedIPv4, defaultPodReachabilityTimeout,
		)
		checkPodIsReachableWithExpected(
			executor.ForContainer("clab-kind-hostB_red"),
			infra.HostBRedIPv4, trunkBluePodA, infra.HostBRedIPv4, defaultPodReachabilityTimeout,
		)
	})

	It("does not reach the mapped tag from an unmapped one or with untagged frames", func() {
		By("a pod on a tag the host admits but no L2VNI maps cannot reach the mapped tag")
		Consistently(func() error {
			_, err := idlePodExecutor.Exec("ping", "-c", "1", "-W", "2", "-I", "net1", trunkRedPodA)
			return err
		}, 10*time.Second, time.Second).ShouldNot(Succeed())

		nodeExec := executor.ForNode(nodes[0].Name)

		By("untagged frames on the trunk are dropped")
		Expect(runOnNode(nodeExec, "sh", "-c", strings.Join([]string{
			"ip link del veth-untagged 2>/dev/null || true",
			"ip link add veth-untagged type veth peer name veth-untag-end",
			"ip link set veth-untagged master " + trunkTestBridge,
			"ip addr add " + trunkUntaggedIP + "/24 dev veth-untag-end",
			"ip link set veth-untagged up",
			"ip link set veth-untag-end up",
		}, "; "))).To(Succeed())
		Consistently(func() error {
			_, err := nodeExec.Exec("ping", "-c", "1", "-W", "2", "-I", "veth-untag-end",
				discardAddressLength(trunkRedGateway))
			return err
		}, 10*time.Second, time.Second).ShouldNot(Succeed())

		Expect(runOnNode(nodeExec, "ip", "link", "del", "veth-untagged")).To(Succeed())

		By("the mapped tags still work after the negative checks")
		checkPodIsReachable(redPodAExecutor, trunkRedPodA, trunkRedPodB)
	})

	It("presents every mapping to the kernel and to FRR", func() {
		for _, node := range nodes {
			routerExec, err := openperouter.ExecutorForNode(routers, node.Name)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				out, err := routerExec.Exec("bridge", "vlan", "tunnelshow",
					"dev", "vxt-"+trunkID)
				if err != nil {
					return err
				}
				for i, tag := range tags {
					vni := trunkVNIForPosition(i)
					if !outputHasMapping(out, tag, vni) {
						return fmt.Errorf("missing vlan %d -> vni %d mapping on %s:\n%s",
							tag, vni, node.Name, out)
					}
				}
				return nil
			}).WithTimeout(45 * time.Second).Should(Succeed())

			Eventually(func() error {
				out, err := routerExec.Exec("bridge", "vni", "show",
					"dev", "vxt-"+trunkID)
				if err != nil {
					return err
				}
				// bridge vni show prints only the VNI column, so the
				// vid-to-VNI pairing is left to the tunnelshow check above.
				for i, tag := range tags {
					if vni := trunkVNIForPosition(i); !outputHasValue(out, vni) {
						return fmt.Errorf("missing vni %d for vlan %d on %s:\n%s",
							vni, tag, node.Name, out)
					}
				}
				return nil
			}).WithTimeout(45 * time.Second).Should(Succeed())

			Eventually(func() error {
				known, out, err := evpnVNIsKnownByZebra(routerExec)
				if err != nil {
					return err
				}
				for i, tag := range tags {
					vni := trunkVNIForPosition(i)
					if _, ok := known[vni]; !ok {
						return fmt.Errorf("zebra does not know VNI %d (vlan %d) on %s:\n%s",
							vni, tag, node.Name, out)
					}
				}
				return nil
			}).WithTimeout(45 * time.Second).Should(Succeed())

			for i := range tags {
				endpoint := fmt.Sprintf("br-pe-%d", trunkVNIForPosition(i))
				Expect(runOnNode(routerExec, "ip", "link", "show", endpoint)).To(Succeed(),
					"SVI %s should exist in the router netns on %s", endpoint, node.Name)
			}
		}
	})

	It("carries twenty tags on one trunk with working sampled connectivity", func() {
		Expect(tags).To(HaveLen(2 + trunkScaleTags))
		checkPodIsReachable(
			executor.ForPod(trunkTestNamespace, "scale-a", "agnhost"),
			discardAddressLength(trunkScalePodIP(0, 2)),
			discardAddressLength(trunkScalePodIP(0, 3)),
		)
	})

	It("keeps the other tag connected when one L2VNI is removed", func() {
		By("deleting the blue L2VNI")
		Expect(Updater.DeleteL2VNI("blue-200")).To(Succeed())

		checkPodIsReachable(redPodAExecutor, trunkRedPodA, trunkRedPodB)

		blueIsReachable := func() error {
			_, err := bluePodAExecutor.Exec("ping", "-c", "1", "-W", "2", "-I", "net1", trunkBluePodB)
			return err
		}
		By("waiting for the removed tag's dataplane to be torn down")
		Eventually(blueIsReachable).WithTimeout(2 * time.Minute).
			WithPolling(time.Second).ShouldNot(Succeed())
		By("keeping the removed tag down once the deletion is applied")
		Consistently(blueIsReachable, 10*time.Second, time.Second).ShouldNot(Succeed())

		Expect(Updater.Update(allResources(l2vnis))).To(Succeed())
		checkPodIsReachable(bluePodAExecutor, trunkBluePodA, trunkBluePodB)
	})

	It("restores the trunk mappings and connectivity after a router restart", PodModeOnly, func() {
		routerPods, err := openperouter.RouterPodsForNodes(cs, []corev1.Node{nodes[0]})
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).To(HaveLen(1))

		By("deleting the router pod on the first node")
		Expect(cs.CoreV1().Pods(routerPods[0].Namespace).Delete(context.Background(),
			routerPods[0].Name, metav1.DeleteOptions{})).To(Succeed())

		Eventually(func() error {
			restarted, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			if err := openperouter.AreReady(restarted); err != nil {
				return err
			}
			newRouter, err := openperouter.ExecutorForNode(restarted, nodes[0].Name)
			if err != nil {
				return err
			}
			out, err := newRouter.Exec("bridge", "vlan", "tunnelshow",
				"dev", "vxt-"+trunkID)
			if err != nil {
				return err
			}
			if !outputHasMapping(out, trunkRedTag, trunkRedVNI) {
				return fmt.Errorf("mapping for vlan %d missing after restart:\n%s", trunkRedTag, out)
			}
			return nil
		}).WithTimeout(3 * time.Minute).Should(Succeed())

		checkPodIsReachable(redPodAExecutor, trunkRedPodA, trunkRedPodB)
		checkPodIsReachable(bluePodBExecutor, trunkBluePodB, trunkBluePodA)
	})

	It("coexists with a standalone L2VNI on the same nodes", func() {
		const (
			standaloneVNI     = int32(500)
			standaloneGateway = "192.171.50.1/24"
			standalonePodA    = "192.171.50.2"
			standalonePodB    = "192.171.50.3"
		)
		standalone := v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "standalone-500",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L2VNISpec{
				VNI:           standaloneVNI,
				GatewayIPs:    []string{standaloneGateway},
				RoutingDomain: l3vniRoutingDomain("red"),
				HostMaster: &v1alpha1.HostMaster{
					Type: v1alpha1.LinuxBridge,
					LinuxBridge: &v1alpha1.LinuxBridgeConfig{
						Lifecycle: v1alpha1.BridgeLifecycleManaged,
					},
				},
			},
		}
		resources := allResources(l2vnis)
		resources.L2VNIs = append(resources.L2VNIs, standalone)
		Expect(Updater.Update(resources)).To(Succeed())

		standaloneNad := createNad("standalone-500", "br-hs-500", 0, []string{standaloneGateway})
		first := createPod("standalone-a", nodes[0].Name, standaloneNad, standalonePodA+"/24")
		second := createPod("standalone-b", nodes[1].Name, standaloneNad, standalonePodB+"/24")
		Expect(removeGatewayFromPod(first)).To(Succeed())
		Expect(removeGatewayFromPod(second)).To(Succeed())

		checkPodIsReachable(
			executor.ForPod(first.Namespace, first.Name, "agnhost"),
			standalonePodA, standalonePodB,
		)

		By("the trunk tags keep working next to the standalone VNI")
		checkPodIsReachable(redPodAExecutor, trunkRedPodA, trunkRedPodB)
	})
})

// trunkIDFor returns the identifier the controller derives from a trunk's
// namespace and name. The digest is bounded to trunkIDLength base-trunkIDBase
// digits and left-padded with '0' because big.Int.Text drops leading zeros.
func trunkIDFor(namespace, name string) string {
	digest := sha256.Sum256([]byte(namespace + "/" + name))
	id := new(big.Int).SetBytes(digest[:])
	id.Mod(id, trunkIDModulus)
	encoded := id.Text(trunkIDBase)
	if len(encoded) >= trunkIDLength {
		return encoded
	}
	return strings.Repeat("0", trunkIDLength-len(encoded)) + encoded
}

func trunkScaleTagRange() []int32 {
	tags := make([]int32, 0, trunkScaleTags)
	for i := range trunkScaleTags {
		tags = append(tags, trunkScaleTagBase+int32(i))
	}
	return tags
}

func trunkScalePodIP(i, host int) string {
	return fmt.Sprintf("10.200.%d.%d/24", i, host)
}

// trunkVNIForPosition maps a position in the trunk tag list to the VNI the
// suite configured for it.
func trunkVNIForPosition(i int) int32 {
	switch i {
	case 0:
		return trunkRedVNI
	case 1:
		return trunkBlueVNI
	}
	return int32(10400 + i - 2)
}

// outputHasMapping reports whether an iproute2 mapping dump carries the
// tag/VNI pair. iproute2 aggregates contiguous VLANs into one line
// ("400-419    10400-10419"), so ranges are paired positionally instead of
// compared as whole fields.
func outputHasMapping(out string, tag, vni int32) bool {
	for _, line := range strings.Split(out, "\n") {
		fields := numericFields(line)
		if len(fields) < 2 {
			continue
		}
		if fieldPairMatches(fields[0], fields[1], tag, vni) ||
			fieldPairMatches(fields[0], fields[1], vni, tag) {
			return true
		}
	}
	return false
}

// evpnVNIsKnownByZebra returns the VNIs zebra reports and the output it was
// read from. JSON is the stable surface: the text form is a detailed list on
// some FRR versions and a summary table on others, so it is only used as a
// fallback.
func evpnVNIsKnownByZebra(exec executor.Executor) (map[int32]struct{}, string, error) {
	jsonOut, err := exec.Exec("vtysh", "-c", "show evpn vni json")
	if err != nil {
		return nil, jsonOut, err
	}
	known, parseErr := parseEVPNVNIsJSON(jsonOut)
	if parseErr == nil {
		return known, jsonOut, nil
	}

	textOut, err := exec.Exec("vtysh", "-c", "show evpn vni")
	if err != nil {
		return nil, jsonOut, fmt.Errorf("unreadable show evpn vni json (%w) and text fallback failed: %w", parseErr, err)
	}
	known = map[int32]struct{}{}
	for _, line := range strings.Split(textOut, "\n") {
		fields := numericFields(line)
		if len(fields) == 0 {
			continue
		}
		if vni, ok := singleValue(fields[0]); ok {
			known[vni] = struct{}{}
		}
	}
	return known, textOut, nil
}

func parseEVPNVNIsJSON(out string) (map[int32]struct{}, error) {
	known := map[int32]struct{}{}
	var keyed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &keyed); err == nil {
		for key := range keyed {
			vni, err := strconv.Atoi(key)
			if err != nil {
				continue
			}
			known[int32(vni)] = struct{}{}
		}
	}
	var list []struct {
		VNI int32 `json:"vni"`
	}
	if len(known) == 0 {
		if err := json.Unmarshal([]byte(out), &list); err == nil {
			for _, entry := range list {
				if entry.VNI != 0 {
					known[entry.VNI] = struct{}{}
				}
			}
		}
	}
	if len(known) == 0 {
		return nil, fmt.Errorf("no VNIs in show evpn vni json output: %s", out)
	}
	return known, nil
}

func singleValue(field string) (int32, bool) {
	low, high, ok := parseField(field)
	if !ok || low != high {
		return 0, false
	}
	return low, true
}

// outputHasValue reports whether an iproute2 dump carries the value, which
// may be aggregated into a range such as "10400-10419".
func outputHasValue(out string, value int32) bool {
	for _, line := range strings.Split(out, "\n") {
		for _, field := range numericFields(line) {
			low, high, ok := parseField(field)
			if ok && value >= low && value <= high {
				return true
			}
		}
	}
	return false
}

func numericFields(line string) []string {
	fields := strings.Fields(line)
	numeric := make([]string, 0, len(fields))
	for _, field := range fields {
		if _, _, ok := parseField(field); ok {
			numeric = append(numeric, field)
		}
	}
	return numeric
}

// fieldPairMatches pairs the values of two columns, which may be single
// numbers or ranges of the same width, and reports whether x maps to y.
func fieldPairMatches(left, right string, x, y int32) bool {
	leftLow, leftHigh, ok := parseField(left)
	if !ok {
		return false
	}
	rightLow, rightHigh, ok := parseField(right)
	if !ok {
		return false
	}
	if rightHigh-rightLow != leftHigh-leftLow {
		return x >= leftLow && x <= leftHigh && y >= rightLow && y <= rightHigh
	}
	if x < leftLow || x > leftHigh || y < rightLow || y > rightHigh {
		return false
	}
	return x-leftLow == y-rightLow
}

func parseField(field string) (int32, int32, bool) {
	start, end, isRange := strings.Cut(field, "-")
	low, err := strconv.Atoi(start)
	if err != nil || low < 0 {
		return 0, 0, false
	}
	if !isRange {
		return int32(low), int32(low), true
	}
	high, err := strconv.Atoi(end)
	if err != nil || high < low {
		return 0, 0, false
	}
	return int32(low), int32(high), true
}

func runOnNode(exec executor.Executor, cmd string, args ...string) error {
	GinkgoHelper()
	out, err := exec.Exec(cmd, args...)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w. Output: %s", cmd, strings.Join(args, " "), err, out)
	}
	return nil
}

func linkIndex(exec executor.Executor, name string) string {
	GinkgoHelper()
	out, err := exec.Exec("sh", "-c", fmt.Sprintf("cat /sys/class/net/%s/ifindex", name))
	Expect(err).NotTo(HaveOccurred())
	return strings.TrimSpace(out)
}

func setupTrunkHostBridge(exec executor.Executor) error {
	return runOnNode(exec, "sh", "-c", strings.Join([]string{
		"ip link show " + trunkTestBridge + " >/dev/null 2>&1 || ip link add " + trunkTestBridge + " type bridge",
		"ip link set " + trunkTestBridge + " type bridge vlan_filtering 1 vlan_default_pvid 0",
		"ip link set " + trunkTestBridge + " up",
	}, "; "))
}

func teardownTrunkHostBridge(exec executor.Executor) error {
	return runOnNode(exec, "sh", "-c", strings.Join([]string{
		"ip link del " + trunkTestBridge + " 2>/dev/null || true",
		"ip link del veth-untagged 2>/dev/null || true",
	}, "; "))
}

func allowTrunkTags(exec executor.Executor, hostVeth string, tags []int32) error {
	out, err := exec.Exec("bridge", "vlan", "show", "dev", hostVeth)
	if err != nil {
		return fmt.Errorf("reading vlan membership of %s: %w. Output: %s", hostVeth, err, out)
	}
	var missing []string
	for _, tag := range tags {
		vid := strconv.Itoa(int(tag))
		if !outputHasField(out, vid) {
			missing = append(missing, vid)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	// iproute2 takes one vid per invocation: a list exits zero and silently
	// admits only the first one.
	commands := make([]string, 0, len(missing))
	for _, vid := range missing {
		commands = append(commands, fmt.Sprintf("bridge vlan add dev %s vid %s", hostVeth, vid))
	}
	return runOnNode(exec, "sh", "-c", strings.Join(commands, " && "))
}

func outputHasField(out, value string) bool {
	for _, line := range strings.Split(out, "\n") {
		if slices.Contains(strings.Fields(line), value) {
			return true
		}
	}
	return false
}
