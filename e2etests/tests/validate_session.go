// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
)

const Established = true

func validateFRRK8sSessionForHostSession(name string, hostsession v1alpha1.HostSession, established bool, frrk8sPods ...*corev1.Pod) {
	cidrs := hostsession.LocalCIDRs

	Expect(cidrs).NotTo(BeEmpty(), "either IPv4 or IPv6 CIDR must be provided")

	for _, cidr := range cidrs {
		neighborIP, err := openperouter.RouterIPFromCIDR(cidr)
		Expect(err).NotTo(HaveOccurred())

		for _, p := range frrk8sPods {
			By(fmt.Sprintf("checking the session between %s and session %s for CIDR %s", p.Name, name, cidr))
			exec := executor.ForPod(p.Namespace, p.Name, "frr")
			validate.SessionWithNeighbor(
				exec,
				validate.SessionParameters{
					FromName:    p.Name,
					ToName:      name,
					NeighborIP:  neighborIP,
					Established: established,
				},
			)
		}
	}
}

func waitForType5Route(exec executor.Executor, prefix string) {
	Eventually(func() error {
		evpn, err := frr.EVPNInfo(exec)
		if err != nil {
			return err
		}
		if !evpn.ContainsType5Prefix(prefix) {
			return fmt.Errorf("Type-5 route for %s not yet present", prefix)
		}
		return nil
	}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
}

// validateSessionDownForNeigh validates that the neighbor is down
// or if the session does not exist.
func validateSessionDownForNeigh(exec executor.Executor, neighborIP string) {
	Eventually(func() error {
		neigh, err := frr.NeighborInfo(neighborIP, exec)
		if errors.As(err, &frr.NoNeighborError{}) {
			return nil
		}
		if err != nil {
			return err
		}

		if neigh.BgpState == "Established" {
			return fmt.Errorf("neighbor %s is established: %v", neighborIP, neigh)
		}
		return nil
	}, 5*time.Minute, time.Second).ShouldNot(HaveOccurred())
}
