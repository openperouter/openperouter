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
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("Router Host configuration", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	frrk8sPods := []*corev1.Pod{}
	nodes := []corev1.Node{}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		frrk8sPods, err = frrk8s.Pods(cs)
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
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	BeforeEach(func() {
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
	})

	validateTORSession := func() {
		exec := executor.ForContainer(infra.KindLeaf)
		Eventually(func() error {
			for _, node := range nodes {
				neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
				Expect(err).NotTo(HaveOccurred())
				validateSessionWithNeighbor(infra.KindLeaf, node.Name, exec, neighborIP, Established)
			}
			return nil
		}, time.Minute, time.Second).ShouldNot(HaveOccurred())
	}
	It("peers with the tor", func() {
		validateTORSession()
	})

	Context("with a l3 vni", func() {
		vni := v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "red",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: "red",
				HostSession: &v1alpha1.HostSession{
					ASN:     64514,
					HostASN: 64515,
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: "192.169.10.0/24",
					},
				},
				VNI: 100,
			},
		}
		BeforeEach(func() {
			err := Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{
					vni,
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("establishes a session with the host and then removes it when deleting the vni", func() {
			frrConfig, err := frrk8s.ConfigFromHostSession(*vni.Spec.HostSession, vni.Name)
			Expect(err).ToNot(HaveOccurred())
			err = Updater.Update(config.Resources{
				FRRConfigurations: frrConfig,
			})
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForHostSession(vni.Name, *vni.Spec.HostSession, Established, frrk8sPods...)

			status.ExpectSuccessfulStatus(Updater.Client(), HostMode)

			By("deleting the vni removes the session with the host")
			err = Updater.Client().Delete(context.Background(), &vni)
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForHostSession(vni.Name, *vni.Spec.HostSession, !Established, frrk8sPods...)

			status.ExpectSuccessfulStatus(Updater.Client(), HostMode)
		})
	})

	Context("with a l3 passthrough", func() {
		l3Passthrough := v1alpha1.L3Passthrough{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "red",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3PassthroughSpec{
				HostSession: v1alpha1.HostSession{
					ASN:     64514,
					HostASN: 64515,
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: "192.169.10.0/24",
					},
				},
			},
		}
		BeforeEach(func() {
			err := Updater.Update(config.Resources{
				L3Passthrough: []v1alpha1.L3Passthrough{
					l3Passthrough,
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("establishes a session with the host and then removes it when deleting the vni", func() {
			frrConfig, err := frrk8s.ConfigFromHostSession(l3Passthrough.Spec.HostSession, l3Passthrough.Name)
			Expect(err).ToNot(HaveOccurred())
			err = Updater.Update(config.Resources{
				FRRConfigurations: frrConfig,
			})
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForHostSession(l3Passthrough.Name, l3Passthrough.Spec.HostSession, Established, frrk8sPods...)

			status.ExpectSuccessfulStatus(Updater.Client(), HostMode)

			By("deleting the vni removes the session with the host")
			err = Updater.Client().Delete(context.Background(), &l3Passthrough)
			Expect(err).NotTo(HaveOccurred())

			validateFRRK8sSessionForHostSession(l3Passthrough.Name, l3Passthrough.Spec.HostSession, !Established, frrk8sPods...)
		})
	})

	// This test must be the last of the ordered describe as it will remove the underlay
	It("deleting the underlay removes the session with the tor", func() {
		validateTORSession()

		By("deleting the vni removes the session with the host")
		err := Updater.Client().Delete(context.Background(), &infra.Underlay)
		Expect(err).NotTo(HaveOccurred())

		exec := executor.ForContainer(infra.KindLeaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			validateSessionDownForNeigh(exec, neighborIP)
		}
	})
})

var _ = Describe("Underlay BFD Configuration", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	nodes := []corev1.Node{}

	BeforeEach(func() {
		cs = k8sclient.New()
		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items

		neighbors := []string{}
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
			Expect(err).NotTo(HaveOccurred())
			neighbors = append(neighbors, neighborIP)
		}

		// Enable BFD on leafkind
		err = infra.UpdateLeafKindConfig(nodes, true)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for router pods to rollout")
		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		err = infra.UpdateLeafKindConfig(nodes, false)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("should establish BFD sessions with the ToR",
		func(underlay v1alpha1.Underlay) {
			By("applying the underlay configuration")
			err := Updater.Update(config.Resources{
				Underlays: []v1alpha1.Underlay{
					underlay,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("validating BFD sessions are established")
			exec := executor.ForContainer(infra.KindLeaf)
			Eventually(func() error {
				bfdPeers, err := frr.GetBFDPeers(exec)
				if err != nil {
					return err
				}

				if len(bfdPeers.Peers) != len(nodes) {
					return fmt.Errorf("expecting %d BFD peers, got %d", len(nodes), len(bfdPeers.Peers))
				}

				for _, node := range nodes {
					neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
					Expect(err).NotTo(HaveOccurred())

					peer, ok := bfdPeers.Peers[neighborIP]
					if !ok {
						return fmt.Errorf("BFD session with %s not found", neighborIP)
					}
					if peer.Status != "up" {
						return fmt.Errorf("BFD session with %s is not up", neighborIP)
					}
				}
				return nil
			}, 3*time.Minute, time.Second).ShouldNot(HaveOccurred())

			By("validating BGP sessions are still established")
			for _, node := range nodes {
				neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
				Expect(err).NotTo(HaveOccurred())
				validateSessionWithNeighbor(infra.KindLeaf, node.Name, exec, neighborIP, Established)
			}

			if underlay.Spec.Neighbors[0].BFD != nil && underlay.Spec.Neighbors[0].BFD.TransmitInterval != nil {
				By("validating BFD parameters are negotiated with the remote peer")
				exec := executor.ForContainer(infra.KindLeaf)
				Eventually(func(g Gomega) {
					bfdPeers, err := frr.GetBFDPeers(exec)
					g.Expect(err).NotTo(HaveOccurred())

					for _, node := range nodes {

						nodeIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
						Expect(err).NotTo(HaveOccurred())

						peer, ok := bfdPeers.Peers[nodeIP]
						g.Expect(ok).To(BeTrue(), "BFD peer for router %s not found", nodeIP)
						g.Expect(peer.Status).To(Equal("up"), "BFD session with router %s is not up", nodeIP)

						bfdSettings := underlay.Spec.Neighbors[0].BFD
						if bfdSettings.DetectMultiplier != nil {
							g.Expect(peer.RemoteDetectMultiplier).To(Equal(int(*bfdSettings.DetectMultiplier)),
								"Remote detect multiplier mismatch")
						}
						if bfdSettings.TransmitInterval != nil {
							g.Expect(peer.RemoteTransmitInterval).To(Equal(int(*bfdSettings.TransmitInterval)),
								"Remote transmit interval mismatch")
						}
						if bfdSettings.ReceiveInterval != nil {
							g.Expect(peer.RemoteReceiveInterval).To(Equal(int(*bfdSettings.ReceiveInterval)),
								"Remote receive interval mismatch")
						}
					}
				}, time.Minute, time.Second).Should(Succeed())
			}
		},
		Entry("simple bfd",
			v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay-simple-bfd",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:  64514,
					Nics: []string{"toswitch"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "100.65.0.0/24",
					},
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     64512,
							Address: "192.168.11.2",
							BFD:     &v1alpha1.BFDSettings{},
						},
					},
				},
			}),
		Entry("bfd with parameters",
			v1alpha1.Underlay{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "underlay-bfd-params",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.UnderlaySpec{
					ASN:  64514,
					Nics: []string{"toswitch"},
					EVPN: &v1alpha1.EVPNConfig{
						VTEPCIDR: "100.65.0.0/24",
					},
					Neighbors: []v1alpha1.Neighbor{
						{
							ASN:     64512,
							Address: "192.168.11.2",
							BFD: &v1alpha1.BFDSettings{
								TransmitInterval: ptr.To(uint32(90)),
								ReceiveInterval:  ptr.To(uint32(80)),
								DetectMultiplier: ptr.To(uint32(5)),
							},
						},
					},
				},
			}),
	)
})
