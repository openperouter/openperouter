// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openperouter/openperouter/api/v1alpha1"

	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
)

var _ = Describe("Node Router Status", func() {
	const routerNamespace = openperouter.Namespace

	var nodes []corev1.Node
	var cs kubernetes.Interface

	BeforeEach(func() {
		cs = k8sclient.New()

		var err error
		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).NotTo(BeEmpty(), "should have at least one node")
	})

	It("should ensure RouterNodeConfigurationStatus per each node", func() {
		assertSingleNodeStatusPerNode(nodes, routerNamespace)
		openperouter.AssertNodesStatusReady(Updater.Client())

		By("delete all CRs and verify they are recreated")
		Expect(Updater.Client().DeleteAllOf(context.Background(), &v1alpha1.RouterNodeConfigurationStatus{}, client.InNamespace(routerNamespace))).To(Succeed())

		assertSingleNodeStatusPerNode(nodes, routerNamespace)
		openperouter.AssertNodesStatusReady(Updater.Client())
	})
})

func assertSingleNodeStatusPerNode(nodes []corev1.Node, namespace string) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		nodeStatusList := &v1alpha1.RouterNodeConfigurationStatusList{}
		g.Expect(Updater.Client().List(context.Background(), nodeStatusList, &client.ListOptions{Namespace: namespace})).To(Succeed())

		g.Expect(nodeStatusList.Items).To(HaveLen(len(nodes)), "should have single node-status per node")

		nodeStatusByName := map[string]v1alpha1.RouterNodeConfigurationStatus{}
		for _, nodeStatus := range nodeStatusList.Items {
			nodeStatusByName[nodeStatus.Name] = nodeStatus
		}

		for _, node := range nodes {
			nodeStatus, exists := nodeStatusByName[node.Name]
			g.Expect(exists).To(BeTrueBecause("should have node-status for node %q", node.Name))

			g.Expect(nodeStatus.OwnerReferences).To(ConsistOf([]metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "Node",
				Name:       node.Name,
				UID:        node.UID,
			}}), "node-status %q should have owner reference to the node %q", nodeStatus.Name, node.Name)
		}
	}).WithTimeout(10 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
}
