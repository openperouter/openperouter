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
	"slices"
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
	clientset "k8s.io/client-go/kubernetes"
)

var ValidatorPath string

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
			ASN:  64514,
			Nics: []string{"toswitch"},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     64517,
					Address: "192.168.11.2",
				},
			},
			EVPN: &v1alpha1.EVPNConfig{
				VTEPCIDR: "100.65.0.0/24",
			},
		},
	}

	var cs clientset.Interface
	routerPods := []*corev1.Pod{}

	ginkgo.BeforeEach(func() {
		cs = k8sclient.New()
		ginkgo.By("ensuring the validator is in all the pods")
		var err error
		routerPods, err = openperouter.RouterPods(cs)
		Expect(err).NotTo(HaveOccurred())
		for _, pod := range routerPods {
			ensureValidator(cs, pod)
		}

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
	})

	ginkgo.AfterEach(func() {
		dumpIfFails(cs)
		Expect(Updater.CleanAll()).To(Succeed())
		ginkgo.By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			newRouterPods, err := openperouter.RouterPods(cs)
			if err != nil {
				return err
			}
			return podsRolled(cs, routerPods, newRouterPods)
		}, time.Minute, time.Second).ShouldNot(HaveOccurred())
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
					HostASN: 64515,
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: "192.169.10.0/24",
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
					HostASN: 64515,
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: "192.169.11.0/24",
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
					HostASN: 64515,
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv6: "2001:db8:1::/64",
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
					HostASN: 64515,
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: "192.169.12.0/24",
						IPv6: "2001:db8:2::/64",
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)

				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       100,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       100,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				validateConfig(l3vniParams{
					VRF: l3vni200.Name,
					HostVeth: &veth{
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l3vniParams{
					VRF: l3vni200.Name,
					HostVeth: &veth{
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
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       100,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIDeletedTestSelector, p)
			}

			for _, p := range routerPods {
				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					HostVeth: &veth{
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
			resources.L3VNIs[0].Spec.HostSession.HostASN = 64516
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				changedVni := resources.L3VNIs[0]
				validateConfig(l3vniParams{
					VRF: changedVni.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(changedVni.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(changedVni.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)
			}

			for _, p := range routerPods {
				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

			validate := func(vtepCidr string) {
				for _, p := range routerPods {
					ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

					vtepIP := vtepIPForPod(cs, vtepCidr, p)
					validateConfig(l3vniParams{
						VRF: l3vni100.Name,
						HostVeth: &veth{
							NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
							NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
						},
						VNI:       100,
						VXLanPort: 4789,
						VTEPIP:    vtepIP,
					}, l3VNIConfiguredTestSelector, p)

					ginkgo.By(fmt.Sprintf("validating underlay for pod %s", p.Name))
					validateConfig(underlayParams{
						UnderlayInterface: "toswitch",
						EVPN: &evpnParams{
							VtepIP: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				}
			}

			validate(underlay.Spec.EVPN.VTEPCIDR)

			ginkgo.By("editing the vtep cidr vni")

			newCidr := "100.64.0.0/24"
			resources.Underlays[0].Spec.EVPN.VTEPCIDR = newCidr
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			validate(newCidr)

			ginkgo.By("editing the underlay nic (to non existent one)")
			resources.Underlays[0].Spec.Nics[0] = "foo"
			err = Updater.Update(resources)
			Expect(err).NotTo(HaveOccurred())

			ginkgo.By("waiting for the routers to be rolled out again")
			Eventually(func() error {
				newRouterPods, err := openperouter.RouterPods(cs)
				if err != nil {
					return err
				}
				return podsRolled(cs, routerPods, newRouterPods)
			}, time.Minute, time.Second).ShouldNot(HaveOccurred())
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l3vniParams{
					VRF: l3vni300.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       l3vni300.Spec.VNI,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l3vniParams{
					VRF: l3vni400.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       l3vni400.Spec.VNI,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)

				validateConfig(l3vniParams{
					VRF: l3vni100.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       l3vni100.Spec.VNI,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				validateConfig(l3vniParams{
					VRF: l3vni300.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni300.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       l3vni300.Spec.VNI,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l3VNIConfiguredTestSelector, p)

				validateConfig(l3vniParams{
					VRF: l3vni400.Name,
					HostVeth: &veth{
						NSIPv4: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv4),
						NSIPv6: routerIPWithNetmask(l3vni400.Spec.HostSession.LocalCIDR.IPv6),
					},
					VNI:       l3vni400.Spec.VNI,
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
				HostVeth: &veth{
					NSIPv4: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv4),
					NSIPv6: routerIPWithNetmask(l3vni100.Spec.HostSession.LocalCIDR.IPv6),
				},
				VNI:       100,
				VXLanPort: 4789,
			}
			l3VNI200Params := l3vniParams{
				VRF: l3vni200.Name,
				HostVeth: &veth{
					NSIPv4: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv4),
					NSIPv6: routerIPWithNetmask(l3vni200.Spec.HostSession.LocalCIDR.IPv6),
				},
				VNI:       200,
				VXLanPort: 4789,
			}

			validate := func(p *corev1.Pod, params l3vniParams, test string) {
				ginkgo.GinkgoHelper()
				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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
				VNI:          400,
				L2GatewayIPs: []string{"192.168.1.4/24"},
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l2vniParams{
					VRF:       l2vni300.Name,
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l2vniParams{
					VRF:       l2vni300.Name,
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)

				validateConfig(l2vniParams{
					VRF:          l2vni400.Name,
					VNI:          400,
					VXLanPort:    4789,
					VTEPIP:       vtepIP,
					L2GatewayIPs: l2vni400.Spec.L2GatewayIPs,
				}, l2VNIConfiguredTestSelector, p)
			}

			ginkgo.By("delete the first vni")
			err = Updater.Client().Delete(context.Background(), &l2vni300)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l2vniParams{
					VRF:          l2vni400.Name,
					VNI:          400,
					VXLanPort:    4789,
					VTEPIP:       vtepIP,
					L2GatewayIPs: l2vni400.Spec.L2GatewayIPs,
				}, l2VNIConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating VNI is deleted for pod %s", p.Name))
				validateConfig(l2vniParams{
					VRF:       l2vni300.Name,
					VNI:       300,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIDeletedTestSelector, p)

			}

			for _, p := range routerPods {
				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l2vniParams{
					VRF:       l2vni300.Name,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)

				validateConfig(l2vniParams{
					VRF:       l2vni300.Name,
					VNI:       600,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, l2VNIConfiguredTestSelector, p)
			}

			for _, p := range routerPods {
				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)

				ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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
				VRF:       l2vni300.Name,
				VNI:       300,
				VXLanPort: 4789,
			}
			l2VNI400Params := l2vniParams{
				VRF:          l2vni400.Name,
				VNI:          400,
				VXLanPort:    4789,
				L2GatewayIPs: l2vni400.Spec.L2GatewayIPs,
			}

			validate := func(p *corev1.Pod, params l2vniParams, test string) {
				ginkgo.GinkgoHelper()
				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
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
			underlay1WithNodeSelector.Spec.EVPN.VTEPCIDR = "100.65.0.0/24"

			underlay2WithNodeSelector := underlay.DeepCopy()
			underlay2WithNodeSelector.Name = "underlay2"
			underlay2WithNodeSelector.Spec.NodeSelector = nodeSelectorUnderlay2
			underlay2WithNodeSelector.Spec.EVPN.VTEPCIDR = "100.66.0.0/24"

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
					vtepIP := vtepIPForPod(cs, underlay1WithNodeSelector.Spec.EVPN.VTEPCIDR, p)
					validateConfig(underlayParams{
						UnderlayInterface: "toswitch",
						EVPN: &evpnParams{
							VtepIP: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				case nodes[1].Name:
					ginkgo.By(fmt.Sprintf("validating underlay2 configured on node %s", p.Spec.NodeName))
					vtepIP := vtepIPForPod(cs, underlay2WithNodeSelector.Spec.EVPN.VTEPCIDR, p)
					validateConfig(underlayParams{
						UnderlayInterface: "toswitch",
						EVPN: &evpnParams{
							VtepIP: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				default:
					ginkgo.By(fmt.Sprintf("validating underlay is not configured for pod %q on node %s", p.Name, p.Spec.NodeName))
					validateConfig(underlayParams{
						UnderlayInterface: "toswitch",
					}, underlayNotConfiguredTestSelector, p)
				}
			}

			ginkgo.By(fmt.Sprintf("Unlabel the node %q", nodes[1].Name))
			routerPodsToRollout := []*corev1.Pod{}
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[1].Name {
					routerPodsToRollout = append(routerPodsToRollout, p)
				}
			}

			Expect(
				k8s.UnlabelNodes(cs, nodes[1]),
			).To(Succeed())

			ginkgo.By("waiting for the routers with deleted underlay to rollout")
			Eventually(func() error {
				newRouterPods, err := openperouter.RouterPods(cs)
				if err != nil {
					return err
				}
				newRouterPodsToRollout := []*corev1.Pod{}
				for _, p := range newRouterPods {
					if p.Spec.NodeName == nodes[1].Name {
						newRouterPodsToRollout = append(newRouterPodsToRollout, p)
					}
				}
				return podsRolled(cs, routerPodsToRollout, newRouterPodsToRollout)
			}).
				WithTimeout(time.Minute).
				WithPolling(time.Second).
				ShouldNot(HaveOccurred())
			routerPods, err = routerPodsWithValidator(cs)
			Expect(err).ToNot(HaveOccurred())

			ginkgo.By(fmt.Sprintf("Check that only node %q has the underlay configured", nodes[0].Name))
			for _, p := range routerPods {
				if p.Spec.NodeName == nodes[0].Name {
					ginkgo.By(fmt.Sprintf("validating underlay1 configured on node %s", p.Spec.NodeName))
					vtepIP := vtepIPForPod(cs, underlay1WithNodeSelector.Spec.EVPN.VTEPCIDR, p)
					validateConfig(underlayParams{
						UnderlayInterface: "toswitch",
						EVPN: &evpnParams{
							VtepIP: vtepIP,
						},
					}, underlayConfiguredTestSelector, p)
				} else {
					ginkgo.By(fmt.Sprintf("validating underlay is not configured on node %s", p.Spec.NodeName))
					validateConfig(underlayParams{
						UnderlayInterface: "toswitch",
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
				vtepIP := vtepIPForPod(cs, underlay1WithNodeSelector.Spec.EVPN.VTEPCIDR, p)
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepIP: vtepIP,
					},
				}, underlayConfiguredTestSelector, p)
			}
		})
	})

	ginkgo.Context("Underlay with vtepInterface", func() {
		underlayWithVTEPInterface := v1alpha1.Underlay{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "underlay",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.UnderlaySpec{
				ASN:  64514,
				Nics: []string{"toswitch"},
				Neighbors: []v1alpha1.Neighbor{
					{
						ASN:     64517,
						Address: "192.168.11.2",
					},
				},
				EVPN: &v1alpha1.EVPNConfig{
					VTEPInterface: "toswitch",
				},
			},
		}

		l2vni := v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vtepif-l2",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L2VNISpec{
				VNI: 500,
			},
		}

		ginkgo.It("is applied correctly using the moved nic as vtep source", func() {
			Expect(Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlayWithVTEPInterface,
				},
				L2VNIs: []v1alpha1.L2VNI{
					l2vni,
				},
			})).To(Succeed())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating Underlay with vtepInterface for pod %s", p.Name))

				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					EVPN: &evpnParams{
						VtepInterface: "toswitch",
					},
				}, underlayConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating L2 VNI uses vtepInterface for pod %s", p.Name))

				validateConfig(l2vniParams{
					VRF:           l2vni.Name,
					VNI:           l2vni.Spec.VNI,
					VXLanPort:     4789,
					VTEPInterface: "toswitch",
				}, l2VNIConfiguredTestSelector, p)
			}
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
						HostASN: 64515,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "192.169.10.0/24",
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
				HostVeth: &veth{
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
	TargetNS string `json:"targetns"`
	HostVeth *veth  `json:"veth"`
}

type l3vniParams struct {
	VRF       string `json:"vrf"`
	VTEPIP    string `json:"vtepip"`
	HostVeth  *veth  `json:"veth"`
	VNI       uint32 `json:"vni"`
	VXLanPort int    `json:"vxlanport"`
}

type veth struct {
	HostIPv4 string `json:"hostipv4"`
	NSIPv4   string `json:"nsipv4"`
	HostIPv6 string `json:"hostipv6"`
	NSIPv6   string `json:"nsipv6"`
}

type l2vniParams struct {
	VRF           string   `json:"vrf"`
	VTEPIP        string   `json:"vtepip"`
	VTEPInterface string   `json:"vtepiface"`
	VNI           uint32   `json:"vni"`
	VXLanPort     int      `json:"vxlanport"`
	L2GatewayIPs  []string `json:"l2gatewayips,omitempty"`
}

type underlayParams struct {
	UnderlayInterface string      `json:"underlay_interface"`
	EVPN              *evpnParams `json:"evpn"`
}

type evpnParams struct {
	VtepIP        string `json:"vtep_ip"`
	VtepInterface string `json:"vtep_interface"`
}

func validateConfig[T any](config T, test string, pod *corev1.Pod) {
	fileToValidate := sendConfigToValidate(pod, config)
	Eventually(func() error {
		exec := executor.ForPod(pod.Namespace, pod.Name, "frr")
		res, err := exec.Exec("/validatehost", "--ginkgo.focus", test, "--paramsfile", fileToValidate)
		if err != nil {
			return fmt.Errorf("failed to validate test %s : %s %w", test, res, err)
		}
		return nil
	}).
		WithTimeout(6 * time.Second).
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

func vtepIPForPod(cs clientset.Interface, vtepCIDR string, pod *corev1.Pod) string {
	node, err := k8s.NodeObjectForPod(cs, pod)
	Expect(err).NotTo(HaveOccurred())
	vtepIP, err := openperouter.VtepIPForNode(vtepCIDR, node)
	Expect(err).NotTo(HaveOccurred())
	return vtepIP
}

func routerIPWithNetmask(cidr string) string {
	if cidr == "" {
		return ""
	}
	routerIP, err := openperouter.RouterIPFromCIDR(cidr)
	Expect(err).NotTo(HaveOccurred())

	_, ipNet, err := net.ParseCIDR(cidr)
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

func podsRolled(cs clientset.Interface, oldPods, newPods []*corev1.Pod) error {
	oldPodsNames := []string{}
	for _, p := range oldPods {
		oldPodsNames = append(oldPodsNames, p.Name)
	}

	if len(newPods) != len(oldPodsNames) {
		return fmt.Errorf("new pods len %d different from old pods len: %d", len(newPods), len(oldPodsNames))
	}

	for _, p := range newPods {
		if slices.Contains(oldPodsNames, p.Name) {
			return fmt.Errorf("old pod %s not deleted yet", p.Name)
		}
		if !k8s.PodIsReady(p) {
			return fmt.Errorf("pod %s is not ready", p.Name)
		}
	}
	return nil
}

func routerPodsWithValidator(cs clientset.Interface) ([]*corev1.Pod, error) {
	routerPods, err := openperouter.RouterPods(cs)
	if err != nil {
		return nil, err
	}
	for _, pod := range routerPods {
		ensureValidator(cs, pod)
	}
	return routerPods, nil
}
