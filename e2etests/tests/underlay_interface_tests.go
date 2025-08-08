// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// NOTE: These are BDD-style integration tests that define the expected behavior
// of Underlay interface management functionality. The implementation is not yet
// complete, so these tests are not expected to pass initially. They serve as
// living documentation of the intended behavior and will guide the implementation.

var _ = Describe("Underlay Interface Management", func() {
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

	Context("Direct NIC Movement", func() {
		It("moves existing NIC to router namespace and reports 'Moved' status", func() {
			By("creating an Underlay with direct attachment mode and existing NIC")
			underlay := directModeUnderlay("direct-nic-test", []string{"toswitch"})

			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())

			By("expecting UnderlayNodeStatus to show interface as 'Moved'")
			nodes, err := getClusterNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes {
				Eventually(func(g Gomega) {
					status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, underlay.Name)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(status.Status.InterfaceStatuses).To(HaveLen(1))
					g.Expect(status.Status.InterfaceStatuses[0]).To(SatisfyAll(
						HaveField("Name", "toswitch"),
						HaveField("Status", v1alpha1.InterfaceStatusMoved),
						HaveField("Message", ContainSubstring("successfully moved")),
					))
				}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			}

			By("verifying interface is actually moved to router namespace")
			for _, node := range nodes {
				Eventually(func(g Gomega) {
					err := checkInterfaceInRouterNamespace(node.Name, "toswitch")
					g.Expect(err).NotTo(HaveOccurred(), "Interface should be present in router namespace")
				}).WithTimeout(60 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
			}
		})

		It("handles non-existent NIC and reports 'NotFound' status", func() {
			By("creating an Underlay with direct attachment mode and non-existent NIC")
			underlay := directModeUnderlay("missing-nic-test", []string{"nonexistent-nic"})

			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())

			By("expecting UnderlayNodeStatus to show interface as 'NotFound'")
			nodes, err := getClusterNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes {
				Eventually(func(g Gomega) {
					status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, underlay.Name)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(status.Status.InterfaceStatuses).To(HaveLen(1))
					g.Expect(status.Status.InterfaceStatuses[0]).To(SatisfyAll(
						HaveField("Name", "nonexistent-nic"),
						HaveField("Status", v1alpha1.InterfaceStatusNotFound),
						HaveField("Message", ContainSubstring("does not exist")),
					))
				}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			}
		})

		It("detects attempt to change interface and reports error", func() {
			By("creating initial Underlay with one interface")
			underlay := directModeUnderlay("interface-change-test", []string{"toswitch"})

			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for initial interface configuration")
			nodes, err := getClusterNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes {
				Eventually(func(g Gomega) {
					status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, underlay.Name)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(status.Status.InterfaceStatuses).To(HaveLen(1))
					g.Expect(status.Status.InterfaceStatuses[0]).To(SatisfyAll(
						HaveField("Name", "toswitch"),
						HaveField("Status", v1alpha1.InterfaceStatusMoved),
					))
				}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			}

			By("attempting to change to a different interface")
			underlay.Spec.Nics = []string{"eth2"}
			err = Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())

			By("expecting error status due to interface change conflict")
			for _, node := range nodes {
				Eventually(func(g Gomega) {
					status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, underlay.Name)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(status.Status.InterfaceStatuses).To(HaveLen(1))
					g.Expect(status.Status.InterfaceStatuses[0]).To(SatisfyAll(
						HaveField("Status", v1alpha1.InterfaceStatusError),
						HaveField("Message", ContainSubstring("existing underlay found")),
					))
				}).WithTimeout(60 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
			}
		})

		It("recovers if invalid configuration is replaced with valid", func() {
			By("creating an Underlay that would cause configuration issues")
			underlay := directModeUnderlay("node-failure-test", []string{"nonexistent-nic"})

			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())

			By("expecting status to report interface not found")
			nodes, err := getClusterNodes(cs)
			Expect(err).NotTo(HaveOccurred())
			for _, node := range nodes {
				Eventually(func(g Gomega) {
					status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, underlay.Name)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(status.Status.InterfaceStatuses).To(HaveLen(1))
					g.Expect(status.Status.InterfaceStatuses[0]).To(SatisfyAll(
						HaveField("Name", "nonexistent-nic"),
						HaveField("Status", v1alpha1.InterfaceStatusNotFound),
					))
				}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			}

			By("updating to valid interface after simulated resolution")
			underlay.Spec.Nics = []string{"toswitch"}
			err = Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{underlay},
			})
			Expect(err).NotTo(HaveOccurred())

			By("expecting status to recover and show successful configuration")
			for _, node := range nodes {
				Eventually(func(g Gomega) {
					status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, underlay.Name)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(status.Status.InterfaceStatuses).To(HaveLen(1))
					g.Expect(status.Status.InterfaceStatuses[0]).To(SatisfyAll(
						HaveField("Name", "toswitch"),
						HaveField("Status", v1alpha1.InterfaceStatusMoved),
					))
				}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			}
		})
	})

})

// Factory functions for creating test Underlay objects
func directModeUnderlay(name string, nics []string) v1alpha1.Underlay {
	return v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN:      64514,
			VTEPCIDR: "100.65.0.0/24",
			Nics:     nics,
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     64514,
					Address: "192.168.11.2",
				},
			},
		},
	}
}

// Helper functions
func getClusterNodes(cs clientset.Interface) ([]corev1.Node, error) {
	nodeList, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

// Network interface checking helpers
func checkInterfaceInHostNamespace(nodeName, interfaceName string) error {
	routerPodName := fmt.Sprintf("router-%s", nodeName)
	exec := executor.ForPod(openperouter.Namespace, routerPodName, "frr")
	_, err := exec.Exec("ip", "link", "show", interfaceName)
	return err
}

func checkInterfaceInRouterNamespace(nodeName, interfaceName string) error {
	// Find router pod running on the specified node
	cs := k8sclient.New()
	pods, err := cs.CoreV1().Pods(openperouter.Namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=router",
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})
	if err != nil {
		return fmt.Errorf("failed to list router pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no router pod found on node %s", nodeName)
	}

	routerPodName := pods.Items[0].Name
	exec := executor.ForPod(openperouter.Namespace, routerPodName, "frr")
	_, err = exec.Exec("ip", "link", "show", interfaceName)
	return err
}
