// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
)

var ValidatorPath string

var cniBinaries = []string{"macvlan", "ipvlan", "static", "dhcp"}

const (
	cniBinDir   = "/opt/openperouter/cni/bin"
	cniCacheDir = "/var/lib/openperouter/cni/cache"
)

type cniTarget struct {
	exec  executor.Executor
	label string
}

const (
	underlayConfiguredTestSelector      = "EXTERNAL.*underlay.* is configured.*"
	underlayNotConfiguredTestSelector   = "EXTERNAL.*underlay.* is not configured.*"
	l3VNIConfiguredTestSelector         = "EXTERNAL.*l3.*vni.*configured"
	l3VNIDeletedTestSelector            = "EXTERNAL.*l3.*vni.*deleted"
	l2VNIConfiguredTestSelector         = "EXTERNAL.*l2.*vni.*configured"
	l2VNIDeletedTestSelector            = "EXTERNAL.*l2.*vni.*deleted"
	l3PassthroughConfiguredTestSelector = "EXTERNAL.*l3.*passthrough.*configured"
	l3PassthroughDeletedTestSelector    = "EXTERNAL.*l3.*passthrough.*deleted"
)

var _ = ginkgo.Describe("Router Host configuration", func() {
	underlay := v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "underlay",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN:        64514,
			Interfaces: []v1alpha1.UnderlayInterface{{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"}}},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64517)),
					Address: new("192.168.11.2"),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	}

	var cs clientset.Interface
	routerPods := []*corev1.Pod{}

	ginkgo.BeforeEach(func() {
		cs = k8sclient.New()

		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("waiting for all router pods to be ready")
		Eventually(func(g Gomega) {
			pods, err := openperouter.RouterPods(cs)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pods).NotTo(BeEmpty(), "no router pods found")
			for _, p := range pods {
				g.Expect(p.DeletionTimestamp).To(BeNil(), "pod %s is being deleted", p.Name)
				g.Expect(k8s.PodIsReady(p)).To(BeTrue(), "pod %s must be ready", p.Name)
			}
			routerPods = pods
		}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())

		ginkgo.By("waiting for underlay veth to be recreated on each node")
		for _, pod := range routerPods {
			nodeName := pod.Spec.NodeName
			Eventually(func() bool {
				return openperouter.UnderlayVethsExists(nodeName)
			}).WithTimeout(30*time.Second).WithPolling(2*time.Second).Should(BeTrue(),
				"toswitch not found on %s", nodeName)
		}

		for _, pod := range routerPods {
			ensureValidator(cs, pod)
		}

		cs = k8sclient.New()
	})

	ginkgo.AfterEach(func() {
		dumpIfFails(cs)
		Expect(Updater.CleanAll()).To(Succeed())
	})

	ginkgo.Context("L3", func() {
		l3vni100 := v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "first",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: "first",
				VNI: 100,
				HostSession: &v1alpha1.HostSession{
					ASN:     64514,
					HostASN: new(int64(64515)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: new("192.169.10.0/24"),
					},
				},
			},
		}
		l3vni200 := v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "second",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: "second",
				VNI: 200,
				HostSession: &v1alpha1.HostSession{
					ASN:     64514,
					HostASN: new(int64(64515)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: new("192.169.11.0/24"),
					},
				},
			},
		}
		l3vni300 := v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ipv6-only",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: "ipv6-only",
				VNI: 300,
				HostSession: &v1alpha1.HostSession{
					ASN:     64514,
					HostASN: new(int64(64515)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv6: new("2001:db8:1::/64"),
					},
				},
			},
		}
		l3vni400 := v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dual-stack",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: "dual-stack",
				VNI: 400,
				HostSession: &v1alpha1.HostSession{
					ASN:     64514,
					HostASN: new(int64(64515)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: new("192.169.12.0/24"),
						IPv6: new("2001:db8:2::/64"),
					},
				},
			},
		}

		ginkgo.It("is applied correctly", func() {
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					l3vni100,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)

				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       100,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("works with two l3 vnis and deletion", func() {
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					l3vni100,
					l3vni200,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       100,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				validateConfig(l3vniParams{
					VRF: l3vni200.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       200,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)
			}

			ginkgo.By("delete the first vni")
			err = Updater.Client().Delete(context.Background(), &l3vni100)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l3vniParams{
					VRF: l3vni200.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       200,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating VNI is deleted for pod %s", p.Name))
				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       100,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIDeletedTestSelector, p)
			}

			for _, p := range routerPods {
				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("works while editing the vni parameters", func() {
			resources := config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					*l3vni100.DeepCopy(),
				},
			}

			err := Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       100,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)
			}

			ginkgo.By("editing the first vni")

			resources.L3VNIs[0].Spec.HostSession.ASN = 64515
			resources.L3VNIs[0].Spec.VNI = 300
			resources.L3VNIs[0].Spec.HostSession.HostASN = new(int64(64516))
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				changedVni := resources.L3VNIs[0]
				validateConfig(l3vniParams{
					VRF: changedVni.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(changedVni.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(changedVni.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)
			}

			for _, p := range routerPods {
				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("works while editing the underlay parameters", func() {
			resources := config.Resources{
				Underlays: []v1alpha1.Underlay{
					*underlay.DeepCopy(),
				},
				L3VNIs: []v1alpha1.L3VNI{
					l3vni100,
				},
			}

			err := Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			validate := func(tunnelEndpoint *v1alpha1.TunnelEndpointConfig) {
				for _, p := range routerPods {
					ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

					vtepIP := vtepIPv4ForPod(cs, tunnelEndpoint, p)
					validateConfig(l3vniParams{
						VRF: l3vni100.Name,
						LinkIPs: &linkIPs{
							NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
							NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
						},
						VNI:       100,
						VXLanPort: 4789,
						VTEPIP:    vtepIP,
					}, l3VNIConfiguredTestSelector, p)

					ginkgo.By(fmt.Sprintf("validating underlay for pod %s", p.Name))
					validateConfig(underlayParams{
						UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
						TunnelEndpoint: &tunnelEndpointParams{
							IPv4: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				}
			}

			validate(underlay.Spec.TunnelEndpoint)

			ginkgo.By("editing the vtep cidr vni")

			newCIDRs := []string{"100.64.0.0/24"}
			resources.Underlays[0].Spec.TunnelEndpoint.CIDRs = newCIDRs
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			validate(resources.Underlays[0].Spec.TunnelEndpoint)

			ginkgo.By("editing the underlay nic (to non existent one)")
			resources.Underlays[0].Spec.Interfaces[0].NetworkDevice.InterfaceName = "foo"
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("verifying nodes report degraded status")
			for _, p := range routerPods {
				Eventually(func(g Gomega) {
					expectNodeCondition(g, p.Spec.NodeName, v1alpha1.ConditionTypeReady, metav1.ConditionFalse)
					expectNodeCondition(g, p.Spec.NodeName, v1alpha1.ConditionTypeDegraded, metav1.ConditionTrue)
				}, time.Minute, time.Second).Should(Succeed())
			}

			ginkgo.By("verifying router pods were NOT restarted")
			currentPods, err := openperouter.RouterPods(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(podNodeToUIDs(currentPods)).To(Equal(podNodeToUIDs(routerPods)))

			ginkgo.By("restoring the underlay nic to a valid one")
			resources.Underlays[0].Spec.Interfaces[0].NetworkDevice.InterfaceName = "toswitch1"
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("verifying nodes recover from degraded status")
			for _, p := range routerPods {
				Eventually(func(g Gomega) {
					expectNodeCondition(g, p.Spec.NodeName, v1alpha1.ConditionTypeReady, metav1.ConditionTrue)
					expectNodeCondition(g, p.Spec.NodeName, v1alpha1.ConditionTypeDegraded, metav1.ConditionFalse)
				}, 2*time.Minute, time.Second).Should(Succeed())
			}
		})

		ginkgo.It("works with IPv6-only L3VNI", func() {
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					l3vni300,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating IPv6-only VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l3vniParams{
					VRF: l3vni300.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       uint32(l3vni300.Spec.VNI),
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("works with dual-stack L3VNI", func() {
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					l3vni400,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating dual-stack VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l3vniParams{
					VRF: l3vni400.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       uint32(l3vni400.Spec.VNI),
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("works with mixed IPv4, IPv6, and dual-stack L3VNIs", func() {
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					l3vni100,
					l3vni300,
					l3vni400,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating mixed VNIs for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)

				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       uint32(l3vni100.Spec.VNI),
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				validateConfig(l3vniParams{
					VRF: l3vni300.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       uint32(l3vni300.Spec.VNI),
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				validateConfig(l3vniParams{
					VRF: l3vni400.Name,
					LinkIPs: &linkIPs{
						NSIPv4: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       uint32(l3vni400.Spec.VNI),
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)
			}
		})
		ginkgo.It("works with node selectors", func() {
			nodeSelectorVNI100 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/l3vni100": "true",
				},
			}
			nodeSelectorVNI200 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/l3vni200": "true",
				},
			}
			nodeSelectorVNI100AndVNI200 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/l3vni100": "true",
					"openperouter/l3vni200": "true",
				},
			}

			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

			ginkgo.By("Label two nodes so L3 VNIs are configured at each of one")
			Expect(
				k8s.LabelNodes(cs, nodeSelectorVNI100.MatchLabels, nodes[0]),
			).To(Succeed())
			Expect(
				k8s.LabelNodes(cs, nodeSelectorVNI200.MatchLabels, nodes[1]),
			).To(Succeed())

			ginkgo.DeferCleanup(func() {
				k8s.UnlabelNodes(cs, nodes[0], nodes[1])
			})

			l3vni100WithNodeSelector := l3vni100.DeepCopy()
			l3vni100WithNodeSelector.Spec.NodeSelector = nodeSelectorVNI100
			l3vni200WithNodeSelector := l3vni200.DeepCopy()
			l3vni200WithNodeSelector.Spec.NodeSelector = nodeSelectorVNI200

			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					*l3vni100WithNodeSelector,
					*l3vni200WithNodeSelector,
				},
			})).To(Succeed())

			l3VNI100Params := l3vniParams{
				VRF: l3vni100.Name,
				LinkIPs: &linkIPs{
					NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
					NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
				},
				VNI:       100,
				VXLanPort: 4789,
			}
			l3VNI200Params := l3vniParams{
				VRF: l3vni200.Name,
				LinkIPs: &linkIPs{
					NSIPv4: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv4),
					NSIPv6: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv6),
				},
				VNI:       200,
				VXLanPort: 4789,
			}

			validate := func(p *corev1.Pod, params l3vniParams, test string) {
				ginkgo.GinkgoHelper()
				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				checkMsg := "configured"
				if test == l3VNIDeletedTestSelector {
					checkMsg = "not configured"
				}
				ginkgo.By(fmt.Sprintf("validating L3VNI %d %s for pod %q", params.VNI, checkMsg, p.Name))
				params.VTEPIP = vtepIP
				validateConfig(params, test, p)
			}

			ginkgo.By("Check that each node has different L3 VNI configured")
			for _, p := range routerPods {
				switch p.Spec.NodeName {
				case nodes[0].Name:
					validate(p, l3VNI100Params, l3VNIConfiguredTestSelector)
					validate(p, l3VNI200Params, l3VNIDeletedTestSelector)
				case nodes[1].Name:
					validate(p, l3VNI100Params, l3VNIDeletedTestSelector)
					validate(p, l3VNI200Params, l3VNIConfiguredTestSelector)
				default:
					validate(p, l3VNI100Params, l3VNIDeletedTestSelector)
					validate(p, l3VNI200Params, l3VNIDeletedTestSelector)
				}

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}

			ginkgo.By("Change node labels to configure both L3 VNIs at the same node")
			k8s.UnlabelNodes(cs, nodes[1])
			Expect(
				k8s.LabelNodes(cs, nodeSelectorVNI100AndVNI200.MatchLabels, nodes[0]),
			).To(Succeed())

			ginkgo.By("Check that just one node is configured with both L3 VNIs")
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[0].Name {
					validate(p, l3VNI100Params, l3VNIConfiguredTestSelector)
					validate(p, l3VNI200Params, l3VNIConfiguredTestSelector)
				} else {
					validate(p, l3VNI100Params, l3VNIDeletedTestSelector)
					validate(p, l3VNI200Params, l3VNIDeletedTestSelector)
				}

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}

			ginkgo.By("Reconfigure l3vni200 without node selector")
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3VNIs: []v1alpha1.L3VNI{
					l3vni200,
				},
			})).To(Succeed())

			ginkgo.By("Check L3 VNI 200 node selector is at all the nodes now")
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[0].Name {
					validate(p, l3VNI100Params, l3VNIConfiguredTestSelector)
					validate(p, l3VNI200Params, l3VNIConfiguredTestSelector)
				} else {
					validate(p, l3VNI100Params, l3VNIDeletedTestSelector)
					validate(p, l3VNI200Params, l3VNIConfiguredTestSelector)
				}

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})
	})

	ginkgo.Context("L2", func() {
		l2vni300 := v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "first",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L2VNISpec{
				VNI: 300,
			},
		}
		l2vni400 := v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "second",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L2VNISpec{
				VNI: 400,
			},
		}

		ginkgo.It("is applied correctly", func() {
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2vni300,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l2vniParams{
					VRF:       "",
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("works with two l2 vnis and deletion", func() {
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2vni300,
					l2vni400,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l2vniParams{
					VRF:       "",
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)

				validateConfig(l2vniParams{
					VRF:       "",
					VNI:       400,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)
			}

			ginkgo.By("delete the first vni")
			err = Updater.Client().Delete(context.Background(), &l2vni300)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l2vniParams{
					VRF:       "",
					VNI:       400,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating VNI is deleted for pod %s", p.Name))
				validateConfig(l2vniParams{
					VRF:       "",
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIDeletedTestSelector, p)

			}

			for _, p := range routerPods {
				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("works while editing the vni parameters", func() {
			resources := config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L2VNIs: []v1alpha1.L2VNI{
					*l2vni300.DeepCopy(),
				},
			}

			err := Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(l2vniParams{
					VRF:       "",
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)
			}

			ginkgo.By("editing the first vni")

			resources.L2VNIs[0].Spec.VNI = 600
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)

				validateConfig(l2vniParams{
					VRF:       "",
					VNI:       600,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)
			}

			for _, p := range routerPods {
				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})
		ginkgo.It("works with node selectors", func() {
			nodeSelectorVNI300 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/l2vni300": "true",
				},
			}
			nodeSelectorVNI400 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/l2vni400": "true",
				},
			}
			nodeSelectorVNI300AndVNI400 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/l2vni300": "true",
					"openperouter/l2vni400": "true",
				},
			}
			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

			ginkgo.By("Label two nodes so L2 VNIs are configured at each of one")
			Expect(
				k8s.LabelNodes(cs, nodeSelectorVNI300.MatchLabels, nodes[0]),
			).To(Succeed())
			Expect(
				k8s.LabelNodes(cs, nodeSelectorVNI400.MatchLabels, nodes[1]),
			).To(Succeed())

			ginkgo.DeferCleanup(func() {
				k8s.UnlabelNodes(cs, nodes[0], nodes[1])
			})
			l2vni300WithNodeSelector := l2vni300.DeepCopy()
			l2vni300WithNodeSelector.Spec.NodeSelector = nodeSelectorVNI300
			l2vni400WithNodeSelector := l2vni400.DeepCopy()
			l2vni400WithNodeSelector.Spec.NodeSelector = nodeSelectorVNI400

			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L2VNIs: []v1alpha1.L2VNI{
					*l2vni300WithNodeSelector,
					*l2vni400WithNodeSelector,
				},
			})).To(Succeed())
			l2VNI300Params := l2vniParams{
				VRF:       "",
				VNI:       300,
				VXLanPort: 4789,
			}
			l2VNI400Params := l2vniParams{
				VRF:       "",
				VNI:       400,
				VXLanPort: 4789,
			}

			validate := func(p *corev1.Pod, params l2vniParams, test string) {
				ginkgo.GinkgoHelper()
				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				checkMsg := "configured"
				if test == l2VNIDeletedTestSelector {
					checkMsg = "not configured"
				}
				ginkgo.By(fmt.Sprintf("validating L2VNI %d %s for pod %q", params.VNI, checkMsg, p.Name))
				params.VTEPIP = vtepIP
				validateConfig(params, test, p)
			}

			ginkgo.By("Check that each node has different L2 VNI configured")
			for _, p := range routerPods {
				switch p.Spec.NodeName {
				case nodes[0].Name:
					validate(p, l2VNI300Params, l2VNIConfiguredTestSelector)
					validate(p, l2VNI400Params, l2VNIDeletedTestSelector)
				case nodes[1].Name:
					validate(p, l2VNI300Params, l2VNIDeletedTestSelector)
					validate(p, l2VNI400Params, l2VNIConfiguredTestSelector)
				default:
					validate(p, l2VNI300Params, l2VNIDeletedTestSelector)
					validate(p, l2VNI400Params, l2VNIDeletedTestSelector)
				}

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}

			ginkgo.By("Change node labels to configure both L2 VNIs at the same node")
			Expect(
				k8s.UnlabelNodes(cs, nodes[1]),
			).To(Succeed())
			Expect(
				k8s.LabelNodes(cs, nodeSelectorVNI300AndVNI400.MatchLabels, nodes[0]),
			).To(Succeed())

			ginkgo.By("Check that just one node is configured with both L2 VNIs")
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[0].Name {
					validate(p, l2VNI300Params, l2VNIConfiguredTestSelector)
					validate(p, l2VNI400Params, l2VNIConfiguredTestSelector)
				} else {
					validate(p, l2VNI300Params, l2VNIDeletedTestSelector)
					validate(p, l2VNI400Params, l2VNIDeletedTestSelector)
				}

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}

			ginkgo.By("Remove L2 VNI 400 node selector")
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2vni400,
				},
			})).To(Succeed())

			ginkgo.By("Check L2 VNI 400 node selector is at all the nodes now")
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[0].Name {
					validate(p, l2VNI300Params, l2VNIConfiguredTestSelector)
					validate(p, l2VNI400Params, l2VNIConfiguredTestSelector)
				} else {
					validate(p, l2VNI300Params, l2VNIDeletedTestSelector)
					validate(p, l2VNI400Params, l2VNIConfiguredTestSelector)
				}

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				vtepIP := vtepIPv4ForPod(cs, underlay.Spec.TunnelEndpoint, p)
				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})
	})

	ginkgo.Context("Underlay", func() {
		ginkgo.It("works with node selectors", func() {
			nodeSelectorUnderlay1 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/underlay1": "true",
				},
			}
			nodeSelectorUnderlay2 := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/underlay2": "true",
				},
			}

			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

			ginkgo.By("Label two nodes so underlays are configured at each one")
			Expect(
				k8s.LabelNodes(cs, nodeSelectorUnderlay1.MatchLabels, nodes[0]),
			).To(Succeed())
			Expect(
				k8s.LabelNodes(cs, nodeSelectorUnderlay2.MatchLabels, nodes[1]),
			).To(Succeed())

			ginkgo.DeferCleanup(func() {
				k8s.UnlabelNodes(cs, nodes[0], nodes[1])
			})

			underlay1WithNodeSelector := underlay.DeepCopy()
			underlay1WithNodeSelector.Name = "underlay1"
			underlay1WithNodeSelector.Spec.NodeSelector = nodeSelectorUnderlay1
			underlay1WithNodeSelector.Spec.TunnelEndpoint.CIDRs = []string{"100.65.0.0/24"}

			underlay2WithNodeSelector := underlay.DeepCopy()
			underlay2WithNodeSelector.Name = "underlay2"
			underlay2WithNodeSelector.Spec.NodeSelector = nodeSelectorUnderlay2
			underlay2WithNodeSelector.Spec.TunnelEndpoint.CIDRs = []string{"100.66.0.0/24"}

			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					*underlay1WithNodeSelector,
					*underlay2WithNodeSelector,
				},
			})).To(Succeed())

			ginkgo.By(fmt.Sprintf("Check that each node %q has underlay 1 and that node %q has underlay 2", nodes[0].Name, nodes[1].Name))
			for _, p := range routerPods {
				switch p.Spec.NodeName {
				case nodes[0].Name:
					ginkgo.By(fmt.Sprintf("validating underlay1 configured on node %s", p.Spec.NodeName))
					vtepIP := vtepIPv4ForPod(cs, underlay1WithNodeSelector.Spec.TunnelEndpoint, p)
					validateConfig(underlayParams{
						UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
						TunnelEndpoint: &tunnelEndpointParams{
							IPv4: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				case nodes[1].Name:
					ginkgo.By(fmt.Sprintf("validating underlay2 configured on node %s", p.Spec.NodeName))
					vtepIP := vtepIPv4ForPod(cs, underlay2WithNodeSelector.Spec.TunnelEndpoint, p)
					validateConfig(underlayParams{
						UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
						TunnelEndpoint: &tunnelEndpointParams{
							IPv4: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				default:
					ginkgo.By(fmt.Sprintf("validating underlay is not configured for pod %q on node %s", p.Name, p.Spec.NodeName))
					validateConfig(underlayParams{
						UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					}, underlayNotConfiguredTestSelector, p)
				}
			}

			ginkgo.By(fmt.Sprintf("Unlabel the node %q", nodes[1].Name))
			Expect(
				k8s.UnlabelNodes(cs, nodes[1]),
			).To(Succeed())

			ginkgo.By("waiting for the underlay to be cleaned up on the unlabeled node")
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[1].Name {
					validateConfig(underlayParams{
						UnderlayInterfaces: map[string]underlayInterface{"toswitch": {Kind: "netdev"}},
					}, underlayNotConfiguredTestSelector, p)
				}
			}

			ginkgo.By(fmt.Sprintf("Check that only node %q has the underlay configured", nodes[0].Name))
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[0].Name {
					ginkgo.By(fmt.Sprintf("validating underlay1 configured on node %s", p.Spec.NodeName))
					vtepIP := vtepIPv4ForPod(cs, underlay1WithNodeSelector.Spec.TunnelEndpoint, p)
					validateConfig(underlayParams{
						UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
						TunnelEndpoint: &tunnelEndpointParams{
							IPv4: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				} else {
					ginkgo.By(fmt.Sprintf("validating underlay is not configured on node %s", p.Spec.NodeName))
					validateConfig(underlayParams{
						UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					}, underlayNotConfiguredTestSelector, p)
				}
			}

			ginkgo.By("Change underlay1 to apply to all nodes")
			underlay1WithNodeSelector.Spec.NodeSelector = nil
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					*underlay1WithNodeSelector,
				},
			})).To(Succeed())

			ginkgo.By("Check that all nodes now have underlay1 configured")
			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating underlay1 configured on node %q", p.Spec.NodeName))
				vtepIP := vtepIPv4ForPod(cs, underlay1WithNodeSelector.Spec.TunnelEndpoint, p)
				validateConfig(underlayParams{
					UnderlayInterfaces: map[string]underlayInterface{"toswitch1": {Kind: "netdev"}},
					TunnelEndpoint: &tunnelEndpointParams{
						IPv4: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})

		ginkgo.It("moves the underlay interface back to the default netns on deletion", func() {
			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())

			underlayNIC := "toswitch1"

			ginkgo.By(fmt.Sprintf("verifying %s exists in the default netns before creating the underlay", underlayNIC))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInDefaultNetns(node.Name, underlayNIC)
				}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(
					BeTrueBecause("%s should be in the default netns on %s before underlay creation", underlayNIC, node.Name))
			}

			ginkgo.By(fmt.Sprintf("recording the %s IP addresses before creating the underlay", underlayNIC))
			addrsBefore := map[string]string{}
			for _, node := range nodes {
				addrs, err := openperouter.InterfaceIPAddresses(node.Name, underlayNIC)
				Expect(err).NotTo(HaveOccurred(), "failed to get %s addresses on %s", underlayNIC, node.Name)
				addrsBefore[node.Name] = addrs
			}

			ginkgo.By("creating the underlay")
			err = Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By(fmt.Sprintf("verifying %s moved into the perouter netns", underlayNIC))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInNS(node.Name, underlayNIC, openperouter.NamedNetns)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeTrueBecause(
					"%s should be inside the perouter netns on %s after underlay creation", underlayNIC, node.Name))
			}

			ginkgo.By("deleting the underlay")
			Expect(Updater.CleanAll()).To(Succeed())

			ginkgo.By("waiting for all router pods to be ready after underlay deletion")
			Eventually(func(g Gomega) {
				pods, err := openperouter.RouterPods(cs)
				g.Expect(err).NotTo(HaveOccurred())
				for _, p := range pods {
					g.Expect(k8s.PodIsReady(p)).To(BeTrueBecause("pod %s must be ready", p.Name))
				}
			}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(Succeed())

			ginkgo.By(fmt.Sprintf("verifying %s is back in the default netns after underlay deletion", underlayNIC))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInDefaultNetns(node.Name, underlayNIC)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeTrueBecause(
					"%s should be back in the default netns on %s after underlay deletion", underlayNIC, node.Name))
			}

			ginkgo.By(fmt.Sprintf("verifying %s is UP and has the same IP addresses as before", underlayNIC))
			for _, node := range nodes {
				Expect(openperouter.InterfaceIsUp(node.Name, underlayNIC)).To(BeTrueBecause(
					"%s should be UP on %s after underlay deletion", underlayNIC, node.Name))

				addrsAfter, err := openperouter.InterfaceIPAddresses(node.Name, underlayNIC)
				Expect(err).NotTo(HaveOccurred(), "failed to get %s addresses on %s after deletion", underlayNIC, node.Name)
				Expect(addrsAfter).To(Equal(addrsBefore[node.Name]),
					"%s IP addresses on %s should be preserved after underlay deletion; was: %q, is: %q",
					underlayNIC, node.Name, addrsBefore[node.Name], addrsAfter)
			}
		})

		ginkgo.It("changes the underlay NIC without restarting the router pod", func() {
			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())

			oldNIC := "toswitch1"
			newNIC := "toswitch2"

			ginkgo.By(fmt.Sprintf("recording the %s IP addresses before the change", oldNIC))
			oldNICAddrsBefore := map[string]string{}
			for _, node := range nodes {
				addrs, err := openperouter.InterfaceIPAddresses(node.Name, oldNIC)
				Expect(err).NotTo(HaveOccurred(), "failed to get %s addresses on %s", oldNIC, node.Name)
				oldNICAddrsBefore[node.Name] = addrs
			}

			vniForSwap := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "swap-test",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "swap-test",
					VNI: 500,
				},
			}
			vniBridge := "br-pe-500"

			ginkgo.By("creating the underlay and an L3VNI with " + oldNIC)
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
				L3VNIs:    []v1alpha1.L3VNI{vniForSwap},
			})).To(Succeed())

			ginkgo.By(fmt.Sprintf("waiting for %s to move into the perouter netns", oldNIC))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInNS(node.Name, oldNIC, openperouter.NamedNetns)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeTrueBecause(
					"%s should be inside the perouter netns on %s", oldNIC, node.Name))
			}

			ginkgo.By(fmt.Sprintf("waiting for VNI bridge %s to appear in the perouter netns", vniBridge))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInNS(node.Name, vniBridge, openperouter.NamedNetns)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeTrueBecause(
					"%s should exist in the perouter netns on %s", vniBridge, node.Name))
			}

			ginkgo.By("updating the underlay to use " + newNIC)
			updatedUnderlay := v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN: 64514,
					Interfaces: []v1alpha1.UnderlayInterface{
						{
							Type:          "NetworkDevice",
							NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: newNIC},
						},
					},
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     new(int64(64513)),
							Address: new("192.168.12.2"),
						},
					},
					TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
						CIDRs: []string{"100.65.0.0/24"},
					},
				},
			}
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{updatedUnderlay},
				L3VNIs:    []v1alpha1.L3VNI{vniForSwap},
			})).To(Succeed())

			ginkgo.By(fmt.Sprintf("waiting for %s to move into the perouter netns", newNIC))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInNS(node.Name, newNIC, openperouter.NamedNetns)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeTrueBecause(
					"%s should be inside the perouter netns on %s after underlay update", newNIC, node.Name))
			}

			ginkgo.By(fmt.Sprintf("verifying %s is back in the default netns with original IPs", oldNIC))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInDefaultNetns(node.Name, oldNIC)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeTrueBecause(
					"%s should be back in the default netns on %s after underlay update", oldNIC, node.Name))

				Expect(openperouter.InterfaceIsUp(node.Name, oldNIC)).To(BeTrueBecause(
					"%s should be UP on %s after underlay update", oldNIC, node.Name))

				Expect(openperouter.InterfaceIPAddresses(node.Name, oldNIC)).To(
					Equal(oldNICAddrsBefore[node.Name]),
					"%s IP addresses on %s should be preserved after underlay update",
					oldNIC,
					node.Name,
				)
			}

			ginkgo.By(fmt.Sprintf("waiting for VNI bridge %s to be re-established after underlay swap", vniBridge))
			for _, node := range nodes {
				Eventually(func() bool {
					return openperouter.IsInterfaceInNS(node.Name, vniBridge, openperouter.NamedNetns)
				}).WithTimeout(2 * time.Minute).WithPolling(time.Second).Should(BeTrueBecause(
					"%s should be re-established in the perouter netns on %s after underlay swap", vniBridge, node.Name))
			}

			ginkgo.By("verifying router pods were NOT restarted")
			currentPods, err := openperouter.RouterPods(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(podNodeToUIDs(currentPods)).To(Equal(podNodeToUIDs(routerPods)))
		})
	})

	ginkgo.Context("L3Passthrough", func() {
		ginkgo.It("works with node selectors", func() {
			nodeSelectorPassthrough := &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"openperouter/passthrough": "true",
				},
			}
			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

			ginkgo.By("Label two nodes so L3Passthroughs are configured at each one")
			Expect(
				k8s.LabelNodes(cs, nodeSelectorPassthrough.MatchLabels, nodes[0]),
			).To(Succeed())

			ginkgo.DeferCleanup(func() {
				k8s.UnlabelNodes(cs, nodes[0], nodes[1])
			})

			passthroughWithNodeSelector := v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "passthrough1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					NodeSelector: nodeSelectorPassthrough,
					HostSession: v1alpha1.HostSession{
						ASN:     64514,
						HostASN: new(int64(64515)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: new("192.169.10.0/24"),
						},
					},
				},
			}

			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
				L3Passthrough: []v1alpha1.L3Passthrough{
					passthroughWithNodeSelector,
				},
			})).To(Succeed())

			l3PassthroughParams := l3passthroughParams{
				LinkIPs: &linkIPs{
					NSIPv4: routerIPWithNetmask(passthroughWithNodeSelector.Spec.HostSession.LocalCIDR.IPv4),
					NSIPv6: routerIPWithNetmask(passthroughWithNodeSelector.Spec.HostSession.LocalCIDR.IPv6),
				},
			}

			validate := func(p *corev1.Pod, params l3passthroughParams, test string) {
				ginkgo.GinkgoHelper()
				checkMsg := "configured"
				if test == l3PassthroughDeletedTestSelector {
					checkMsg = "not configured"
				}
				ginkgo.By(fmt.Sprintf("validating L3Passthrough %s %s for pod %q", params.TargetNS, checkMsg, p.Name))
				validateConfig(params, test, p)
			}

			ginkgo.By(fmt.Sprintf("Check that node %q has passthrough and the others do not", nodes[0].Name))
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[0].Name {
					validate(p, l3PassthroughParams, l3PassthroughConfiguredTestSelector)
				} else {
					validate(p, l3PassthroughParams, l3PassthroughDeletedTestSelector)
				}
			}

			ginkgo.By(fmt.Sprintf("Unlabel the node %q", nodes[0].Name))
			Expect(
				k8s.UnlabelNodes(cs, nodes[0]),
			).To(Succeed())

			ginkgo.By("Check that no node has the passthrough configured")
			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating passthrough is not configured on node %s", p.Spec.NodeName))
				validate(p, l3PassthroughParams, l3PassthroughDeletedTestSelector)
			}

			ginkgo.By("Change passthrough to apply to all nodes")
			passthroughWithNodeSelector.Spec.NodeSelector = nil
			Expect(Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{
					passthroughWithNodeSelector,
				},
			})).To(Succeed())
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("Check that all nodes now have passthrough1 configured")
			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating passthrough1 configured on node %q", p.Spec.NodeName))
				validate(p, l3PassthroughParams, l3PassthroughConfiguredTestSelector)
			}
		})
	})
})

