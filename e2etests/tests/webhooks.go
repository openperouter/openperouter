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
	"k8s.io/utils/ptr"
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
					VRF: ptr.To("test-vrf-1"),
					HostSession: &v1alpha1.HostSession{
						ASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.0.0.0/24"),
						},
						HostASN: ptr.To(int64(65002)),
					},
					VNI:       ptr.To(int64(100)),
					VXLanPort: ptr.To(int32(4789)),
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
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
			},
			Entry("when trying to create a VNI without VRF field", v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-no-vrf",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VNI:       ptr.To(int64(103)),
					VXLanPort: ptr.To(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN:     65001,
						HostASN: ptr.To(int64(65002)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.0.3.0/24"),
						},
					},
				},
			}, "spec.vrf: Required value"),
			Entry("when trying to create a VNI with the same VNI as an existing one", v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       ptr.To("test-vrf-2"),
					VNI:       ptr.To(int64(100)),
					VXLanPort: ptr.To(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN:     65001,
						HostASN: ptr.To(int64(65002)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.0.1.0/24"),
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
					VRF:       ptr.To("test-vrf-3"),
					VNI:       ptr.To(int64(101)),
					VXLanPort: ptr.To(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN:     65001,
						HostASN: ptr.To(int64(65002)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("invalid-cidr"),
						},
					},
				},
			}, "invalid local CIDR"),
		)
	})

	Context("when L2VNI is created before L3VNI with overlap", func() {
		It("should fail", func() {
			l2vni1 := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-l2vni-1",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VRF:          ptr.To("test-vrf-1"),
					VNI:          ptr.To(int64(200)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"10.0.0.1/25"},
				},
			}
			By("creating the L2VNI")
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni1},
			})
			Expect(err).NotTo(HaveOccurred())

			l3vni1 := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: ptr.To("test-vrf-1"),
					HostSession: &v1alpha1.HostSession{
						ASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.0.0.0/24"),
							IPv6: ptr.To("2000::1/64"),
						},
						HostASN: ptr.To(int64(65002)),
					},
					VNI:       ptr.To(int64(100)),
					VXLanPort: ptr.To(int32(4789)),
				},
			}
			By("creating the L3VNI")
			err = Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{l3vni1},
			})
			Expect(err).To(MatchError(ContainSubstring((`subnet overlap in VRF "test-vrf-1": IPNet 10.0.0.0/24 ` +
				`(L3VNI openperouter-system/test-vni-1) overlaps with IPNet 10.0.0.0/25 ` +
				`(L2VNI openperouter-system/test-l2vni-1)`))))
		})
	})

	Context("when L2VNIs webhooks are enabled", func() {
		BeforeEach(func() {
			l2vni1 := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-l2vni-1",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:       ptr.To(int64(200)),
					VXLanPort: ptr.To(int32(4789)),
				},
			}
			By("creating the first L2VNI")
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni1},
			})
			Expect(err).NotTo(HaveOccurred())

			l3vni1 := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vni-1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF: ptr.To("test-vrf-1"),
					HostSession: &v1alpha1.HostSession{
						ASN: 65001,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.0.0.0/24"),
							IPv6: ptr.To("2000::1/64"),
						},
						HostASN: ptr.To(int64(65002)),
					},
					VNI:       ptr.To(int64(100)),
					VXLanPort: ptr.To(int32(4789)),
				},
			}
			By("creating the first L3VNI")
			err = Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{l3vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("the webhook should block",
			func(l2vnis []v1alpha1.L2VNI, expectedError string) {
				err := Updater.Update(config.Resources{
					L2VNIs: l2vnis,
				})
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
			},
			Entry("when trying to create an L2VNI with the same VNI as an existing one", []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-l2vni-2",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:       ptr.To(int64(200)), // Same VNI as l2vni1
					VXLanPort: ptr.To(int32(4789)),
				},
			}}, "duplicate vni"),
			Entry("when trying to create an L2VNI with an invalid IPv4 address", []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-invalid-ip4",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(201)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"not-an-ip-address"},
				},
			}}, `invalid l2gatewayips for vni "l2-invalid-ip4" = [not-an-ip-address]: invalid cidr: invalid CIDR address: not-an-ip-address`),
			Entry("when trying to create an L2VNI with an invalid format in L2GatewayIP", []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-bad-format",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(202)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"256.256.256.256/24"},
				},
			}}, `invalid l2gatewayips for vni "l2-bad-format" = [256.256.256.256/24]: invalid cidr: invalid CIDR address: 256.256.256.256/24`),
			Entry("when trying to create an L2VNI with mixed valid and invalid IPs", []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-mixed-ips",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(203)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.1.1/24", "invalid-ip"},
				},
			}}, `invalid l2gatewayips for vni "l2-mixed-ips" = [192.168.1.1/24 invalid-ip]: invalid cidr: invalid CIDR address: invalid-ip`),
			Entry("when trying to create an L2VNI with more than 2 IPs", []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-too-many",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(204)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.1.1/24", "2001:db8::1/64", "10.0.0.1/24"},
				},
			}}, "Too many"),
			Entry("when trying to create an L2VNI with 2 IPv4 addresses", []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-two-ipv4",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(205)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.1.1/24", "10.0.0.1/24"},
				},
			}}, `invalid l2gatewayips for vni "l2-two-ipv4" = [192.168.1.1/24 10.0.0.1/24]: IPFamilyForAddresses: same address family ["192.168.1.1" "10.0.0.1"]`),
			Entry("when trying to create an L2VNI with 2 IPv6 addresses", []v1alpha1.L2VNI{{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-two-ipv6",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(206)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"2001:db8::1/64", "2001:db9::1/64"},
				},
			}}, `invalid l2gatewayips for vni "l2-two-ipv6" = [2001:db8::1/64 2001:db9::1/64]: IPFamilyForAddresses: same address family ["2001:db8::1" "2001:db9::1"]`),
			Entry("when trying to create an L2VNI with overlapping IPv4 address", []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "first",
						Namespace: openperouter.Namespace,
					},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(207)),
						VRF:          ptr.To("test-vrf-1"),
						VXLanPort:    ptr.To(int32(4789)),
						L2GatewayIPs: []string{"192.168.123.1/24", "2000:0:0:123::1/64"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "second",
						Namespace: openperouter.Namespace,
					},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(208)),
						VRF:          ptr.To("test-vrf-1"),
						VXLanPort:    ptr.To(int32(4789)),
						L2GatewayIPs: []string{"192.168.123.1/24"},
					},
				},
			}, `subnet overlap in VRF "test-vrf-1": IPNet 192.168.123.0/24 (L2VNI openperouter-system/second) `+
				`overlaps with IPNet 192.168.123.0/24 (L2VNI openperouter-system/first)`),
			Entry("when trying to create an L2VNI with overlapping IPv6 address", []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "first",
						Namespace: openperouter.Namespace,
					},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(209)),
						VRF:          ptr.To("test-vrf-1"),
						VXLanPort:    ptr.To(int32(4789)),
						L2GatewayIPs: []string{"192.168.123.1/24", "2000:0:0:123::1/64"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "second",
						Namespace: openperouter.Namespace,
					},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(210)),
						VRF:          ptr.To("test-vrf-1"),
						VXLanPort:    ptr.To(int32(4789)),
						L2GatewayIPs: []string{"2000:0:0:123::1:1/112"},
					},
				},
			}, `subnet overlap in VRF "test-vrf-1": IPNet 2000::123:0:0:1:0/112 (L2VNI openperouter-system/second) `+
				`overlaps with IPNet 2000:0:0:123::/64 (L2VNI openperouter-system/first)`),
			Entry("when trying to create an L2VNI with IPv4 address that overlaps with L3VNI", []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "first",
						Namespace: openperouter.Namespace,
					},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(207)),
						VRF:          ptr.To("test-vrf-1"),
						VXLanPort:    ptr.To(int32(4789)),
						L2GatewayIPs: []string{"10.0.0.1/24"},
					},
				},
			}, `subnet overlap in VRF "test-vrf-1": IPNet 10.0.0.0/24 (L3VNI openperouter-system/test-vni-1) `+
				`overlaps with IPNet 10.0.0.0/24 (L2VNI openperouter-system/first)`),
			Entry("when trying to create an L2VNI with IPv6 address that overlaps with L3VNI", []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "first",
						Namespace: openperouter.Namespace,
					},
					Spec: &v1alpha1.L2VNISpec{
						VNI:          ptr.To(int64(207)),
						VRF:          ptr.To("test-vrf-1"),
						VXLanPort:    ptr.To(int32(4789)),
						L2GatewayIPs: []string{"2000::2/64"},
					},
				},
			}, `subnet overlap in VRF "test-vrf-1": IPNet 2000::/64 (L3VNI openperouter-system/test-vni-1) `+
				`overlaps with IPNet 2000::/64 (L2VNI openperouter-system/first)`),
		)

		It("should allow creating an L2VNI with valid IPv4 address", func() {
			l2vni := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-valid-ipv4",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(210)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.1.1/24"},
				},
			}
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow creating an L2VNI with valid IPv6 address", func() {
			l2vni := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-valid-ipv6",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(211)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"2001:db8::1/64"},
				},
			}
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should allow creating an L2VNI with dual-stack (IPv4 and IPv6) addresses", func() {
			l2vni := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2-dualstack",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(212)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.1.1/24", "2001:db8::1/64"},
				},
			}
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when L2VNI immutability is tested", func() {
		BeforeEach(func() {
			l2vni1 := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(300)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.10.1/24"},
				},
			}
			By("creating an L2VNI with gateway IP")
			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block updates to L2GatewayIPs when changing IP", func() {
			l2vniUpdated := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(300)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.20.1/24"},
				},
			}

			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vniUpdated},
			})
			Expect(err).To(MatchError(ContainSubstring("L2GatewayIPs cannot be changed")))
		})

		It("should block updates to L2GatewayIPs when adding an IP", func() {
			l2vniUpdated := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(300)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{"192.168.10.1/24", "2001:db8::1/64"},
				},
			}

			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vniUpdated},
			})
			Expect(err).To(MatchError(ContainSubstring("L2GatewayIPs cannot be changed")))
		})

		It("should block updates to L2GatewayIPs when removing an IP", func() {
			l2vniUpdated := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "l2vni-immutable",
					Namespace: openperouter.Namespace,
				},
				Spec: &v1alpha1.L2VNISpec{
					VNI:          ptr.To(int64(300)),
					VXLanPort:    ptr.To(int32(4789)),
					L2GatewayIPs: []string{},
				},
			}

			err := Updater.Update(config.Resources{
				L2VNIs: []v1alpha1.L2VNI{l2vniUpdated},
			})
			Expect(err).To(MatchError(ContainSubstring("L2GatewayIPs cannot be changed")))
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
					VRF:       ptr.To("vrf-immutable"),
					VNI:       ptr.To(int64(400)),
					VXLanPort: ptr.To(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN:     65000,
						HostASN: ptr.To(int64(65001)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.0.0.0/24"),
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
					VRF:       ptr.To("vrf-immutable"),
					VNI:       ptr.To(int64(400)),
					VXLanPort: ptr.To(int32(4789)),
					HostSession: &v1alpha1.HostSession{
						ASN:     65000,
						HostASN: ptr.To(int64(65001)),
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.0.1.0/24"),
						},
					},
				},
			}

			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{l3vniUpdated},
			})
			Expect(err).To(MatchError(ContainSubstring("LocalCIDR can't be changed")))
		})
	})

	Context("when Underlay webhooks are enabled", func() {
		DescribeTable("the webhook should block",
			func(underlay v1alpha1.Underlay, expectedError string) {
				err := Updater.Update(config.Resources{
					Underlays: []v1alpha1.Underlay{underlay},
				})
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
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
						VTEPCIDR: ptr.To("192.168.1.0/24"),
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
						VTEPCIDR: ptr.To("notacidr"),
					},
				},
			}, "invalid vtep CIDR"),
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
						VTEPCIDR: ptr.To("192.168.1.0/24"),
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
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
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
								VTEPCIDR: ptr.To("192.168.2.0/24"),
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
								VTEPCIDR: ptr.To("notacidr"),
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
							IPv4: ptr.To("10.10.0.0/24"),
						},
						HostASN: ptr.To(int64(65011)),
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
							IPv4: ptr.To("10.20.0.0/24"),
						},
						HostASN: ptr.To(int64(65021)),
					},
				},
			}
			err = Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthrough2},
			})
			Expect(err).To(MatchError(ContainSubstring("more than one")))
		})

		DescribeTable("the webhook should block",
			func(passthrough v1alpha1.L3Passthrough, expectedError string) {
				err := Updater.Update(config.Resources{
					L3Passthrough: []v1alpha1.L3Passthrough{passthrough},
				})
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
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
							IPv4: ptr.To("invalid-cidr"),
						},
						HostASN: ptr.To(int64(65031)),
					},
				},
			}, "invalid local CIDR"),
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
							IPv4: ptr.To("10.50.0.0/24"),
						},
						HostASN: ptr.To(int64(65051)),
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
							IPv4: ptr.To("10.60.0.0/24"), // Different LocalCIDR
						},
						HostASN: ptr.To(int64(65051)),
					},
				},
			}

			err := Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthroughUpdated},
			})
			Expect(err).To(MatchError(ContainSubstring("LocalCIDR can't be changed")))
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
					VRF: ptr.To("vrf-overlap"),
					HostSession: &v1alpha1.HostSession{
						ASN: 65070,
						LocalCIDR: v1alpha1.LocalCIDRConfig{
							IPv4: ptr.To("10.70.0.0/24"),
						},
						HostASN: ptr.To(int64(65071)),
					},
					VNI:       ptr.To(int64(500)),
					VXLanPort: ptr.To(int32(4789)),
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
							IPv4: ptr.To("10.70.0.0/24"), // Same CIDR as L3VNI
						},
						HostASN: ptr.To(int64(65081)),
					},
				},
			}

			err := Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{passthroughOverlap},
			})
			Expect(err).To(MatchError(ContainSubstring("overlapping")))
		})
	})
})
