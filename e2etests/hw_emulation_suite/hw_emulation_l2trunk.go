// SPDX-License-Identifier:Apache-2.0

package qemu_e2e

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// Emulated VFs of the toleafkind1 PF in the QEMU VM, see
// clab/qemu/vm/create-vfs.sh. The controller runs with
// TEST_FLAG_DO_NOT_USE_EMULATED_SRIOV_VFS set, so VF <index> resolves to the
// netdev "toleafkind1v<index>".
const (
	emulatedPF          = "toleafkind1"
	trunkVFIndex        = int32(1)
	vlan33VF1           = "toleafkind1v2"
	vlan33VF2           = "toleafkind1v3"
	vlan44VF1           = "toleafkind1v4"
	vlan44VF2           = "toleafkind1v5"
	l2trunkTestNS       = "test-l2trunk-sriov"
	vlanA               = int32(33)
	vlanB               = int32(44)
	trunkVFKernelDriver = "virtio-pci"
)

var trunkVFNetdev = fmt.Sprintf("%sv%d", emulatedPF, trunkVFIndex)

var _ = Describe("HWEmulation SR-IOV L2Trunk", Ordered, QEMUSupport, GroutSupport, func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	// trunkVFPCIAddress maps each node to the PCI address of its trunk VF.
	trunkVFPCIAddress := map[string]string{}

	l3vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			VNI: 100,
			HostSession: &v1alpha1.HostSession{
				ASN:        64514,
				HostASN:    new(int64(64515)),
				LocalCIDRs: []string{"192.169.10.0/24"},
			},
		},
	}

	trunk := v1alpha1.L2Trunk{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sriov-trunk",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2TrunkSpec{
			Type: v1alpha1.L2TrunkTypeSRIOV,
			SRIOV: &v1alpha1.SRIOVTrunkConfig{
				PFName:  emulatedPF,
				VFIndex: new(trunkVFIndex),
			},
		},
	}

	l2vniA := trunkL2VNI("tenant-a", 110, vlanA, "10.110.0.1/24", trunk.Name, l3vniRed.Name)
	l2vniB := trunkL2VNI("tenant-b", 120, vlanB, "10.120.0.1/24", trunk.Name, l3vniRed.Name)
	l2vniC := hostBridgeL2VNI("tenant-c", 130, "10.130.0.1/24", l3vniRed.Name)

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())
		cs = k8sclient.New()

		var err error
		routers, err = openperouter.Get(cs, false)
		Expect(err).To(Succeed())
		routers.Dump(GinkgoWriter)

		// The trunk VF loses its netdev once bound to vfio-pci, so its PCI
		// address is read before the trunk is created.
		for router := range routers.GetExecutors() {
			nodeExec := executor.ForNode(router.NodeName())
			pciAddr, err := emulatedVFPCIAddress(nodeExec, trunkVFNetdev)
			Expect(err).To(Succeed(), "node %s", router.NodeName())
			trunkVFPCIAddress[router.NodeName()] = pciAddr

			for _, vf := range []string{vlan33VF1, vlan33VF2, vlan44VF1, vlan44VF2} {
				Expect(nodeExec.Exec("ip", "link", "set", vf, "up")).Error().To(Succeed(),
					"node %s, VF %s", router.NodeName(), vf)
			}
		}

		By("Creating the accelerated underlay")
		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay(infra.Default)},
		})).To(Succeed())

		By("Verifying BGP sessions with leafkind1")
		leafExec := executor.ForContainer(infra.KindLeaf)
		for router := range routers.GetExecutors() {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, router.NodeName())
			Expect(err).To(Succeed())
			validate.SessionWithNeighbor(leafExec, validate.SessionParameters{
				FromName:    infra.KindLeaf,
				ToName:      router.NodeName(),
				NeighborIP:  neighborIP,
				Established: Established,
			})
		}
	})

	AfterAll(func() {
		Expect(k8s.DeleteNamespace(cs, l2trunkTestNS)).To(Succeed())
		Expect(Updater.CleanAll()).To(Succeed())
		Eventually(func() error {
			routers, err := openperouter.Get(cs, false)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).Should(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs, l2trunkTestNS)
	})

	It("should bind the trunk VF to grout and carry both L2VNIs on it", func() {
		Expect(Updater.Update(config.Resources{
			L3VNIs:   []v1alpha1.L3VNI{l3vniRed},
			L2Trunks: []v1alpha1.L2Trunk{trunk},
			L2VNIs:   []v1alpha1.L2VNI{l2vniA, l2vniB},
		})).To(Succeed())

		for router := range routers.GetExecutors() {
			pciAddr := trunkVFPCIAddress[router.NodeName()]
			portName := trunkPortName(emulatedPF, trunkVFIndex)
			Eventually(func(g Gomega) {
				g.Expect(showGroutInterface(g, router, portName)).To(And(
					HaveField("Type", "port"),
					HaveField("Devargs", pciAddr),
					HaveField("Flags", ContainElement("up")),
				))
				for _, tag := range []int32{vlanA, vlanB} {
					g.Expect(showGroutInterface(g, router, trunkVLANName(emulatedPF, trunkVFIndex, tag))).To(And(
						HaveField("VLANParent", portName),
						HaveField("VLANID", tag),
					))
				}
				expectBoundToVFIO(g, router, pciAddr)
			}, 2*time.Minute, time.Second).Should(Succeed(), "router %s, port %s", router.Name(), portName)
		}
		openperouter.AssertNodesStatusReady(Updater.Client())
	})

	It("should connect the workload VFs on the same VLAN through the trunk", func() {
		// Each pod gets its own VF, so that same-VNI traffic crosses the
		// emulated embedded switch instead of staying inside the node.
		pods := []l2vniPod{
			{name: "tenant-a-1", vni: l2vniA, ip: "10.110.0.10/24", nadMaster: vlan33VF1},
			{name: "tenant-a-2", vni: l2vniA, ip: "10.110.0.11/24", nadMaster: vlan33VF2},
			{name: "tenant-b-1", vni: l2vniB, ip: "10.120.0.10/24", nadMaster: vlan44VF1},
			{name: "tenant-b-2", vni: l2vniB, ip: "10.120.0.11/24", nadMaster: vlan44VF2},
		}

		ensureTestNamespace(cs, l2trunkTestNS)

		// The pods share their L2 segments across nodes, so they all run
		// on a single node to avoid address conflicts.
		executors := createL2VNIPods(cs, l2trunkTestNS, firstRouterNode(routers), pods)

		for _, from := range pods {
			By(fmt.Sprintf("pinging the gateway of VNI %d from %s", from.vni.Spec.VNI, from.name))
			canPingFromPod(executors[from.name], addressOnly(from.vni.Spec.GatewayIPs[0]))

			for _, to := range pods {
				if to.name == from.name || to.vni.Name != from.vni.Name {
					continue
				}
				By(fmt.Sprintf("pinging %s from %s", to.name, from.name))
				canPingFromPod(executors[from.name], addressOnly(to.ip))
			}
		}
	})

	It("should route between a tap-attached L2VNI and a trunk-attached L2VNI in the same L3VNI", func() {
		By("adding the tap-attached L2VNI to the same L3VNI as the trunked ones")
		Expect(Updater.Update(config.Resources{
			L3VNIs:   []v1alpha1.L3VNI{l3vniRed},
			L2Trunks: []v1alpha1.L2Trunk{trunk},
			L2VNIs:   []v1alpha1.L2VNI{l2vniA, l2vniB, l2vniC},
		})).To(Succeed())
		openperouter.AssertNodesStatusReady(Updater.Client())

		ensureTestNamespace(cs, l2trunkTestNS)

		// tenant-c reaches its L2VNI through the managed host bridge of a
		// plain L2VNI, while tenant-a rides the trunk on one of its workload
		// VFs. The trunk pod gets a fresh IP to avoid colliding with the pods
		// left over by the previous spec.
		pods := []l2vniPod{
			{name: "tenant-c-tap", vni: l2vniC, ip: "10.130.0.10/24", nadMaster: hostBridgeName(l2vniC.Spec.VNI)},
			{name: "tenant-a-trunk", vni: l2vniA, ip: "10.110.0.12/24", nadMaster: vlan33VF1},
		}

		// The pods share their L2 segments across nodes, so they all run
		// on a single node to avoid address conflicts.
		executors := createL2VNIPods(cs, l2trunkTestNS, firstRouterNode(routers), pods)

		for _, p := range pods {
			By(fmt.Sprintf("pinging the gateway of VNI %d from %s", p.vni.Spec.VNI, p.name))
			canPingFromPod(executors[p.name], addressOnly(p.vni.Spec.GatewayIPs[0]))
		}

		// The pods sit in different L2VNIs, so reaching each other proves the
		// traffic is routed through the shared L3VNI (symmetric IRB).
		for _, from := range pods {
			for _, to := range pods {
				if to.name == from.name {
					continue
				}
				By(fmt.Sprintf("pinging %s from %s", to.name, from.name))
				canPingFromPod(executors[from.name], addressOnly(to.ip))
			}
		}
	})

	It("should hand the trunk VF back to the kernel when the trunk is removed", func() {
		Expect(Updater.CleanButUnderlay()).To(Succeed())

		for router := range routers.GetExecutors() {
			pciAddr := trunkVFPCIAddress[router.NodeName()]
			portName := trunkPortName(emulatedPF, trunkVFIndex)
			nodeExec := executor.ForNode(router.NodeName())
			Eventually(func(g Gomega) {
				g.Expect(groutInterfaces(g, router)).NotTo(ContainElement(HaveField("Name", portName)))
				g.Expect(nodeExec.Exec("readlink", filepath.Join("/sys/bus/pci/devices", pciAddr, "driver"))).
					To(MatchRegexp(`/%s\s*$`, trunkVFKernelDriver))
				g.Expect(emulatedVFPCIAddress(nodeExec, trunkVFNetdev)).To(Equal(pciAddr))
			}, 2*time.Minute, time.Second).Should(Succeed(), "router %s, port %s", router.Name(), portName)
		}
		openperouter.AssertNodesStatusReady(Updater.Client())
	})
})

