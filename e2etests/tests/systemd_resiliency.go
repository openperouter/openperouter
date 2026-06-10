// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"

	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Systemd Router Restart Resiliency", Label("systemd"), Ordered, func() {
	var cs clientset.Interface
	nodes := []corev1.Node{}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		_, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				infra.Underlay,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		// Configure leaf switches with node neighbors
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
		Expect(infra.LeafKind2Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the underlay to be removed from all nodes")
		for _, node := range nodes {
			Eventually(func(g Gomega) {
				isConfigured, err := openperouter.UnderlayConfigured(node.Name)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(isConfigured).To(BeFalse())
			}, 2*time.Minute, time.Second).Should(Succeed())
		}
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	validateTORSessions := func() {
		leaves := []string{infra.KindLeaf, infra.KindLeaf2}
		for _, leaf := range leaves {
			exec := executor.ForContainer(leaf)
			Eventually(func() error {
				for _, node := range nodes {
					neighborIP, err := infra.NeighborIP(leaf, node.Name)
					Expect(err).NotTo(HaveOccurred())
					validateSessionWithNeighbor(leaf, node.Name, exec, neighborIP, Established)
				}
				return nil
			}, time.Minute, time.Second).ShouldNot(HaveOccurred())
		}
	}

	restartRouterOnNode := func(node corev1.Node) {
		By("restarting FRR container via systemd on node " + node.Name)
		// Exec directly into the kind node (docker container) where systemd runs
		nodeExec := executor.ForContainer(node.Name)

		_, err := nodeExec.Exec("systemctl", "restart", "routerpod-pod.service")
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			output, err := nodeExec.Exec("systemctl", "is-active", "routerpod-pod.service")
			if err != nil {
				return nil
			}
			if strings.TrimSpace(output) != "active" {
				return nil
			}
			return fmt.Errorf("service still active")
		}, 30*time.Second, time.Second).Should(Succeed())
	}

	It("recovers BGP sessions after FRR restart", func() {
		By("validating initial sessions with TOR switches")
		validateTORSessions()

		By("restarting FRR on all nodes")
		for _, node := range nodes {
			restartRouterOnNode(node)
		}

		By("validating sessions re-established after restart")
		validateTORSessions()
	})

})
