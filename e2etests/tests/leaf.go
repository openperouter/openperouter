// SPDX-License-Identifier:Apache-2.0

package tests

import (
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
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