// l2vniPod is a pod attached to an L2VNI through a macvlan NAD. The NAD master
// is a workload VF when the L2VNI rides a trunk, or the managed host bridge of
// a plain L2VNI.
type l2vniPod struct {
	name      string
	vni       v1alpha1.L2VNI
	ip        string
	nadMaster string
}

type groutInterface struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Flags      []string `json:"flags"`
	Devargs    string   `json:"devargs"`
	VLANParent string   `json:"vlan_parent"`
	VLANID     int32    `json:"vlan_id"`
}

func trunkL2VNI(name string, vni int32, tag int32, gatewayIP, trunkName, l3vniName string) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: vni,
			RoutingDomain: &v1alpha1.RoutingDomain{
				Type:  v1alpha1.RoutingDomainTypeL3VNI,
				L3VNI: &v1alpha1.L3VNIReference{Name: l3vniName},
			},
			GatewayIPs: []string{gatewayIP},
			Trunk: &v1alpha1.L2TrunkReference{
				Name: trunkName,
				VLAN: v1alpha1.VLANConfig{Tag: tag},
			},
		},
	}
}

// hostBridgeL2VNI returns an L2VNI that does not ride a trunk: grout exposes
// it to the host through a TAP enslaved to a managed Linux bridge, which
// workloads attach to.
func hostBridgeL2VNI(name string, vni int32, gatewayIP, l3vniName string) v1alpha1.L2VNI {
	return v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: vni,
			RoutingDomain: &v1alpha1.RoutingDomain{
				Type:  v1alpha1.RoutingDomainTypeL3VNI,
				L3VNI: &v1alpha1.L3VNIReference{Name: l3vniName},
			},
			GatewayIPs: []string{gatewayIP},
			HostMaster: &v1alpha1.HostMaster{
				Type: v1alpha1.LinuxBridge,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		},
	}
}

