// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"

	corev1 "k8s.io/api/core/v1"
)

func checkBGPPrefixesForHostSession(frrk8s *corev1.Pod, hostSession v1alpha1.HostSession, prefixes []string, shouldExist bool) {
	exec := executor.ForPod(frrk8s.Namespace, frrk8s.Name, "frr")
	Eventually(func() error {
		ipv4Routes, ipv6Routes, err := frr.BGPRoutesFor(exec)
		Expect(err).NotTo(HaveOccurred())

		ipv4Prefixes, ipv6Prefixes := separateIPFamilies(prefixes)

		if err := checkPrefixesForIPFamily(frrk8s, ipv4Prefixes, hostSession.LocalCIDR.IPv4, "IPv4", shouldExist, ipv4Routes); err != nil {
			return err
		}

		if err := checkPrefixesForIPFamily(frrk8s, ipv6Prefixes, hostSession.LocalCIDR.IPv6, "IPv6", shouldExist, ipv6Routes); err != nil {
			return err
		}

		return nil
	}, 4*time.Minute, time.Second).WithOffset(1).ShouldNot(HaveOccurred())
}

func checkPrefixesForIPFamily(frrk8s *corev1.Pod, prefixes []string, localCIDR string,
	ipFamily string, shouldExist bool, routes frr.BGPRoutes) error {
	if len(prefixes) == 0 || localCIDR == "" {
		return nil
	}

	vniRouterIP, err := openperouter.RouterIPFromCIDR(localCIDR)
	Expect(err).NotTo(HaveOccurred())

	for _, p := range prefixes {
		routeExists := routes.HaveRoute(p, vniRouterIP)
		if shouldExist && !routeExists {
			return fmt.Errorf("%s prefix %s with nexthop %s not found in routes %v for pod %s", ipFamily, p, vniRouterIP, routes, frrk8s.Name)
		}
		if !shouldExist && routeExists {
			return fmt.Errorf("%s prefix %s with nexthop %s found in routes %v for pod %s", ipFamily, p, vniRouterIP, routes, frrk8s.Name)
		}
	}
	return nil
}
