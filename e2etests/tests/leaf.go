// SPDX-License-Identifier:Apache-2.0

package tests

import (
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
)

func changeLeafPrefixes(leaf infra.Leaf, redPrefixes, bluePrefixes []string) {
	leafConfiguration := infra.LeafConfiguration{
		Leaf: leaf,
		Red: infra.Addresses{
			IPV4: redPrefixes,
		},
		Blue: infra.Addresses{
			IPV4: bluePrefixes,
		},
	}
	config, err := infra.LeafConfigToFRR(leafConfiguration)
	Expect(err).NotTo(HaveOccurred())
	err = leaf.ReloadConfig(config)
	Expect(err).NotTo(HaveOccurred())
}

func removeLeafPrefixes(leaf infra.Leaf) {
	changeLeafPrefixes(leaf, []string{}, []string{})
}

func redistributeConnectedForLeaf(leaf infra.Leaf) {
	leafConfiguration := infra.LeafConfiguration{
		Leaf: leaf,
		Red: infra.Addresses{
			RedistributeConnected: true,
		},
		Blue: infra.Addresses{
			RedistributeConnected: true,
		},
	}
	config, err := infra.LeafConfigToFRR(leafConfiguration)
	Expect(err).NotTo(HaveOccurred())
	err = leaf.ReloadConfig(config)
	Expect(err).NotTo(HaveOccurred())
}
