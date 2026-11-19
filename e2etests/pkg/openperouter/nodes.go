// SPDX-License-Identifier:Apache-2.0

package openperouter

import . "github.com/onsi/ginkgo/v2"

func FirstRouterNode(routers Routers) string {
	GinkgoHelper()
	for router := range routers.GetExecutors() {
		return router.NodeName()
	}
	Fail("no routers found")
	return ""
}