type l3passthroughParams struct {
	TargetNS string   `json:"targetns"`
	LinkIPs  *linkIPs `json:"linkIPs"`
}

type l3vniParams struct {
	VRF       string   `json:"vrf"`
	VTEPIP    string   `json:"vtepip"`
	LinkIPs   *linkIPs `json:"linkIPs"`
	VNI       uint32   `json:"vni"`
	VXLanPort int      `json:"vxlanport"`
}

type linkIPs struct {
	HostIPv4 string `json:"hostipv4"`
	NSIPv4   string `json:"nsipv4"`
	HostIPv6 string `json:"hostipv6"`
	NSIPv6   string `json:"nsipv6"`
}

type l2vniParams struct {
	VRF          string   `json:"vrf"`
	VTEPIP       string   `json:"vtepip"`
	VNI          uint32   `json:"vni"`
	VXLanPort    int      `json:"vxlanport"`
	L2GatewayIPs []string `json:"l2gatewayips,omitempty"`
}

type underlayParams struct {
	UnderlayInterfaces map[string]underlayInterface `json:"underlay_interfaces"`
	TunnelEndpoint     *tunnelEndpointParams        `json:"tunnel_endpoint"`
}

// underlayInterface mirrors hostnetwork.UnderlayInterface.
type underlayInterface struct {
	Kind string `json:"kind"`
}

