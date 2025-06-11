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
			vni1 := v1alpha1.VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vni1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.VNISpec{
					VNI:       1001,
					LocalCIDR: "192.168.1.0/24",
				},
			}
			By("creating the first VNI")
			err := Updater.Update(config.Resources{
				VNIs: []v1alpha1.VNI{vni1},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		DescribeTable("the webhook should block",
			func(vni v1alpha1.VNI, expectedError string) {
				err := Updater.Update(config.Resources{
					VNIs: []v1alpha1.VNI{vni},
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(expectedError))
			},
			Entry("when trying to create a VNI with the same VNI as an existing one", v1alpha1.VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vni2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.VNISpec{
					VNI:       1001,
					LocalCIDR: "192.168.2.0/24",
				},
			}, "duplicate vni"),
			Entry("when trying to create a VNI with an invalid CIDR", v1alpha1.VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vni2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.VNISpec{
					VNI:       1002,
					LocalCIDR: "thisisnotacidr",
				},
			}, "invalid local CIDR"),
			Entry("when updating a VNI with an invalid cidr", v1alpha1.VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vni1",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.VNISpec{
					VNI:       1001,
					LocalCIDR: "thisisnotacidr",
				},
			}, "invalid local CIDR"),
		)
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
					ASN:      65000,
					Nics:     []string{"nic1", "nic2"},
					VTEPCIDR: "192.168.1.0/24",
				},
			}, "can only have one nic"),
			Entry("when trying to create an underlay with invalid vtep cidr", v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:      65000,
					Nics:     []string{"nic1"},
					VTEPCIDR: "notacidr",
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
					ASN:      65000,
					Nics:     []string{"nic1"},
					VTEPCIDR: "192.168.1.0/24",
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
							ASN:      65001,
							Nics:     []string{"nic2"},
							VTEPCIDR: "192.168.2.0/24",
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
							ASN:      65000,
							Nics:     []string{"nic1"},
							VTEPCIDR: "notacidr",
						},
					},
				},
				"invalid vtep CIDR",
			),
		)
	})

})
