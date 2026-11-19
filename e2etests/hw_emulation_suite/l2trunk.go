// SPDX-License-Identifier:Apache-2.0

package qemu_e2e

import (
	"fmt"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/grout"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	"github.com/openperouter/openperouter/e2etests/pkg/workloads"
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

var _ = Describe("SR-IOV L2Trunk", Ordered, QEMUSupport, GroutSupport, func() {
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

	l2vniA := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-a",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: 110,
			RoutingDomain: &v1alpha1.RoutingDomain{
				Type:  v1alpha1.RoutingDomainTypeL3VNI,
				L3VNI: &v1alpha1.L3VNIReference{Name: l3vniRed.Name},
			},
			GatewayIPs: []string{"10.110.0.1/24"},
			Trunk: &v1alpha1.L2TrunkReference{
				Name: trunk.Name,
				VLAN: v1alpha1.VLANConfig{Tag: vlanA},
			},
		},
	}

	l2vniB := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-b",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: 120,
			RoutingDomain: &v1alpha1.RoutingDomain{
				Type:  v1alpha1.RoutingDomainTypeL3VNI,
				L3VNI: &v1alpha1.L3VNIReference{Name: l3vniRed.Name},
			},
			GatewayIPs: []string{"10.120.0.1/24"},
			Trunk: &v1alpha1.L2TrunkReference{
				Name: trunk.Name,
				VLAN: v1alpha1.VLANConfig{Tag: vlanB},
			},
		},
	}

	// tenant-c does not ride a trunk: grout exposes it to the host through a
	// TAP enslaved to a managed Linux bridge, which workloads attach to.
	l2vniC := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tenant-c",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: 130,
			RoutingDomain: &v1alpha1.RoutingDomain{
				Type:  v1alpha1.RoutingDomainTypeL3VNI,
				L3VNI: &v1alpha1.L3VNIReference{Name: l3vniRed.Name},
			},
			GatewayIPs: []string{"10.130.0.1/24"},
			HostMaster: &v1alpha1.HostMaster{
				Type: v1alpha1.LinuxBridge,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{
					Lifecycle: v1alpha1.BridgeLifecycleManaged,
				},
			},
		},
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())
		cs = k8sclient.New()
		Expect(k8s.CreateNamespace(cs, l2trunkTestNS)).Error().To(Succeed())

		var err error
		routers, err = openperouter.Get(cs, false)
		Expect(err).To(Succeed())
		routers.Dump(GinkgoWriter)

		// The trunk VF loses its netdev once bound to vfio-pci, so its PCI
		// address is read before the trunk is created.
		for router := range routers.GetExecutors() {
			nodeExec := executor.ForNode(router.NodeName())
			pciAddr, err := grout.EmulatedVFPCIAddress(nodeExec, trunkVFNetdev)
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
			portName := grout.TrunkPortName(emulatedPF, trunkVFIndex)
			Eventually(func(g Gomega) {
				g.Expect(grout.InterfaceForName(g, router, portName)).To(And(
					HaveField("Type", "port"),
					HaveField("Devargs", pciAddr),
					HaveField("Flags", ContainElement("up")),
				))
				for _, tag := range []int32{vlanA, vlanB} {
					g.Expect(grout.InterfaceForName(g, router, grout.TrunkVLANName(emulatedPF, trunkVFIndex, tag))).To(And(
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
		pods := []workloads.L2VNIPod{
			{Name: "tenant-a-1", VNI: l2vniA, IP: "10.110.0.10/24", NADMaster: vlan33VF1},
			{Name: "tenant-a-2", VNI: l2vniA, IP: "10.110.0.11/24", NADMaster: vlan33VF2},
			{Name: "tenant-b-1", VNI: l2vniB, IP: "10.120.0.10/24", NADMaster: vlan44VF1},
			{Name: "tenant-b-2", VNI: l2vniB, IP: "10.120.0.11/24", NADMaster: vlan44VF2},
		}

		// The pods share their L2 segments across nodes, so they all run
		// on a single node to avoid address conflicts.
		executors := workloads.CreateL2VNIPods(cs, l2trunkTestNS, openperouter.FirstRouterNode(routers), pods)

		for _, from := range pods {
			By(fmt.Sprintf("pinging the gateway of VNI %d from %s", from.VNI.Spec.VNI, from.Name))
			workloads.CanPingFromPod(executors[from.Name], workloads.AddressOnly(from.VNI.Spec.GatewayIPs[0]))

			for _, to := range pods {
				if to.Name == from.Name || to.VNI.Name != from.VNI.Name {
					continue
				}
				By(fmt.Sprintf("pinging %s from %s", to.Name, from.Name))
				workloads.CanPingFromPod(executors[from.Name], workloads.AddressOnly(to.IP))
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

		By("advertising the external red subnet from leafA as a Type-5 route")
		Expect(infra.LeafAConfig.RedistributeConnected()).To(Succeed())
		DeferCleanup(infra.LeafAConfig.Reset)

		// tenant-c reaches its L2VNI through the managed host bridge of a
		// plain L2VNI, while tenant-a rides the trunk on one of its workload
		// VFs. The trunk pod gets a fresh IP to avoid colliding with the pods
		// left over by the previous spec.
		pods := []workloads.L2VNIPod{
			{Name: "tenant-c-tap", VNI: l2vniC, IP: "10.130.0.10/24", NADMaster: workloads.HostBridgeName(l2vniC.Spec.VNI)},
			{Name: "tenant-a-trunk", VNI: l2vniA, IP: "10.110.0.12/24", NADMaster: vlan33VF1},
		}

		// The pods share their L2 segments across nodes, so they all run
		// on a single node to avoid address conflicts.
		executors := workloads.CreateL2VNIPods(cs, l2trunkTestNS, openperouter.FirstRouterNode(routers), pods)

		for _, p := range pods {
			By(fmt.Sprintf("pinging the gateway of VNI %d from %s", p.VNI.Spec.VNI, p.Name))
			workloads.CanPingFromPod(executors[p.Name], workloads.AddressOnly(p.VNI.Spec.GatewayIPs[0]))
		}

		// The pods sit in different L2VNIs, so reaching each other proves the
		// traffic is routed through the shared L3VNI (symmetric IRB).
		for _, from := range pods {
			for _, to := range pods {
				if to.Name == from.Name {
					continue
				}
				By(fmt.Sprintf("pinging %s from %s", to.Name, from.Name))
				workloads.CanPingFromPod(executors[from.Name], workloads.AddressOnly(to.IP))
			}
		}

		for _, p := range pods {
			By(fmt.Sprintf("pinging hostA_red from %s", p.Name))
			workloads.CanPingFromPod(executors[p.Name], infra.HostARedIPv4)
		}
	})

	It("should hand the trunk VF back to the kernel when the trunk is removed", func() {
		Expect(Updater.CleanButUnderlay()).To(Succeed())

		for router := range routers.GetExecutors() {
			pciAddr := trunkVFPCIAddress[router.NodeName()]
			portName := grout.TrunkPortName(emulatedPF, trunkVFIndex)
			nodeExec := executor.ForNode(router.NodeName())
			Eventually(func(g Gomega) {
				g.Expect(grout.Interfaces(g, router)).NotTo(ContainElement(HaveField("Name", portName)))
				g.Expect(nodeExec.Exec("readlink", filepath.Join("/sys/bus/pci/devices", pciAddr, "driver"))).
					To(MatchRegexp(`/%s\s*$`, trunkVFKernelDriver))
				g.Expect(grout.EmulatedVFPCIAddress(nodeExec, trunkVFNetdev)).To(Equal(pciAddr))
			}, 2*time.Minute, time.Second).Should(Succeed(), "router %s, port %s", router.Name(), portName)
		}
		openperouter.AssertNodesStatusReady(Updater.Client())
	})
})
