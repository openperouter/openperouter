// SPDX-License-Identifier:Apache-2.0

package systemd_static

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
)

type routeCheck int

const (
	mustContain routeCheck = iota
	shouldNotContain
)

// checkRouteFromLeaf checks if the expected routes from a leaf are present in the routers' EVPN info
func checkRouteFromLeaf(leaf infra.Leaf, routers openperouter.Routers, vni v1alpha1.L3VNI, check routeCheck, prefixes []string) {
	By(fmt.Sprintf("checking routes from leaf %s on vni %s, check %v %v", leaf.Name, vni.Name, check, prefixes))
	Eventually(func() error {
		for exec := range routers.GetExecutors() {
			evpn, err := frr.EVPNInfo(exec)
			if err != nil {
				return fmt.Errorf("failed to get EVPN info from %s: %w", exec.Name(), err)
			}
			for _, prefix := range prefixes {
				if check == mustContain && !evpn.ContainsType5RouteForVNI(prefix, leaf.VTEPIP, int(vni.Spec.VNI)) {
					return fmt.Errorf("type5 route for %s - %s not found in %v in router %s", prefix, leaf.VTEPIP, evpn, exec.Name())
				}
				if check == shouldNotContain && evpn.ContainsType5RouteForVNI(prefix, leaf.VTEPIP, int(vni.Spec.VNI)) {
					return fmt.Errorf("type5 route for %s - %s found in %v in router %s", prefix, leaf.VTEPIP, evpn, exec.Name())
				}
			}
		}
		return nil
	}, 3*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
}
