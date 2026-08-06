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
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var _ = Describe("Neighbor passwordSecret", Ordered, func() {
	const (
		bgpPassword = "TestBGPSecret123"
		secretName  = "bgp-auth-test"
	)

	var cs clientset.Interface
	nodes := []corev1.Node{}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		cs = k8sclient.New()
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items
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
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("resetting leaf switches to no password")
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
	})

	It("should resolve password from Secret and re-establish after rotation", func() {
		const rotatedPassword = "RotatedSecret456"

		By("creating a basic-auth Secret with the BGP password")
		Expect(
			k8s.CreateBasicAuthSecret(cs, secretName, openperouter.Namespace, bgpPassword),
		).To(Succeed())

		By("configuring the leaf switch with the matching password")
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{
			Password: bgpPassword,
		})).To(Succeed())

		By("creating the underlay with passwordSecret referencing the Secret")
		underlay := v1alpha1.Underlay{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "underlay",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.UnderlaySpec{
				ASN: 64514,
				Interfaces: []v1alpha1.UnderlayInterface{
					{Type: "NetworkDevice", NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "toswitch1"}},
				},
				TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
					CIDRs: []string{"100.65.0.0/24"},
				},
				Neighbors: []v1alpha1.Neighbor{
					{
						ASN:            new(int64(64512)),
						Address:        new("192.168.11.2"),
						PasswordSecret: new(secretName),
					},
				},
			},
		}

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{underlay},
		})).To(Succeed())

		By("verifying the BGP session is established with the authenticated peer")
		exec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				exec,
				validationParameters{
					fromName:    infra.KindLeaf,
					toName:      node.Name,
					neighborIP:  neighborIP,
					established: Established,
				},
			)
		}

		By("rotating the password in both the Secret and the leaf switch")
		Expect(
			k8s.UpdateBasicAuthSecret(cs, secretName, openperouter.Namespace, rotatedPassword),
		).To(Succeed())
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{
			Password: rotatedPassword,
		})).To(Succeed())

		By("verifying the session re-establishes with the rotated password")
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionWithNeighbor(
				exec,
				validationParameters{
					fromName:    infra.KindLeaf,
					toName:      node.Name,
					neighborIP:  neighborIP,
					established: Established,
				},
			)
		}
	})
})
