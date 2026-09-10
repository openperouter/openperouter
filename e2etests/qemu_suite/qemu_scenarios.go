// SPDX-License-Identifier:Apache-2.0

package qemu_e2e

import (
	"fmt"
	"strings"
	"time"

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
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	emptyPrefixes       = []string{}
	leafAVRFRedPrefixes = []string{"192.168.20.0/24", "2001:db8:20::/64"}
)

var AcceleratedUnderlay = v1alpha1.Underlay{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "underlay",
		Namespace: openperouter.Namespace,
	},
	Spec: v1alpha1.UnderlaySpec{
		ASN: 64514,
		Interfaces: []v1alpha1.UnderlayInterface{
			{
				Type: "NetworkDevice",
				NetworkDevice: &v1alpha1.NetworkDevice{
					InterfaceName: "toswitch1",
				},
			},
			{
				Type: "NetworkDevice",
				NetworkDevice: &v1alpha1.NetworkDevice{
					InterfaceName: "toswitch2",
				},
			},
		},
		Neighbors: []v1alpha1.Neighbor{
			{
				ASN:                  new(int64(64512)),
				Address:              new("192.168.11.2"),
				ConnectTimeSeconds:   new(int64(5)),
				KeepaliveTimeSeconds: new(int64(3)),
				HoldTimeSeconds:      new(int64(9)),
			},
			{
				ASN:                  new(int64(64513)),
				Address:              new("192.168.12.2"),
				ConnectTimeSeconds:   new(int64(5)),
				KeepaliveTimeSeconds: new(int64(3)),
				HoldTimeSeconds:      new(int64(9)),
			},
		},
		TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
			CIDRs: []string{"100.65.0.0/24"},
		},
	},
}

// --- EVPN accelerated scenarios ---

const testNamespace = "test-clab-l2vni"

var _ = Describe("QEMU scenarios", Ordered, GroutSupport, func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	var nodes []corev1.Node

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())
		cs = k8sclient.New()

		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		routers.Dump(GinkgoWriter)

		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())

		By("Creating underlay")
		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{AcceleratedUnderlay},
		})).To(Succeed())

		By("Verifying BGP sessions with leafkind1")
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
		Expect(Updater.CleanAll()).To(Succeed())
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs, testNamespace)
		Expect(Updater.CleanButUnderlay()).To(Succeed())
		Expect(infra.LeafAConfig.Reset()).To(Succeed())
		Expect(infra.LeafBConfig.Reset()).To(Succeed())
	})

	It("should configure L3Passthrough host session in FRR", func() {
		passthrough := v1alpha1.L3Passthrough{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "passthrough",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3PassthroughSpec{
				HostSession: v1alpha1.HostSession{
					ASN:        64514,
					HostASN:    new(int64(64515)),
					LocalCIDRs: []string{"192.169.10.0/24"},
				},
			},
		}

		By("Creating L3Passthrough")
		Expect(Updater.Update(config.Resources{
			L3Passthrough: []v1alpha1.L3Passthrough{passthrough},
		})).To(Succeed())

		By("Verifying host session CIDR in FRR running config")
		for exec := range routers.GetExecutors() {
			Eventually(func() error {
				cfg, err := frr.RunningConfig(exec)
				if err != nil {
					return fmt.Errorf("failed to get FRR running config from %s: %w", exec.Name(), err)
				}
				if !strings.Contains(cfg, "192.169.10.") {
					return fmt.Errorf("FRR config on %s does not contain host session CIDR:\n%s", exec.Name(), cfg)
				}
				return nil
			}, 2*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		}
	})

	It("should receive Type-5 routes via L3VNI", func() {
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

		By("Creating L3VNI red")
		Expect(Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{l3vniRed},
		})).To(Succeed())

		By("Configuring leafA to advertise routes in VRF red")
		Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, leafAVRFRedPrefixes, emptyPrefixes)).To(Succeed())

		By("Verifying Type-5 routes received from leafA")
		for exec := range routers.GetExecutors() {
			validate.Type5RouteExists(exec, "192.168.20.0/24")
		}
	})
})
