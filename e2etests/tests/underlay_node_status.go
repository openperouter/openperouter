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
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// NOTE: These are BDD-style integration tests that define the expected behavior
// of UnderlayNodeStatus functionality. The controller implementation is not yet
// complete, so these tests are not expected to pass initially. They serve as
// living documentation of the intended behavior and will guide the implementation.

var _ = Describe("UnderlayNodeStatus Lifecycle", func() {
	var cs clientset.Interface
	routerPods := []*corev1.Pod{}

	BeforeEach(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routerPods, err = openperouter.RouterPods(cs)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			return openperouter.DaemonsetRolled(cs, routerPods)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	It("creates status objects when Underlay is created", func() {
		By("getting the actual cluster nodes")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">", 0), "Cluster should have at least one node")

		By("creating an Underlay resource")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("expecting the controller to create UnderlayNodeStatus objects")
		Eventually(func(g Gomega) {
			count, err := countUnderlayNodeStatuses(Updater.Client(), Updater.Namespace())
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(count).To(Equal(len(nodes)), "Controller should create UnderlayNodeStatus objects for each node")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying the status objects have correct node references")
		for _, node := range nodes {
			status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, infra.Underlay.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status).To(And(
				HaveField("Spec.NodeName", Equal(node.Name)),
				HaveField("Spec.UnderlayName", Equal(infra.Underlay.Name)),
			))
		}
	})

	It("cleans up status objects when Underlay is deleted", func() {
		By("getting the actual cluster nodes")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">", 0), "Cluster should have at least one node")

		By("creating an Underlay resource")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("expecting status objects to be created")
		Eventually(func(g Gomega) {
			count, err := countUnderlayNodeStatuses(Updater.Client(), Updater.Namespace())
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(count).To(Equal(len(nodes)), "Should create status objects for all nodes")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())

		By("deleting the Underlay")
		underlayToDelete := infra.Underlay.DeepCopy()
		err = Updater.Client().Delete(context.Background(), underlayToDelete)
		Expect(err).NotTo(HaveOccurred())

		By("expecting all status objects to be cleaned up via owner references")
		Eventually(func(g Gomega) {
			count, err := countUnderlayNodeStatuses(Updater.Client(), Updater.Namespace())
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(count).To(BeZero(), "Should remove all status objects when Underlay is deleted")
		}, 2*time.Minute, 5*time.Second).Should(Succeed())
	})

	It("updates lastReconciled timestamp", func() {
		By("getting the actual cluster nodes")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">", 0), "Cluster should have at least one node")

		By("creating an Underlay resource")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("expecting lastReconciled to be set for all status objects")
		for _, node := range nodes {
			Eventually(func(g Gomega) {
				status, err := getUnderlayNodeStatus(Updater.Client(), Updater.Namespace(), node.Name, infra.Underlay.Name)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(status).NotTo(BeNil())
				g.Expect(status).To(HaveField("Status.LastReconciled", Not(BeNil())))
			}, 2*time.Minute, 5*time.Second).Should(Succeed(), fmt.Sprintf("LastReconciled should be set during reconciliation for node %s", node.Name))
		}
	})
})

// Helper functions for UnderlayNodeStatus management

func getUnderlayNodeStatus(client ctrlclient.Client, namespace, nodeName, underlayName string) (*v1alpha1.UnderlayNodeStatus, error) {
	statusName := fmt.Sprintf("%s.%s", underlayName, nodeName)
	status := &v1alpha1.UnderlayNodeStatus{}

	err := client.Get(context.Background(), types.NamespacedName{
		Name:      statusName,
		Namespace: namespace,
	}, status)

	if err != nil {
		return nil, err
	}
	return status, nil
}

func countUnderlayNodeStatuses(client ctrlclient.Client, namespace string) (int, error) {
	statusList := &v1alpha1.UnderlayNodeStatusList{}
	err := client.List(context.Background(), statusList, &ctrlclient.ListOptions{})
	if err != nil {
		return 0, err
	}

	// Filter by namespace
	count := 0
	for _, status := range statusList.Items {
		if status.Namespace == namespace {
			count++
		}
	}
	return count, nil
}
