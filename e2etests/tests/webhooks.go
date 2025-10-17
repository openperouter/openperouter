// SPDX-License-Identifier:Apache-2.0

package tests

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Webhooks", func() {
	var cs clientset.Interface

	BeforeEach(func() {
		cs = k8sclient.New()
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	Context("when VNIs webhooks are enabled", func() {
		BeforeEach(func() {
			vni1 := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "test-vrf-1",
					HostSession: &v1alpha1.HostSession{
						ASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.0.0/24",
						},
						HostASN: 65002,
					},
					VNI:       100,
					VXLanPort: 4789,
				},
			}
			By("creating the first VNI")
			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("the webhook should block",
			func(vni v1alpha1.L3VNI, expectedError string) {
				err := Updater.Update(config.Resources{
					L3VNIs: []v1alpha1.L3VNI{vni},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			},
			Entry("when trying to create a VNI without VRF field", v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-no-vrf",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VNI:       103,
					VXLanPort: 4789,
					HostSession: &v1alpha1.HostSession{
						ASN:     65001,
						HostASN: 65002,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.3.0/24",
						},
					},
				},
			}, "spec.vrf in body should match"),
			Entry("when trying to create a VNI with the same VNI as an existing one", v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       "test-vrf-2",
					VNI:       100,
					VXLanPort: 4789,
					HostSession: &v1alpha1.HostSession{
						ASN:     65001,
						HostASN: 65002,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.1.0/24",
						},
					},
				},
			}, "duplicate vni"),
			Entry("when trying to create a VNI with an invalid CIDR", v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-3",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       "test-vrf-3",
					VNI:       101,
					VXLanPort: 4789,
					HostSession: &v1alpha1.HostSession{
						ASN:     65001,
						HostASN: 65002,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "invalid-cidr",
						},
					},
				},
			}, "invalid local CIDR"),
			Entry("when trying to create a VNI with the same local and remote ASN", v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-4",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       "test-vrf-4",
					VNI:       102,
					VXLanPort: 4789,
					HostSession: &v1alpha1.HostSession{
						ASN:     65001,
						HostASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.2.0/24",
						},
					},
				},
			}, "hostASN must be different from asn"),
		)
	})

	Context("when L2VNIs webhooks are enabled", func() {
		BeforeEach(func() {
			l2vni1 := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-l2vni-1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:       200,
					VXLanPort: 4789,
				},
			}
			By("creating the first L2VNI")
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("the webhook should block",
			func(l2vni v1alpha1.L2VNI, expectedError string) {
				err := Updater.Update(config.Resources{
					L2VNIs: []v1alpha1.L2VNI{l2vni},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			},
			Entry("when trying to create an L2VNI with the same VNI as an existing one", v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-l2vni-2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:       200, // Same VNI as l2vni1
					VXLanPort: 4789,
				},
			}, "duplicate vni"),
		)
	})

	Context("when L2VNI immutability is tested", func() {
		BeforeEach(func() {
			l2vni1 := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:         300,
					VXLanPort:   4789,
					L2GatewayIP: "192.168.10.1/24",
				},
			}
			By("creating an L2VNI with gateway IP")
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block updates to L2GatewayIP", func() {
			// Try to update the L2GatewayIP
			l2vniUpdated := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L2VNISpec{
					VNI:         300,
					VXLanPort:   4789,
					L2GatewayIP: "192.168.20.1/24", // Different gateway IP
				},
			}

			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vniUpdated},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("L2GatewayIP can't be changed"))
		})
	})

	Context("when L3VNI immutability is tested", func() {
		BeforeEach(func() {
			l3vni1 := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l3vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       "vrf-immutable",
					VNI:       400,
					VXLanPort: 4789,
					HostSession: &v1alpha1.HostSession{
						ASN:     65000,
						HostASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.0.0/24",
						},
					},
				},
			}
			By("creating an L3VNI with LocalCIDR")
			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{l3vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block updates to LocalCIDR", func() {
			l3vniUpdated := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l3vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       "vrf-immutable",
					VNI:       400,
					VXLanPort: 4789,
					HostSession: &v1alpha1.HostSession{
						ASN:     65000,
						HostASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.0.1.0/24",
						},
					},
				},
			}

			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{l3vniUpdated},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("LocalCIDR can't be changed"))
		})
	})

	Context("when Underlay webhooks are enabled", func() {
		DescribeTable("the webhook should block",
			func(underlay v1alpha1.Underlay, expectedError string) {
				err := Updater.Update(config.Resources{
					Underlays: []v1alpha1.Underlay{underlay},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			},
			Entry("when trying to create an underlay with multiple nics", v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:  65000,
					Nics: []string{"nic1", "nic2"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
				},
			}, "can only have one nic"),
			Entry("when trying to create an underlay with invalid vtep cidr", v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:  65000,
					Nics: []string{"nic1"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "notacidr",
					},
				},
			}, "invalid vtep CIDR"),
			Entry("when trying to create an underlay with a neighbor with the same ASN", v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:  65000,
					Nics: []string{"nic1"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     65000,
							Address: "192.168.1.1",
						},
					},
				},
			}, "local ASN 65000 must be different from remote ASN 65000"),
		)
	})

	Context("when multiple underlay scenarios are tested", func() {
		BeforeEach(func() {
			underlay := v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:  65000,
					Nics: []string{"nic1"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "192.168.1.0/24",
					},
				},
			}
			By("creating the first underlay")
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("the webhook should block (multi-underlay and invalid update cases)",
			func(underlays []v1alpha1.Underlay, expectedError string) {
				err := Updater.Update(config.Resources{
					Underlays: underlays,
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			},
			Entry("when trying to create a second different underlay (should fail)",
				[]v1alpha1.Underlay{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "underlay2",
							Namespace: openperouter.Namespace,
						},
						Spec: v1alpha1.UnderlaySpec{
							ASN:  65001,
							Nics: []string{"nic2"},
							EVPN: &v1alpha1.EVPNConfig{
								VTEPCIDR: "192.168.2.0/24",
							},
						},
					},
				},
				"can't have more than one underlay",
			),
			Entry("when updating the existing underlay with an invalid CIDR (should fail)",
				[]v1alpha1.Underlay{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "underlay1",
							Namespace: openperouter.Namespace,
						},
						Spec: v1alpha1.UnderlaySpec{
							ASN:  65000,
							Nics: []string{"nic1"},
							EVPN: &v1alpha1.EVPNConfig{
								VTEPCIDR: "notacidr",
							},
						},
					},
				},
				"invalid vtep CIDR",
			),
		)
	})

	Context("when L3Passthrough webhooks are enabled", func() {
		It("should block creating more than one passthrough", func() {
			passthrough1 := v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-passthrough-1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 65010,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.10.0.0/24",
						},
						HostASN: 65011,
					},
				},
			}
			By("creating the first L3Passthrough")
			err := Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthrough1},
			})
			Expect(err).NotTo(HaveOccurred())

			By("creating the second L3Passthrough")
			passthrough2 := v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-passthrough-2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 65020,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.20.0.0/24",
						},
						HostASN: 65021,
					},
				},
			}
			err = Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthrough2},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("more than one"))

		})

		DescribeTable("the webhook should block",
			func(passthrough v1alpha1.L3Passthrough, expectedError string) {
				err := Updater.Update(config.Resources{
					L3Passthrough: []v1alpha1.L3Passthrough{passthrough},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			},
			Entry("when trying to create an L3Passthrough with invalid CIDR", v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-passthrough-3",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 65030,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "invalid-cidr",
						},
						HostASN: 65031,
					},
				},
			}, "invalid local CIDR"),
			Entry("when trying to create an L3Passthrough with the same local and remote ASN", v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-passthrough-4",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 65040,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.40.0.0/24",
						},
						HostASN: 65040,
					},
				},
			}, "must be different from remote ASN"),
		)
	})

	Context("when L3Passthrough immutability is tested", func() {
		BeforeEach(func() {
			passthrough1 := v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "passthrough-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 65050,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.50.0.0/24",
						},
						HostASN: 65051,
					},
				},
			}
			By("creating an L3Passthrough with LocalCIDR")
			err := Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthrough1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block updates to LocalCIDR", func() {
			passthroughUpdated := v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "passthrough-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 65050,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.60.0.0/24", // Different LocalCIDR
						},
						HostASN: 65051,
					},
				},
			}

			err := Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthroughUpdated},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("LocalCIDR can't be changed"))
		})
	})

	Context("when L3Passthrough overlaps are tested", func() {
		BeforeEach(func() {
			l3vni1 := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "overlap-vni",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: "vrf-overlap",
					HostSession: &v1alpha1.HostSession{
						ASN: 65070,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.70.0.0/24",
						},
						HostASN: 65071,
					},
					VNI:       500,
					VXLanPort: 4789,
				},
			}
			By("creating an L3VNI with a specific LocalCIDR")
			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{l3vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block L3Passthrough creation with overlapping LocalCIDR", func() {
			passthroughOverlap := v1alpha1.L3Passthrough{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "passthrough-overlap",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3PassthroughSpec{
					HostSession: v1alpha1.HostSession{
						ASN: 65080,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: "10.70.0.0/24", // Same CIDR as L3VNI
						},
						HostASN: 65081,
					},
				},
			}

			err := Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthroughOverlap},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
	})

})
