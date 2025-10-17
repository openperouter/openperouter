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
	clientset "k8s.io/client-go/kubernetes"
)

var (
	ValidatorPath string
)

const (
	underlayTestSelector        = "EXTERNAL.*underlay"
	l3VNIConfiguredTestSelector = "EXTERNAL.*l3.*vni.*configured"
	l3VNIDeletedTestSelector    = "EXTERNAL.*l3.*vni.*deleted"
	l2VNIConfiguredTestSelector = "EXTERNAL.*l2.*vni.*configured"
	l2VNIDeletedTestSelector    = "EXTERNAL.*l2.*vni.*deleted"
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
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		ginkgo.By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			return openperouter.DaemonsetRolled(cs, routerPods)
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
			err := Updater.CleanAll()
			Expect(err).NotTo(HaveOccurred())
			ginkgo.By("waiting for the router pod to rollout after removing the underlay")
			Eventually(func() error {
				return openperouter.DaemonsetRolled(cs, routerPods)
			}, time.Minute, time.Second).ShouldNot(HaveOccurred())
		})

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
				}, underlayTestSelector, p)
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
				}, underlayTestSelector, p)
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
				}, underlayTestSelector, p)
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
					}, underlayTestSelector, p)
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
				return openperouter.DaemonsetRolled(cs, routerPods)
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
				}, underlayTestSelector, p)
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
				}, underlayTestSelector, p)
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
				VNI:         400,
				L2GatewayIP: "192.168.1.4/24",
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
				}, underlayTestSelector, p)
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
					VRF:         l2vni400.Name,
					VNI:         400,
					VXLanPort:   4789,
					VTEPIP:      vtepIP,
					L2GatewayIP: l2vni400.Spec.L2GatewayIP,
				}, l2VNIConfiguredTestSelector, p)
			}

			ginkgo.By("delete the first vni")
			err = Updater.Client().Delete(context.Background(), &l2vni300)
			Expect(err).NotTo(HaveOccurred())

			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPForPod(cs, underlay.Spec.EVPN.VTEPCIDR, p)
				validateConfig(l2vniParams{
					VRF:         l2vni400.Name,
					VNI:         400,
					VXLanPort:   4789,
					VTEPIP:      vtepIP,
					L2GatewayIP: l2vni400.Spec.L2GatewayIP,
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
				}, underlayTestSelector, p)
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
				}, underlayTestSelector, p)
			}
		})
	})
})

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
	VRF         string `json:"vrf"`
	VTEPIP      string `json:"vtepip"`
	VNI         uint32 `json:"vni"`
	VXLanPort   int    `json:"vxlanport"`
	L2GatewayIP string `json:"l2gatewayip,omitempty"`
}

type underlayParams struct {
	UnderlayInterface string      `json:"underlay_interface"`
	EVPN              *evpnParams `json:"evpn"`
}

type evpnParams struct {
	VtepIP string `json:"vtep_ip"`
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
	}, time.Minute, time.Second).ShouldNot(HaveOccurred())
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
