// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Namespace setup", Ordered, func() {
	var cs clientset.Interface
	nodes := []corev1.Node{}

	BeforeAll(func() {
		cs = k8sclient.New()
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		By("cleaning all objects")
		Expect(Updater.CleanAll()).To(Succeed())
		By("Waiting for all underlays to be deleted")
		Eventually(Updater.IsUnderlayClean).ProbeEvery(5 * time.Second).WithTimeout(120 * time.Second).Should(Succeed())

		By("Creating underlay")
		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})).To(Succeed())
	})

	It("has expected sysctl settings after underlay deployment", func() {
		By("making sure that perouter namespace has correct values for rp_filters on toswitch{1,2}, all, default interfaces")
		Eventually(func() error {
			for _, node := range nodes {
				if err := openperouter.UnderlaySysctls(
					node.Name,
					map[string]string{
						"net.ipv4.conf.default.rp_filter":   "0",
						"net.ipv4.conf.all.rp_filter":       "0",
						"net.ipv4.conf.toswitch1.rp_filter": "0",
						"net.ipv4.conf.toswitch2.rp_filter": "0",
					},
				); err != nil {
					return err
				}
			}
			return nil
		}).ProbeEvery(5 * time.Second).WithTimeout(60 * time.Second).Should(Succeed())
	})
})