func groutInterfaces(g Gomega, router openperouter.RouterExecutor) []groutInterface {
	out, err := router.Exec("grcli", "--err-exit", "--json", "interface", "show")
	g.Expect(err).To(Succeed(), "router %s: %s", router.Name(), out)
	var ifaces []groutInterface
	g.Expect(json.Unmarshal([]byte(out), &ifaces)).To(Succeed(), out)
	return ifaces
}

// showGroutInterface returns the details of a single grout interface. Unlike
// the interface list, they include the type-specific fields such as devargs
// and the VLAN parent.
func showGroutInterface(g Gomega, router openperouter.RouterExecutor, name string) groutInterface {
	out, err := router.Exec("grcli", "--err-exit", "--json", "interface", "show", "name", name)
	g.Expect(err).To(Succeed(), "router %s, interface %s: %s", router.Name(), name, out)
	var iface groutInterface
	g.Expect(json.Unmarshal([]byte(out), &iface)).To(Succeed(), out)
	return iface
}

// trunkPortName mirrors the name the controller gives to a trunk port: the
// last 10 characters of the PF name and the VF index in two hexadecimal
// digits.
func trunkPortName(pfName string, vfIndex int32) string {
	return fmt.Sprintf("%s%02x", lastChars(pfName, 10), vfIndex)
}

