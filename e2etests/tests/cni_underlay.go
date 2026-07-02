// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"

	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/networklayerprotocol"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const (
	// cniUnderlayInterface is the name of the interface the macvlan CNI
	// plugin creates inside the router netns.
	cniUnderlayInterface = "underlay0"
	// cniUnderlayInterfaceRenamed is used to force a netns rebuild by
	// changing the desired CNI interface name.
	cniUnderlayInterfaceRenamed = "underlay1"
)

// cniUnderlayIP returns the static IP assigned to the CNI-provisioned
// underlay interface of the i-th node, on the subnet shared with leafkind1.
// The .10+ addresses are clear of the ones the dev environment assigns
// (leaf .2, nodes .3/.4).
func cniUnderlayIP(nodeIndex int) string {
	return fmt.Sprintf("192.168.11.%d", 10+nodeIndex)
}

// cniUnderlayForNode builds a node-scoped Underlay whose interface is
// provisioned by the macvlan CNI plugin on top of toswitch1 (which stays in
// the host netns) with a per-node static IP.
func cniUnderlayForNode(nodeIndex int, node corev1.Node, ifName string) v1alpha1.Underlay {
	rawConfig := fmt.Sprintf(`{
  "cniVersion": "1.0.0",
  "name": "macvlan-underlay",
  "plugins": [
    {
      "type": "macvlan",
      "master": "toswitch1",
      "mode": "bridge",
      "ipam": {
        "type": "static",
        "addresses": [
          {"address": "%s/24"}
        ]
      }
    }
  ]
}`, cniUnderlayIP(nodeIndex))

	return v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("underlay-cni-%d", nodeIndex),
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN: 64514,
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"kubernetes.io/hostname": node.Name},
			},
			Interfaces: []v1alpha1.UnderlayInterface{
				{
					Type: v1alpha1.UnderlayInterfaceTypeCNI,
					CNIDevice: &v1alpha1.CNIDevice{
						Type:          v1alpha1.CNIConfigTypeRawConfig,
						RawConfig:     &apiextensionsv1.JSON{Raw: []byte(rawConfig)},
						InterfaceName: ptr.To(ifName),
					},
				},
			},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     new(int64(64512)),
					Address: new("192.168.11.2"),
				},
			},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"100.65.0.0/24"},
			},
		},
	}
}

var _ = Describe("CNI underlay configuration", Ordered, func() {
	var cs clientset.Interface
	nodes := []corev1.Node{}

	cniUnderlays := func(ifName string) []v1alpha1.Underlay {
		underlays := make([]v1alpha1.Underlay, 0, len(nodes))
		for i, node := range nodes {
			underlays = append(underlays, cniUnderlayForNode(i, node, ifName))
		}
		return underlays
	}

	// configureLeafForCNIUnderlay points leafkind1 at the static IPs of the
	// CNI-provisioned interfaces, since the standard leaf configuration only
	// knows the node addresses.
	configureLeafForCNIUnderlay := func() {
		leafConfig := infra.LeafKindConfiguration{
			ASN:              infra.LeafKind1Config.ASN,
			SpinePeerAddress: infra.LeafKind1Config.SpinePeerAddress,
			PERouterASN:      64514,
			BGPAddressFamilies: []networklayerprotocol.NLP{
				{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.Unicast},
			},
		}
		for i := range nodes {
			leafConfig.Neighbors = append(leafConfig.Neighbors, infra.Neighbor{ID: cniUnderlayIP(i)})
		}
		configString, err := infra.LeafKindConfigToFRR(leafConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(infra.LeafKind1Config.ReloadConfig(configString)).To(Succeed())
	}

	validateCNISessions := func(established bool) {
		leafExec := executor.ForContainer(infra.KindLeaf)
		for i, node := range nodes {
			if !established {
				validateSessionDownForNeigh(leafExec, cniUnderlayIP(i))
				continue
			}
			validateSessionWithNeighbor(leafExec, validationParameters{
				fromName:    infra.KindLeaf,
				toName:      node.Name,
				neighborIP:  cniUnderlayIP(i),
				established: established,
			})
		}
	}

	validateCNIInterfaces := func(ifName string, present bool) {
		for _, node := range nodes {
			Eventually(func() bool {
				return openperouter.IsInterfaceInNS(node.Name, ifName, openperouter.NamedNetns)
			}, 3*time.Minute, time.Second).Should(Equal(present),
				fmt.Sprintf("interface %s presence in the router netns of %s should be %t", ifName, node.Name, present))
		}
	}

	BeforeAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		_, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())
		nodesItems, err := cs.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		nodes = nodesItems.Items
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

		configureLeafForCNIUnderlay()

		err = Updater.Update(config.Resources{Underlays: cniUnderlays(cniUnderlayInterface)})
		Expect(err).NotTo(HaveOccurred())
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
		By("restoring the standard leaf configuration")
		Expect(infra.LeafKind1Config.UpdateConfig(nodes, infra.LeafKindConfiguration{})).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs)
	})

	It("establishes BGP sessions through the CNI-provisioned interfaces", func() {
		validateCNIInterfaces(cniUnderlayInterface, true)
		By("checking the parent device stays in the host netns")
		for _, node := range nodes {
			Expect(openperouter.IsInterfaceInDefaultNetns(node.Name, "toswitch1")).To(BeTrue(),
				fmt.Sprintf("toswitch1 should stay in the host netns of %s", node.Name))
		}
		validateCNISessions(Established)
	})

	It("provisions again with a fresh CNI ADD after a netns rebuild", func() {
		By("renaming the CNI interface, which requires rebuilding the netns")
		err := Updater.Update(config.Resources{Underlays: cniUnderlays(cniUnderlayInterfaceRenamed)})
		Expect(err).NotTo(HaveOccurred())

		validateCNIInterfaces(cniUnderlayInterfaceRenamed, true)
		validateCNIInterfaces(cniUnderlayInterface, false)
		validateCNISessions(Established)
	})

	It("removes the CNI interfaces when the underlay is deleted", func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		validateCNIInterfaces(cniUnderlayInterfaceRenamed, false)
		validateCNISessions(!Established)
	})
})
