// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"

	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Hot-Apply Configuration Changes", Ordered, func() {
	var cs clientset.Interface
	var initialRouters openperouter.Routers

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		cs = k8sclient.New()
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		// Wait for the router pods to rollout after removing the underlay
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(initialRouters, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	It("adds a neighbor without restarting the router pods", func() {
		By("Deploying underlay with a single neighbor")
		singleNeighborUnderlay := v1alpha1.Underlay{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "underlay",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.UnderlaySpec{
				ASN:  64514,
				Nics: []string{"toswitch"},
				Neighbors: []v1alpha1.Neighbor{
					{
						ASN:     64512,
						Address: "192.168.11.2",
					},
				},
				EVPN: &v1alpha1.EVPNConfig{
					VTEPCIDR: "100.65.0.0/24",
				},
			},
		}
		err := Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{singleNeighborUnderlay},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for BGP session with first neighbor to establish")
		exec := executor.ForContainer(infra.KindLeaf)
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes := nodesItems.Items

		Eventually(func() error {
			for _, node := range nodes {
				neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
				if err != nil {
					continue
				}
				validateSessionWithNeighbor(infra.KindLeaf, node.Name, exec, neighborIP, Established)
			}
			return nil
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		By("Recording router state before adding neighbor")
		initialRouters, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		By("Adding a second neighbor to the underlay")
		twoNeighborUnderlay := singleNeighborUnderlay.DeepCopy()
		twoNeighborUnderlay.Spec.Neighbors = append(twoNeighborUnderlay.Spec.Neighbors, v1alpha1.Neighbor{
			ASN:     64513,
			Address: "192.168.12.2",
		})
		twoNeighborUnderlay.Spec.Nics = []string{"toswitch", "toswitch2"}
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{*twoNeighborUnderlay},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for BGP session with second neighbor to establish")
		exec2 := executor.ForContainer(infra.KindLeaf2)
		Eventually(func() error {
			for _, node := range nodes {
				neighborIP, err := infra.NeighborIP(infra.KindLeaf2, node.Name)
				if err != nil {
					continue
				}
				validateSessionWithNeighbor(infra.KindLeaf2, node.Name, exec2, neighborIP, Established)
			}
			return nil
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		By("Verifying router pods were NOT restarted")
		Consistently(func() error {
			currentRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(initialRouters, currentRouters)
		}, 30*time.Second, 5*time.Second).Should(HaveOccurred())
	})

})