// trunkVLANName mirrors the name the controller gives to a VLAN sub-interface
// of a trunk: the trunk port name followed by the VLAN tag in three
// hexadecimal digits.
func trunkVLANName(pfName string, vfIndex, tag int32) string {
	return trunkPortName(pfName, vfIndex) + fmt.Sprintf("%03x", tag)
}

// lastChars returns the last n characters of s.
func lastChars(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func firstRouterNode(routers openperouter.Routers) string {
	GinkgoHelper()
	for router := range routers.GetExecutors() {
		return router.NodeName()
	}
	Fail("no routers found")
	return ""
}

// hostBridgeName mirrors the name the controller gives to the managed host
// bridge of an L2VNI.
func hostBridgeName(vni int32) string {
	return fmt.Sprintf("br-hs-%d", vni)
}

// ensureTestNamespace creates the test namespace, tolerating the one left over
// by a previous spec of this ordered suite.
func ensureTestNamespace(cs clientset.Interface, name string) {
	GinkgoHelper()
	if _, err := k8s.CreateNamespace(cs, name); err != nil {
		Expect(apierrors.IsAlreadyExists(err)).To(BeTrue(), "creating namespace %s: %v", name, err)
	}
}

// createL2VNIPods creates one macvlan NAD and one agnhost pod per entry, all
// on the given node, and returns the pods' executors keyed by pod name.
func createL2VNIPods(cs clientset.Interface, namespace, nodeName string, pods []l2vniPod) map[string]executor.Executor {
	GinkgoHelper()
	executors := map[string]executor.Executor{}
	for _, p := range pods {
		By(fmt.Sprintf("creating pod %s in VNI %d", p.name, p.vni.Spec.VNI))
		nad, err := k8s.CreateMacvlanNad(p.name, namespace, p.nadMaster, p.vni.Spec.GatewayIPs)
		Expect(err).To(Succeed())
		pod, err := k8s.CreateAgnhostPod(cs, p.name, namespace,
			k8s.WithNad(nad.Name, namespace, []string{p.ip}),
			k8s.OnNode(nodeName),
		)
		Expect(err).To(Succeed())
		executors[p.name] = executor.ForPod(pod.Namespace, pod.Name, "agnhost")
	}
	return executors
}

func canPingFromPod(exec executor.Executor, ip string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		out, err := exec.Exec("ping", "-c", "1", "-W", "2", "-I", "net1", ip)
		g.Expect(err).To(Succeed(), "ping to %s failed: %s", ip, out)
	}).WithTimeout(40 * time.Second).WithPolling(time.Second).Should(Succeed())
}

func addressOnly(cidr string) string {
	return strings.Split(cidr, "/")[0]
}

// emulatedVFPCIAddress returns the PCI address of an emulated VF netdev. The
// emulated VFs are virtio-net NICs, whose netdev device is a virtio child of
// the PCI device, so the address is the parent of the device link target.
func emulatedVFPCIAddress(nodeExec executor.Executor, netdev string) (string, error) {
	// The path is not built with filepath.Join, which would drop "device/.."
	// before readlink resolves the symlink.
	out, err := nodeExec.Exec("readlink", "-f", "/sys/class/net/"+netdev+"/device/..")
	if err != nil {
		return "", fmt.Errorf("resolving PCI address of %s: %w: %s", netdev, err, out)
	}
	return filepath.Base(strings.TrimSpace(out)), nil
}