type tunnelEndpointParams struct {
	IPv4 string `json:"ipv4"`
}

func validateConfig[T any](config T, test string, pod *corev1.Pod) {
	fileToValidate := sendConfigToValidate(pod, config)
	Eventually(func() error {
		exec := openperouter.ExecutorForPod(pod)
		res, err := exec.Exec("/validatehost", "--ginkgo.focus", test, "--paramsfile", fileToValidate)
		if err != nil {
			return fmt.Errorf("failed to validate test %s : %s %w", test, res, err)
		}
		return nil
	}).
		WithTimeout(2 * time.Minute).
		WithPolling(time.Second).
		ShouldNot(HaveOccurred())
}

func ensureValidator(cs clientset.Interface, pod *corev1.Pod) {
	if pod.Annotations != nil && pod.Annotations["validator"] == "true" {
		return
	}
	dst := fmt.Sprintf("%s/%s:/", pod.Namespace, pod.Name)
	fullargs := []string{"cp", ValidatorPath, dst}
	_, err := exec.Command(executor.Kubectl, fullargs...).CombinedOutput()
	Expect(err).NotTo(HaveOccurred())

	pod.Annotations["validator"] = "true"
	_, err = cs.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func vtepIPv4ForPod(cs clientset.Interface, tunnelEndpoint *v1alpha1.TunnelEndpointConfig, pod *corev1.Pod) string {
	node, err := k8s.NodeObjectForPod(cs, pod)
	Expect(err).NotTo(HaveOccurred())
	vtepIP, err := openperouter.GetVtepIPv4ForNode(tunnelEndpoint, node)
	Expect(err).NotTo(HaveOccurred())
	return vtepIP
}

func routerIPWithNetmask(cidr *string) string {
	if cidr == nil || *cidr == "" {
		return ""
	}
	routerIP, err := openperouter.RouterIPFromCIDR(*cidr)
	Expect(err).NotTo(HaveOccurred())

	_, ipNet, err := net.ParseCIDR(*cidr)
	Expect(err).NotTo(HaveOccurred())

	ones, _ := ipNet.Mask.Size()

	return fmt.Sprintf("%s/%d", routerIP, ones)
}

func sendConfigToValidate[T any](pod *corev1.Pod, toValidate T) string {
	jsonData, err := json.MarshalIndent(toValidate, "", "  ")
	if err != nil {
		panic(err)
	}

	toValidateFile, err := os.CreateTemp(os.TempDir(), "validate-*.json")
	Expect(err).NotTo(HaveOccurred())

	_, err = toValidateFile.Write(jsonData)
	Expect(err).NotTo(HaveOccurred())

	err = k8s.SendFileToPod(toValidateFile.Name(), pod)
	Expect(err).NotTo(HaveOccurred())
	return filepath.Base(toValidateFile.Name())
}

func podNodeToUIDs(pods []*corev1.Pod) map[string]types.UID {
	res := make(map[string]types.UID, len(pods))
	for _, p := range pods {
		res[p.Spec.NodeName] = p.UID
	}
	return res
}

func ValidateCNIBinaries(g Gomega, cs clientset.Interface) {
	cniTargets := k8sModeCNITarget
	if HostMode {
		cniTargets = hostModeCNITarget
	}
	for _, t := range cniTargets(g, cs) {
		for _, bin := range cniBinaries {
			path := cniBinDir + "/" + bin
			out, err := t.exec.Exec("test", "-x", path)
			g.Expect(err).NotTo(HaveOccurred(), "CNI binary %s missing or not executable on %s: %s", path, t.label, out)
		}

		out, err := t.exec.Exec("test", "-d", cniCacheDir)
		g.Expect(err).NotTo(HaveOccurred(), "CNI cache directory %s missing on %s: %s", cniCacheDir, t.label, out)
		out, err = t.exec.Exec("test", "-w", cniCacheDir)
		g.Expect(err).NotTo(HaveOccurred(), "CNI cache directory %s not writable on %s: %s", cniCacheDir, t.label, out)
	}
}

func k8sModeCNITarget(g Gomega, cs clientset.Interface) []cniTarget {
	pods, err := openperouter.ControllerPods(cs)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pods).NotTo(BeEmpty(), "no controller pods found")
	targets := make([]cniTarget, 0, len(pods))
	for _, pod := range pods {
		targets = append(targets, cniTarget{
			exec:  executor.ForPod(pod.Namespace, pod.Name, "controller"),
			label: pod.Name,
		})
	}
	return targets
}

func hostModeCNITarget(g Gomega, cs clientset.Interface) []cniTarget {
	nodes, err := k8s.GetNodes(cs)
	g.Expect(err).NotTo(HaveOccurred())
	targets := make([]cniTarget, 0, len(nodes))
	for _, node := range nodes {
		targets = append(targets, cniTarget{
			exec:  executor.ForPodmanInContainer(node.Name, "controller"),
			label: node.Name,
		})
	}
	return targets
}
