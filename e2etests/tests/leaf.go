// SPDX-License-Identifier:Apache-2.0

package tests

import (
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	corev1 "k8s.io/api/core/v1"
)

func redistributeConnectedForLeaf(leaf infra.Leaf) {
	leafConfiguration := infra.LeafConfiguration{
		Leaf: leaf,
		Red: infra.Addresses{
			RedistributeConnected: true,
		},
		Blue: infra.Addresses{
			RedistributeConnected: true,
		},
		Default: infra.Addresses{
			RedistributeConnected: true,
		},
	}
	config, err := infra.LeafConfigToFRR(leafConfiguration)
	Expect(err).NotTo(HaveOccurred())
	err = leaf.ReloadConfig(config)
	Expect(err).NotTo(HaveOccurred())
}

func redistributeConnectedForLeafKind(nodes []corev1.Node) {
	neighbors := []string{}
	for _, node := range nodes {
		neighborIP, err := infra.NeighborIP(infra.KindLeaf, node.Name)
		Expect(err).NotTo(HaveOccurred())
		neighbors = append(neighbors, neighborIP)
	}

	config := infra.LeafKindConfiguration{
		RedistributeConnected: true,
		Neighbors:             neighbors,
	}

	configString, err := infra.LeafKindConfigToFRR(config)
	Expect(err).NotTo(HaveOccurred())
	err = infra.LeafKindConfig.ReloadConfig(configString)
	Expect(err).NotTo(HaveOccurred())
}

func resetLeafKindConfig(nodes []corev1.Node) {
	err := infra.UpdateLeafKindConfig(nodes, false)
	Expect(err).NotTo(HaveOccurred())
}
