// SPDX-License-Identifier:Apache-2.0

package tests

import (
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	corev1 "k8s.io/api/core/v1"
)

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
