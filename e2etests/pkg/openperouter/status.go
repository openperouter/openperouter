// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetNodeStatus(cli client.Client, nodeName string) (*v1alpha1.RouterNodeConfigurationStatus, error) {
	status := &v1alpha1.RouterNodeConfigurationStatus{}
	err := cli.Get(context.Background(), client.ObjectKey{
		Name:      nodeName,
		Namespace: Namespace,
	}, status)
	if err != nil {
		return nil, err
	}
	return status, nil
}

// AssertNodesStatusReady verifies that all RouterNodeConfigurationStatus CRs
// have Ready=True and Degraded=False conditions set.
func AssertNodesStatusReady(cli client.Client) {
	Eventually(func(g Gomega) {
		statusList := &v1alpha1.RouterNodeConfigurationStatusList{}
		g.Expect(cli.List(context.Background(), statusList, &client.ListOptions{Namespace: Namespace})).To(Succeed())
		g.Expect(statusList.Items).NotTo(BeEmpty(), "should have at least one node status")

		for _, nodeStatus := range statusList.Items {
			g.Expect(nodeStatus.Status).NotTo(BeNil(),
				fmt.Sprintf("node-status %q should have status set", nodeStatus.Name))
			readyCond := apimeta.FindStatusCondition(nodeStatus.Status.Conditions, v1alpha1.ConditionTypeReady)
			g.Expect(readyCond).NotTo(BeNil(),
				fmt.Sprintf("node-status %q should have a Ready condition", nodeStatus.Name))
			g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue),
				fmt.Sprintf("node-status %q Ready should be True", nodeStatus.Name))
			degradedCond := apimeta.FindStatusCondition(nodeStatus.Status.Conditions, v1alpha1.ConditionTypeDegraded)
			g.Expect(degradedCond).NotTo(BeNil(),
				fmt.Sprintf("node-status %q should have a Degraded condition", nodeStatus.Name))
			g.Expect(degradedCond.Status).To(Equal(metav1.ConditionFalse),
				fmt.Sprintf("node-status %q Degraded should be False", nodeStatus.Name))
		}
	}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
}
