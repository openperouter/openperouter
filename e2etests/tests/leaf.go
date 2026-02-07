// SPDX-License-Identifier:Apache-2.0

package tests

import (
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
)

func redistributeConnectedForLeaf(leaf infra.Leaf, redRTs, blueRTs infra.RouteTargets) {
	leafConfiguration := infra.LeafConfiguration{
		Leaf: leaf,
		Red: infra.Addresses{
			RedistributeConnected: true,
			RouteTargets:          redRTs,
		},
		Blue: infra.Addresses{
			RedistributeConnected: true,
			RouteTargets:          blueRTs,
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
